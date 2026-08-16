// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

// testClock drives a limiter without sleeping. Waits are recorded rather than
// served, so a ten-second window is exercised in microseconds and the
// scheduling arithmetic is checked directly instead of inferred from wall
// time.
type testClock struct {
	mu      sync.Mutex
	now     time.Time
	waits   []time.Duration
	blocked chan struct{} // when non-nil, After blocks on it instead of firing
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *testClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	blocked := c.blocked
	c.mu.Unlock()
	if blocked != nil {
		return make(chan time.Time) // never fires; the caller must take ctx.Done
	}
	ch := make(chan time.Time, 1)
	ch <- c.Now()
	return ch
}

// Waits returns every delay the limiter asked for.
func (c *testClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.waits))
	copy(out, c.waits)
	return out
}

func (c *testClock) Block() {
	c.mu.Lock()
	c.blocked = make(chan struct{})
	c.mu.Unlock()
}

// A recordingAfter is an after function that fires immediately and remembers
// every delay it was asked for. A test that has blocked the clock swaps this
// in for the final measurement: the clock's own Waits stop being a witness
// once l.after no longer routes through it.
type recordingAfter struct {
	mu     sync.Mutex
	delays []time.Duration
}

// After records d and returns a channel that has already fired.
func (r *recordingAfter) After(d time.Duration) <-chan time.Time {
	r.mu.Lock()
	r.delays = append(r.delays, d)
	r.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// Delays reports every delay asked for so far.
func (r *recordingAfter) Delays() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, len(r.delays))
	copy(out, r.delays)
	return out
}

// waitUntilParked blocks until the limiter has asked the clock to sleep n
// times. A caller that cancels before its request reaches the wait cancels a
// request that reserved nothing, which tests nothing.
func waitUntilParked(t *testing.T, c *testClock, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(c.Waits()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d requests reached the wait", len(c.Waits()), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// newTestLimiter returns a limiter on a controllable clock.
func newTestLimiter(t *testing.T, rules []Rule, tier Tier) (*StandardLimiter, *testClock) {
	t.Helper()
	clock := newTestClock()
	l := NewLimiter(rules, tier)
	l.now = clock.Now
	l.after = clock.After
	return l, clock
}

// matchCase is one request and the limit it must be paced against.
type matchCase struct {
	name         string
	method, path string
	wantRequests int
}

// TestRuleMatching pins the traps in the published table: the same path with
// different methods has different limits, and a longer path must not fall
// into a shorter one's rule.
func TestRuleMatching(t *testing.T) {
	cases := []matchCase{
		{"general falls through", http.MethodGet, "/anything", 9000},
		{"book", http.MethodGet, "/book", 1500},
		{"books is not book", http.MethodPost, "/books", 500},
		{"prices is not price", http.MethodPost, "/prices", 500},
		{"prices-history is its own rule", http.MethodGet, "/prices-history", 1000},
		{"data orders is not orders", http.MethodGet, "/data/orders", 500},
		{"data trades is not trades", http.MethodGet, "/data/trades", 500},
		{"post order", http.MethodPost, "/order", 5000},
		{"delete order", http.MethodDelete, "/order", 5000},
		{"post orders", http.MethodPost, "/orders", 2000},
		{"cancel all", http.MethodDelete, "/cancel-all", 250},
		{"balance allowance read", http.MethodGet, "/balance-allowance", 200},
		{"balance allowance update is stricter", http.MethodGet, "/balance-allowance/update", 50},
		{"auth", http.MethodPost, "/auth/api-key", 100},
	}

	l, _ := newTestLimiter(t, CLOBRateLimits, TierStandard)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := l.match(tc.method, tc.path)
			if !ok {
				t.Fatalf("%s %s matched no rule", tc.method, tc.path)
			}
			if got := l.rules[idx].Limit.Requests; got != tc.wantRequests {
				t.Errorf("%s %s matched %q (%d req), want %d",
					tc.method, tc.path, l.rules[idx].Prefix, got, tc.wantRequests)
			}
		})
	}
}

// TestSlidingWindowPacing checks that a full window's worth of requests goes
// straight through and the next one waits exactly until the oldest expires.
func TestSlidingWindowPacing(t *testing.T) {
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: 3, Window: 10 * time.Second}}}
	l, clock := newTestLimiter(t, rules, TierStandard)
	ctx := context.Background()
	r := Request{Method: http.MethodGet, Path: "/book"}

	for i := range 3 {
		if err := l.Wait(ctx, r); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("the first three requests waited: %v", got)
	}

	// The fourth has to wait for the first to leave the window.
	if err := l.Wait(ctx, r); err != nil {
		t.Fatal(err)
	}
	waits := clock.Waits()
	if len(waits) != 1 || waits[0] != 10*time.Second {
		t.Fatalf("fourth request waited %v, want one wait of 10s", waits)
	}

	// After the window has passed, capacity is back.
	clock.Advance(10 * time.Second)
	if err := l.Wait(ctx, r); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 1 {
		t.Errorf("a request after the window waited: %v", got)
	}
}

// TestTokenBucketPacing checks the per-signer trading allowance: a full
// bucket spends its burst at once, then refills at the tier's rate.
func TestTokenBucketPacing(t *testing.T) {
	// Two tokens a second, burst of four.
	tier := Tier{"test", TokenBucket{Rate: 2, Burst: 4}, TokenBucket{Rate: 2, Burst: 4}}
	l, clock := newTestLimiter(t, nil, tier)
	ctx := context.Background()
	order := Request{Method: http.MethodPost, Path: "/order"}

	for i := range 4 {
		if err := l.Wait(ctx, order); err != nil {
			t.Fatalf("order %d: %v", i, err)
		}
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("the burst waited: %v", got)
	}

	// The fifth costs a token the bucket does not have: half a second at
	// two per second.
	if err := l.Wait(ctx, order); err != nil {
		t.Fatal(err)
	}
	waits := clock.Waits()
	if len(waits) != 1 || waits[0] != 500*time.Millisecond {
		t.Fatalf("waits = %v, want one of 500ms", waits)
	}
}

// TestBatchCost checks that a batch spends one token per entry, which is what
// the exchange charges and what makes a large batch inadmissible rather than
// merely slow.
func TestBatchCost(t *testing.T) {
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: 10}, TokenBucket{Rate: 1, Burst: 10}}
	l, clock := newTestLimiter(t, nil, tier)
	ctx := context.Background()

	// Ten in one batch empties a burst of ten.
	if err := l.Wait(ctx, Request{Method: http.MethodPost, Path: "/orders", Cost: 10}); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("a batch inside the burst waited: %v", got)
	}

	// One more costs a full second at one token a second.
	if err := l.Wait(ctx, Request{Method: http.MethodPost, Path: "/order"}); err != nil {
		t.Fatal(err)
	}
	waits := clock.Waits()
	if len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("waits = %v, want one of 1s", waits)
	}
}

// TestOrderAndCancelBucketsAreSeparate pins the documented rule that spending
// from one bucket leaves the other untouched.
func TestOrderAndCancelBucketsAreSeparate(t *testing.T) {
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: 2}, TokenBucket{Rate: 1, Burst: 2}}
	l, clock := newTestLimiter(t, nil, tier)
	ctx := context.Background()

	for range 2 {
		if err := l.Wait(ctx, Request{Method: http.MethodPost, Path: "/order"}); err != nil {
			t.Fatal(err)
		}
	}
	// The order bucket is empty; a cancel must still go straight through.
	if err := l.Wait(ctx, Request{Method: http.MethodDelete, Path: "/order"}); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("a cancel waited on an empty order bucket: %v", got)
	}
}

// TestCancelledWaitRefundsTheReservation checks the case that decides whether
// a storm of cancelled requests drains the allowance: a request that never
// goes must give its reservation back.
func TestCancelledWaitRefundsTheReservation(t *testing.T) {
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: 1, Window: 10 * time.Second}}}
	l, clock := newTestLimiter(t, rules, TierStandard)
	r := Request{Method: http.MethodGet, Path: "/book"}

	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// The next one must queue. Cancel it while it waits.
	clock.Block()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx, r) }()
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled wait returned nil")
	}

	// The abandoned slot is free again: the window still holds only the one
	// request that actually went, so after it expires a new one goes at once.
	//
	// Measure with a recordingAfter rather than the clock: swapping l.after
	// away from the clock is what lets this request run at all, and a delay
	// it never sees is a delay it cannot report.
	clock.Advance(10 * time.Second)
	rec := &recordingAfter{}
	l.after = rec.After
	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := rec.Delays(); len(got) != 0 {
		t.Errorf("the refunded slot was still counted against the window: waited %v", got)
	}
}

// TestCancelledOrderRefundsItsWindowSlot is the regression test for a refund
// that used the wrong time. A request is paced by two layers at once, and the
// send time is the later of the two; the sliding window's slot, however,
// holds the time the window itself handed out. Refunding the combined time
// matched no slot, so every cancelled order silently leaked one — and the
// trading bucket is far tighter than the endpoint window, so the combined
// time is the bucket's on essentially every order.
func TestCancelledOrderRefundsItsWindowSlot(t *testing.T) {
	const slots = 10
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: slots, Window: 10 * time.Second}}}
	// A bucket tight enough that the second order is delayed by the bucket
	// rather than by the window, which is what makes the two times differ.
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: 1}, TokenBucket{Rate: 1, Burst: 1}}
	l, clock := newTestLimiter(t, rules, tier)
	order := Request{Method: http.MethodPost, Path: "/order"}

	// One order actually goes, spending one window slot for real.
	if err := l.Wait(context.Background(), order); err != nil {
		t.Fatal(err)
	}

	// The rest queue on the empty bucket and are abandoned.
	clock.Block()
	for i := range slots - 1 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		want := len(clock.Waits()) + 1
		go func() { done <- l.Wait(ctx, order) }()
		// Cancel only once the request is parked. Cancelling sooner returns
		// from the opening ctx.Err() check, having reserved nothing.
		waitUntilParked(t, clock, want)
		cancel()
		if err := <-done; err == nil {
			t.Fatalf("cancelled order %d returned nil", i)
		}
	}

	// Only one request ever went. A plain GET shares the window rule but
	// spends no tokens, so it must go at once rather than queue behind nine
	// orders that were never sent.
	rec := &recordingAfter{}
	l.after = rec.After
	if err := l.Wait(context.Background(), Request{Method: http.MethodGet, Path: "/book"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Delays(); len(got) != 0 {
		t.Errorf("a later request waited %v: the window still counts %d cancelled orders",
			got, slots-1)
	}
}

// TestCancelledWaitsLeaveTheAllowanceExact interleaves cancelled requests
// with successful ones and then counts what the limiter will still admit. A
// refund that goes to the wrong slot is invisible one request at a time; it
// shows up as an allowance that no longer adds up.
func TestCancelledWaitsLeaveTheAllowanceExact(t *testing.T) {
	const (
		slots     = 500
		sent      = 50
		abandoned = 300
	)
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: slots, Window: 10 * time.Second}}}
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: sent}, TokenBucket{Rate: 1, Burst: sent}}
	l, clock := newTestLimiter(t, rules, tier)
	order := Request{Method: http.MethodPost, Path: "/order"}
	book := Request{Method: http.MethodGet, Path: "/book"}

	// Spend the burst for real: sent window slots are now legitimately gone.
	for i := range sent {
		if err := l.Wait(context.Background(), order); err != nil {
			t.Fatalf("order %d: %v", i, err)
		}
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("the burst waited: %v", got)
	}

	// Every further order queues on the empty bucket. Park them all, then
	// abandon them all at once.
	clock.Block()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, abandoned)
	for range abandoned {
		go func() { done <- l.Wait(ctx, order) }()
	}
	waitUntilParked(t, clock, abandoned)
	cancel()
	for range abandoned {
		if err := <-done; err == nil {
			t.Fatal("an abandoned request reported that it could be sent")
		}
	}

	// The window should hold exactly the sent requests, so slots-sent more
	// go straight through and the one after that queues.
	rec := &recordingAfter{}
	l.after = rec.After
	for i := range slots - sent {
		if err := l.Wait(context.Background(), book); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if got := rec.Delays(); len(got) != 0 {
			t.Fatalf("request %d of the remaining allowance waited %v: %d abandoned "+
				"requests were still counted", i, got, abandoned)
		}
	}
	if err := l.Wait(context.Background(), book); err != nil {
		t.Fatal(err)
	}
	if got := rec.Delays(); len(got) != 1 {
		t.Errorf("the request past the window waited %v, want one wait: the "+
			"refunds handed back more than they took", got)
	}
}

// TestRefundLandsOnTheRequestsOwnSlot is the regression test for a refund
// that identified its slot by time. Two requests are routinely scheduled for
// the same instant — anything queued behind the same expiry, or behind the
// same freeze — and a refund that took the first slot holding that time freed
// somebody else's, leaving the abandoned request's own slot counted and the
// live request's slot free for a third. The window then admitted one request
// more than the limit, which is the failure that gets an address throttled.
func TestRefundLandsOnTheRequestsOwnSlot(t *testing.T) {
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: 2, Window: 10 * time.Second}}}
	l, clock := newTestLimiter(t, rules, TierStandard)
	r := Request{Method: http.MethodGet, Path: "/book"}

	// Two requests go for real, filling the window at t0.
	for i := range 2 {
		if err := l.Wait(context.Background(), r); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Fatalf("the first two requests waited: %v", got)
	}

	// Two more queue, both scheduled for the same instant ten seconds out.
	// Park them one at a time so that which slot each holds is not a race.
	clock.Block()
	keptCtx, cancelKept := context.WithCancel(context.Background())
	kept := make(chan error, 1)
	go func() { kept <- l.Wait(keptCtx, r) }()
	defer func() { cancelKept(); <-kept }()
	waitUntilParked(t, clock, 1)

	goneCtx, cancelGone := context.WithCancel(context.Background())
	gone := make(chan error, 1)
	go func() { gone <- l.Wait(goneCtx, r) }()
	waitUntilParked(t, clock, 2)
	cancelGone()
	if err := <-gone; err == nil {
		t.Fatal("a cancelled wait returned nil")
	}

	// The window still owes the request that stayed. A newcomer must queue
	// behind it rather than take its place at t0, where two requests have
	// already gone.
	rec := &recordingAfter{}
	l.after = rec.After
	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got := rec.Delays()
	if len(got) != 1 || got[0] != 10*time.Second {
		t.Errorf("the request after the refund waited %v, want one wait of 10s: "+
			"the refund freed the slot of the request that is still waiting", got)
	}
}

// TestRefundRestoresTheSendItDisplaced is the regression test for a refund
// that blanked its slot. A window remembers only its last Requests sends, so
// reserving displaces the oldest of them; when that send is still inside the
// window, the reservation is queued behind it rather than admitted. Blanking
// the slot on cancellation threw away the record of a send that had really
// happened, and the next request went at once instead of waiting for it.
func TestRefundRestoresTheSendItDisplaced(t *testing.T) {
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: 1, Window: 10 * time.Second}}}
	l, clock := newTestLimiter(t, rules, TierStandard)
	r := Request{Method: http.MethodGet, Path: "/book"}

	// One request goes for real at t0 and fills the window.
	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// The next queues behind it, displacing its record, and is abandoned.
	clock.Block()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx, r) }()
	waitUntilParked(t, clock, 1)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled wait returned nil")
	}

	// The send at t0 is still inside the window, so a request at t0 must wait
	// the full window for it.
	rec := &recordingAfter{}
	l.after = rec.After
	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got := rec.Delays()
	if len(got) != 1 || got[0] != 10*time.Second {
		t.Errorf("the request after the refund waited %v, want one wait of 10s: "+
			"the refund forgot the send that was already made", got)
	}
}

// TestConcurrentWaitsFillEveryWindowExactly drives far more goroutines at one
// saturated endpoint than it can admit and checks the schedule they are
// handed. Nothing here may depend on which goroutine reached the lock first:
// what must hold is that every window of the ladder carries exactly the
// limit — never more, which is what Cloudflare throttles for, and never
// fewer, which is allowance left on the table — and that every waiter is
// given a place rather than left cycling behind the others.
func TestConcurrentWaitsFillEveryWindowExactly(t *testing.T) {
	const (
		goroutines = 200
		limit      = 10
		window     = 10 * time.Second
	)
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: limit, Window: window}}}
	l, clock := newTestLimiter(t, rules, TierStandard)

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(context.Background(), Request{Method: http.MethodGet, Path: "/book"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	// A request admitted at once asks for no delay at all, so the schedule is
	// the recorded waits plus one zero for every request that skipped them.
	waits := clock.Waits()
	schedule := make([]time.Duration, goroutines-len(waits), goroutines)
	schedule = append(schedule, waits...)
	if len(schedule) != goroutines {
		t.Fatalf("%d schedules for %d requests", len(schedule), goroutines)
	}
	slices.Sort(schedule)
	for i, d := range schedule {
		if want := time.Duration(i/limit) * window; d != want {
			t.Fatalf("request %d of %d scheduled at %v, want %v",
				i, goroutines, d, want)
		}
	}
}

// TestTooManyRequestsFreezesOnlyItsOwnBucket checks that a refusal on one
// endpoint does not stall every other endpoint.
func TestTooManyRequestsFreezesOnlyItsOwnBucket(t *testing.T) {
	l, clock := newTestLimiter(t, CLOBRateLimits, TierStandard)
	ctx := context.Background()

	book := Request{Method: http.MethodGet, Path: "/book"}
	price := Request{Method: http.MethodGet, Path: "/price"}

	header := http.Header{}
	header.Set("Retry-After", "5")
	l.Observe(book, http.StatusTooManyRequests, header)

	if err := l.Wait(ctx, book); err != nil {
		t.Fatal(err)
	}
	waits := clock.Waits()
	if len(waits) != 1 || waits[0] != 5*time.Second {
		t.Fatalf("the refused endpoint waited %v, want one of 5s", waits)
	}
	if err := l.Wait(ctx, price); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 1 {
		t.Errorf("an unrelated endpoint was frozen too: %v", got)
	}
}

// TestSuccessfulResponseDoesNotFreeze guards a trap in the documentation: on
// an admitted request Poly-RateLimit-Reset may be the current instant, so
// acting on it would stall the client after every successful call.
func TestSuccessfulResponseDoesNotFreeze(t *testing.T) {
	l, clock := newTestLimiter(t, CLOBRateLimits, TierStandard)
	book := Request{Method: http.MethodGet, Path: "/book"}

	header := http.Header{}
	header.Set("Poly-RateLimit-Reset", "99999999999")
	l.Observe(book, http.StatusOK, header)

	if err := l.Wait(context.Background(), book); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Errorf("a successful response froze the bucket: %v", got)
	}
}

// TestRemainingHeaderIsAuthoritative checks that the exchange's own count
// overrides the local prediction, which is what keeps a client honest when
// several processes share one signer.
func TestRemainingHeaderIsAuthoritative(t *testing.T) {
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: 100}, TokenBucket{Rate: 1, Burst: 100}}
	l, clock := newTestLimiter(t, nil, tier)
	order := Request{Method: http.MethodPost, Path: "/order"}

	// Locally the bucket looks full. The exchange says it is empty.
	header := http.Header{}
	header.Set("Poly-RateLimit-Remaining", "0")
	l.Observe(order, http.StatusOK, header)

	if err := l.Wait(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	waits := clock.Waits()
	if len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("waits = %v, want one of 1s after the server reported an empty bucket", waits)
	}
}

// TestWarningHeaderIsCounted checks the signal Polymarket sends while the
// per-signer limiter runs in warning mode, which a desk wants to monitor
// before enforcement starts.
func TestWarningHeaderIsCounted(t *testing.T) {
	l, _ := newTestLimiter(t, CLOBRateLimits, TierStandard)
	order := Request{Method: http.MethodPost, Path: "/order"}

	header := http.Header{}
	header.Set("Poly-RateLimit-Warning", "true")
	l.Observe(order, http.StatusOK, header)
	l.Observe(order, http.StatusOK, header)
	l.Observe(order, http.StatusOK, http.Header{})

	if got := l.Warnings(); got != 2 {
		t.Errorf("Warnings() = %d, want 2", got)
	}
}

// TestUnknownHostGetsNoPacing checks that this client does not invent limits
// for a server whose limits it does not know.
func TestUnknownHostGetsNoPacing(t *testing.T) {
	if _, ok := RateLimitsFor("https://example.invalid"); ok {
		t.Error("an unknown host was given limits")
	}
	s := NewSession("https://example.invalid")
	if s.Limiter() != nil {
		t.Error("an unknown host got a limiter")
	}
	for _, host := range []string{DefaultHost, GammaHost, DataHost, BridgeHost, RelayerHost} {
		if _, ok := RateLimitsFor(host); !ok {
			t.Errorf("no limits known for %s", host)
		}
		if NewSession(host).Limiter() == nil {
			t.Errorf("%s got no limiter", host)
		}
	}
	if NewSession(DefaultHost, WithLimiter(nil)).Limiter() != nil {
		t.Error("WithLimiter(nil) did not turn pacing off")
	}
}

// TestConcurrentWaitsAreCounted runs many requests through one limiter at
// once. Under -race this also covers the locking; the count is what proves no
// reservation was lost or double-issued.
func TestConcurrentWaitsAreCounted(t *testing.T) {
	const n = 500
	rules := []Rule{{Prefix: "/", Limit: RateLimit{Requests: n, Window: 10 * time.Second}}}
	l, clock := newTestLimiter(t, rules, TierStandard)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(ctx, Request{Method: http.MethodGet, Path: "/book"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if got := clock.Waits(); len(got) != 0 {
		t.Errorf("%d of %d concurrent requests waited despite fitting the window", len(got), n)
	}
	// One more must now queue: the window is exactly full.
	if err := l.Wait(ctx, Request{Method: http.MethodGet, Path: "/book"}); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 1 {
		t.Errorf("the request past the window did not wait: %v", got)
	}
}

// TestWaitRespectsContext checks that a limiter never outlives its caller's
// context, including one that is already done.
func TestWaitRespectsContext(t *testing.T) {
	l, _ := newTestLimiter(t, CLOBRateLimits, TierStandard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx, Request{Method: http.MethodGet, Path: "/book"}); err == nil {
		t.Fatal("a cancelled context was ignored")
	}
}

// A hostileDelayCase is one refusal's delay header and the freeze the limiter
// is allowed to take from it. A header is somebody else's arithmetic, so the
// interesting cases are the ones that are not arithmetic at all.
type hostileDelayCase struct {
	name  string
	key   string
	value string
	want  time.Duration // what the next request to that endpoint must wait
}

// TestDelayHeadersCannotWedgeTheClient checks that no value a server can put
// in a delay header stops this client for longer than maxFreeze.
//
// Both directions of the same fault are pinned here. A Retry-After of a few
// billion seconds once became a freeze of 292 years, which is a wedge that
// outlives the process; one large enough to overflow a Duration wrapped to a
// deadline in the past, which cancelled the backoff altogether and let the
// client hammer a server that had just refused it. A Poly-RateLimit-Reset in
// milliseconds rather than seconds — a plausible mistake on either side of
// the wire — read as a date in the year 5138.
func TestDelayHeadersCannotWedgeTheClient(t *testing.T) {
	cases := []hostileDelayCase{
		{"a delay within the cap is honoured exactly", "Retry-After", "30", 30 * time.Second},
		{"seconds enough to freeze for centuries", "Retry-After", "9223372036", maxFreeze},
		{"seconds enough to overflow a duration", "Retry-After", "10000000000", maxFreeze},
		{"seconds too many to parse at all", "Retry-After", "999999999999999999999", 0},
		{"negative seconds", "Retry-After", "-5", 0},
		{"seconds that are not a number", "Retry-After", "soon", 0},
		{"no delay named", "Retry-After", "", 0},
		{"a date already past", "Retry-After", "Mon, 02 Jan 2006 15:04:05 GMT", 0},
		{"a date past the end of time", "Retry-After", "Fri, 31 Dec 9999 23:59:59 GMT", maxFreeze},
		{"a reset in the far future", "Poly-RateLimit-Reset", "99999999999", maxFreeze},
		{"a reset absurdly far in the future", "Poly-RateLimit-Reset", "999999999999999999", maxFreeze},
		{"a reset already past", "Poly-RateLimit-Reset", "1", 0},
		{"a reset that is not a number", "Poly-RateLimit-Reset", "later", 0},
	}
	book := Request{Method: http.MethodGet, Path: "/book"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, clock := newTestLimiter(t, CLOBRateLimits, TierStandard)
			header := http.Header{}
			header.Set(tc.key, tc.value)
			l.Observe(book, http.StatusTooManyRequests, header)

			if err := l.Wait(context.Background(), book); err != nil {
				t.Fatal(err)
			}
			waits := clock.Waits()
			if tc.want == 0 {
				if len(waits) != 0 {
					t.Fatalf("%s: %q froze the endpoint for %v, want no freeze", tc.key, tc.value, waits)
				}
				return
			}
			if len(waits) != 1 || waits[0] != tc.want {
				t.Fatalf("%s: %q gave waits %v, want one of %v", tc.key, tc.value, waits, tc.want)
			}
		})
	}
}

// A hostileBalanceCase is one Poly-RateLimit-Remaining and the pacing the
// per-signer bucket must still impose after adopting it. The delays are for
// four single orders sent at one instant against a bucket that holds two
// tokens and refills one a second.
type hostileBalanceCase struct {
	name  string
	value string
	want  []time.Duration
}

// TestBalanceHeaderCannotWedgeOrDisarmTheBucket checks the other half of what
// a server can say. The exchange's own count outranks the local prediction,
// but a count that is not a usable number must be discarded rather than
// believed.
//
// A balance of NaN parses cleanly and is permanent: it survives every later
// refill, and NaN < cost is false, so the bucket admits every order for the
// life of the process — the limiter is switched off by a five-byte header. A
// balance of minus infinity, or a merely absurd one, put the next order past
// the end of time instead.
func TestBalanceHeaderCannotWedgeOrDisarmTheBucket(t *testing.T) {
	paced := []time.Duration{time.Second, 2 * time.Second}
	behind := []time.Duration{3 * time.Second, 4 * time.Second, 5 * time.Second, 6 * time.Second}
	cases := []hostileBalanceCase{
		{"no balance named", "", paced},
		{"a balance that is not a number", "plenty", paced},
		{"NaN", "NaN", paced},
		{"positive infinity", "Inf", paced},
		{"a balance beyond the burst", "1e300", paced},
		{"a balance too large to parse", "1e309", paced},
		{"negative infinity", "-Inf", behind},
		{"an absurd deficit", "-1e300", behind},
		{"an ordinary deficit is believed", "-1", []time.Duration{
			2 * time.Second, 3 * time.Second, 4 * time.Second, 5 * time.Second}},
	}
	order := Request{Method: http.MethodPost, Path: "/order"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier := Tier{"test", TokenBucket{Rate: 1, Burst: 2}, TokenBucket{Rate: 1, Burst: 2}}
			l, clock := newTestLimiter(t, nil, tier)
			header := http.Header{}
			header.Set("Poly-RateLimit-Remaining", tc.value)
			l.Observe(order, http.StatusOK, header)

			for i := range 4 {
				if err := l.Wait(context.Background(), order); err != nil {
					t.Fatalf("order %d: %v", i, err)
				}
			}
			waits := clock.Waits()
			if len(waits) != len(tc.want) {
				t.Fatalf("Remaining %q: waits %v, want %v", tc.value, waits, tc.want)
			}
			for i, d := range waits {
				if d != tc.want[i] {
					t.Fatalf("Remaining %q: waits %v, want %v", tc.value, waits, tc.want)
				}
			}
		})
	}
}

// TestFreezeEndsWhenItsDeadlinePasses checks that a refusal defers requests
// rather than retiring them: a storm in which every single response is a 429
// asking for another ten seconds must leave a limiter that paces normally
// once the clock is past the last of those deadlines.
func TestFreezeEndsWhenItsDeadlinePasses(t *testing.T) {
	l, clock := newTestLimiter(t, CLOBRateLimits, TierStandard)
	book := Request{Method: http.MethodGet, Path: "/book"}
	header := http.Header{}
	header.Set("Retry-After", "10")

	for i := range 100 {
		if err := l.Wait(context.Background(), book); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		l.Observe(book, http.StatusTooManyRequests, header)
	}

	clock.Advance(maxFreeze + 10*time.Second)
	before := len(clock.Waits())
	if err := l.Wait(context.Background(), book); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != before {
		t.Errorf("still frozen after every deadline passed: %v", got[before:])
	}
}

// TestRefusalOnUnmatchedEndpointIsHarmless checks a 429 for a path no rule
// covers. Freezing needs a window to freeze, and a rule table without a
// catch-all — or none at all — has none to offer, so the refusal must be
// dropped rather than indexed for.
func TestRefusalOnUnmatchedEndpointIsHarmless(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "5")
	unknown := Request{Method: http.MethodGet, Path: "/nothing/here"}
	tier := Tier{"test", TokenBucket{Rate: 1, Burst: 2}, TokenBucket{Rate: 1, Burst: 2}}

	l, clock := newTestLimiter(t, nil, tier)
	l.Observe(unknown, http.StatusTooManyRequests, header)
	if err := l.Wait(context.Background(), unknown); err != nil {
		t.Fatal(err)
	}
	if got := clock.Waits(); len(got) != 0 {
		t.Errorf("a limiter with no rules paced an unknown endpoint: %v", got)
	}

	l2, clock2 := newTestLimiter(t, RelayerRateLimits, tier)
	l2.Observe(unknown, http.StatusTooManyRequests, header)
	if err := l2.Wait(context.Background(), unknown); err != nil {
		t.Fatal(err)
	}
	// The rule that does exist must be neither frozen nor forgotten.
	if err := l2.Wait(context.Background(), Request{Method: http.MethodPost, Path: "/submit"}); err != nil {
		t.Fatal(err)
	}
	if got := clock2.Waits(); len(got) != 0 {
		t.Errorf("a refusal on an unmatched path froze a rule it never matched: %v", got)
	}
}

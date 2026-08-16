// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"net/http"
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
	clock.Advance(10 * time.Second)
	l.after = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
	}
	before := len(clock.Waits())
	if err := l.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(clock.Waits()) != before {
		t.Error("the refunded slot was still counted against the window")
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

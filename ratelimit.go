// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Polymarket limits requests in two independent ways, and this file
// implements both.
//
// The first is Cloudflare's, counted per IP and per endpoint over a sliding
// window: so many requests every ten seconds. Exceeding it gets requests
// throttled rather than refused.
//
// The second applies to order and cancel requests only. It is a token bucket
// per signing address, with a refill rate and a burst capacity that depend on
// the account's trading volume, and a batch costs one token per entry.
//
// A local limiter is a prediction, not the authority. The IP-based limit is
// shared with every other process behind the same address, so a client that
// paces itself perfectly can still be throttled by a colleague. When the
// exchange reports a balance in a response header, that number wins over
// anything computed here.

// A RateLimit is a sliding-window allowance: at most Requests in any period
// of Window.
type RateLimit struct {
	Requests int
	Window   time.Duration
}

// A TokenBucket is an allowance that refills continuously. Rate is tokens per
// second and Burst is the most a bucket can hold, so a full bucket can spend
// Burst at once and then settles to Rate.
type TokenBucket struct {
	Rate  float64
	Burst int
}

// A Limiter paces requests before they are sent.
//
// Wait blocks until the request may go, or returns the context's error.
// Observe feeds each response back so the limiter can defer to the server's
// own accounting, which is authoritative.
//
// An implementation must be safe for concurrent use.
type Limiter interface {
	Wait(ctx context.Context, r Request) error
	Observe(r Request, statusCode int, header http.Header)
}

// A Rule is one endpoint's limit. Method restricts it to a single HTTP
// method; an empty Method matches any. Prefix matches the start of a request
// path, and the longest matching prefix wins, so a rule for "/data/orders"
// takes precedence over one for "/orders".
type Rule struct {
	Method string
	Prefix string
	Limit  RateLimit
}

// Rate limits Polymarket publishes for its own hosts, transcribed from
// https://docs.polymarket.com/api-reference/rate-limits.
//
// The first entry of each table is the general limit for the host, matched by
// the empty prefix; everything after it is an endpoint the exchange singles
// out for something stricter.
var (
	// CLOBRateLimits are the limits on the order book host.
	CLOBRateLimits = []Rule{
		{Prefix: "/", Limit: RateLimit{9000, 10 * time.Second}},

		{Prefix: "/ok", Limit: RateLimit{100, 10 * time.Second}},

		{Prefix: "/book", Limit: RateLimit{1500, 10 * time.Second}},
		{Prefix: "/books", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/price", Limit: RateLimit{1500, 10 * time.Second}},
		{Prefix: "/prices", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/prices-history", Limit: RateLimit{1000, 10 * time.Second}},
		{Prefix: "/midpoint", Limit: RateLimit{1500, 10 * time.Second}},
		{Prefix: "/midpoints", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/tick-size", Limit: RateLimit{200, 10 * time.Second}},

		{Method: http.MethodGet, Prefix: "/balance-allowance", Limit: RateLimit{200, 10 * time.Second}},
		{Prefix: "/balance-allowance/update", Limit: RateLimit{50, 10 * time.Second}},

		{Prefix: "/data/orders", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/data/trades", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/notifications", Limit: RateLimit{125, 10 * time.Second}},
		{Prefix: "/auth/", Limit: RateLimit{100, 10 * time.Second}},

		// Trading. The exchange also applies a sustained limit over ten
		// minutes; these are the burst figures, which bind first.
		{Method: http.MethodPost, Prefix: "/order", Limit: RateLimit{5000, 10 * time.Second}},
		{Method: http.MethodDelete, Prefix: "/order", Limit: RateLimit{5000, 10 * time.Second}},
		{Method: http.MethodPost, Prefix: "/orders", Limit: RateLimit{2000, 10 * time.Second}},
		{Method: http.MethodDelete, Prefix: "/orders", Limit: RateLimit{2000, 10 * time.Second}},
		{Prefix: "/cancel-all", Limit: RateLimit{250, 10 * time.Second}},
		{Prefix: "/cancel-market-orders", Limit: RateLimit{1500, 10 * time.Second}},
	}

	// GammaRateLimits are the limits on the metadata host.
	GammaRateLimits = []Rule{
		{Prefix: "/", Limit: RateLimit{4000, 10 * time.Second}},
		{Prefix: "/events", Limit: RateLimit{500, 10 * time.Second}},
		{Prefix: "/markets", Limit: RateLimit{300, 10 * time.Second}},
		{Prefix: "/comments", Limit: RateLimit{200, 10 * time.Second}},
		{Prefix: "/tags", Limit: RateLimit{200, 10 * time.Second}},
		{Prefix: "/public-search", Limit: RateLimit{350, 10 * time.Second}},
	}

	// DataRateLimits are the limits on the portfolio host.
	DataRateLimits = []Rule{
		{Prefix: "/", Limit: RateLimit{1000, 10 * time.Second}},
		{Prefix: "/ok", Limit: RateLimit{100, 10 * time.Second}},
		{Prefix: "/trades", Limit: RateLimit{200, 10 * time.Second}},
		{Prefix: "/positions", Limit: RateLimit{150, 10 * time.Second}},
		{Prefix: "/closed-positions", Limit: RateLimit{150, 10 * time.Second}},
	}

	// BridgeRateLimits are the limits on the bridge host. This client does not
	// wrap the bridge yet; the figure is here so that a caller reaching it
	// through Session.Do is paced correctly.
	BridgeRateLimits = []Rule{
		{Prefix: "/", Limit: RateLimit{50, 10 * time.Second}},
	}

	// RelayerRateLimits are the limits on the relayer host, whose /submit
	// endpoint is far stricter than anything else Polymarket publishes.
	RelayerRateLimits = []Rule{
		{Prefix: "/submit", Limit: RateLimit{25, time.Minute}},
	}
)

// Hosts this client knows limits for.
const (
	// BridgeHost is Polymarket's bridge. Not wrapped by this client.
	BridgeHost = "https://bridge.polymarket.com"
	// RelayerHost is Polymarket's transaction relayer. Not wrapped by this
	// client.
	RelayerHost = "https://relayer-v2.polymarket.com"
)

// RateLimitsFor returns the published limits for a Polymarket host, and
// reports whether any are known. An unrecognised host — a proxy, a mock —
// gets none, because guessing a limit for somebody else's server is worse
// than not pacing at all.
func RateLimitsFor(host string) ([]Rule, bool) {
	switch strings.TrimSuffix(host, "/") {
	case DefaultHost:
		return CLOBRateLimits, true
	case GammaHost:
		return GammaRateLimits, true
	case DataHost:
		return DataRateLimits, true
	case BridgeHost:
		return BridgeRateLimits, true
	case RelayerHost:
		return RelayerRateLimits, true
	}
	return nil, false
}

// A Tier is a per-signer trading allowance. Polymarket assigns one from the
// account's thirty-day volume and refreshes it every few hours.
type Tier struct {
	Name   string
	Order  TokenBucket
	Cancel TokenBucket
}

// The published trading tiers. Standard is what an account starts on.
var (
	TierStandard = Tier{"standard", TokenBucket{40, 60}, TokenBucket{80, 120}}
	TierCopper   = Tier{"copper", TokenBucket{60, 90}, TokenBucket{120, 180}}
	TierBronze   = Tier{"bronze", TokenBucket{80, 120}, TokenBucket{160, 240}}
	TierSilver   = Tier{"silver", TokenBucket{200, 300}, TokenBucket{400, 600}}
	TierGold     = Tier{"gold", TokenBucket{400, 600}, TokenBucket{800, 1200}}
	TierPlatinum = Tier{"platinum", TokenBucket{450, 675}, TokenBucket{900, 1350}}
	TierDiamond  = Tier{"diamond", TokenBucket{525, 787}, TokenBucket{1050, 1575}}
	TierElite    = Tier{"elite", TokenBucket{600, 900}, TokenBucket{1200, 1800}}
)

// WithLimiter installs a Limiter, replacing the default. A nil Limiter turns
// pacing off entirely, which is the right choice when something upstream —
// a shared gateway, a proxy — already paces for the whole fleet.
func WithLimiter(l Limiter) Option {
	return func(s *Session) { s.limiter = l; s.limiterSet = true }
}

// WithTradingTier tells the default limiter which per-signer trading tier the
// account is on. The default is TierStandard, the slowest; an account with
// volume that has been promoted should say so, or it paces itself far below
// what it is allowed.
//
// The tier the exchange actually applied comes back on every order response
// in the Poly-RateLimit-Tier header, and the default limiter adopts it.
func WithTradingTier(t Tier) Option {
	return func(s *Session) { s.tradingTier = t; s.tierSet = true }
}

// NewLimiter returns the default limiter for a set of rules and a trading
// tier. Rules may be nil, in which case only the per-signer trading buckets
// apply.
func NewLimiter(rules []Rule, tier Tier) *StandardLimiter {
	l := &StandardLimiter{
		rules:  rules,
		tier:   tier,
		now:    time.Now,
		after:  time.After,
		window: make(map[int]*slidingWindow, len(rules)),
	}
	l.order = newTokenBucketState(tier.Order)
	l.cancel = newTokenBucketState(tier.Cancel)
	return l
}

// A StandardLimiter paces requests against Polymarket's published limits: a
// sliding window per endpoint, and a token bucket per signer for orders and
// cancellations.
//
// It is safe for concurrent use. Waiting happens outside its lock, so a
// request queued behind a full bucket does not stall requests to other
// endpoints.
type StandardLimiter struct {
	rules []Rule
	tier  Tier

	// now and after are injectable so tests can drive a fake clock rather
	// than sleep through a ten-second window.
	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	mu     sync.Mutex
	window map[int]*slidingWindow
	order  *tokenBucketState
	cancel *tokenBucketState

	// warnings counts responses carrying Poly-RateLimit-Warning, which the
	// exchange sends while the per-signer limiter runs in warning mode.
	warnings int64
}

// Warnings reports how many responses arrived carrying the exchange's
// rate-limit warning header, which marks a request that would have been
// rejected once the per-signer limiter is enforced. A number that climbs is
// a signal to slow down before enforcement begins.
func (l *StandardLimiter) Warnings() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warnings
}

// Wait blocks until the request may be sent.
func (l *StandardLimiter) Wait(ctx context.Context, r Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cost := r.Cost
	if cost < 1 {
		cost = 1
	}

	// Reserve in both layers under one lock, then sleep outside it. Holding
	// the lock across the sleep would let one saturated endpoint block every
	// other endpoint's requests.
	l.mu.Lock()
	now := l.now()
	when := now

	idx, ok := l.match(r.Method, r.Path)
	var win *slidingWindow
	// slot names the reservation the window handed out, which is what must be
	// given back on cancellation. Naming it is not a convenience: the trading
	// bucket below may push the send past the window's own time, so a refund
	// described by when would be asking the window to give back a slot it
	// never issued.
	var slot windowReservation
	if ok {
		win = l.windowFor(idx)
		var t time.Time
		if t, slot = win.reserve(now); t.After(when) {
			when = t
		}
	}

	bucket := l.bucketFor(r.Method, r.Path)
	if bucket != nil {
		if t := bucket.reserve(now, float64(cost)); t.After(when) {
			when = t
		}
	}
	l.mu.Unlock()

	delay := when.Sub(now)
	if delay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		// The request will not be sent, so hand the reservations back.
		// Without this a storm of cancellations drains a bucket that
		// nothing ever used.
		l.mu.Lock()
		if win != nil {
			win.release(slot)
		}
		if bucket != nil {
			bucket.release(float64(cost))
		}
		l.mu.Unlock()
		return ctx.Err()
	case <-l.after(delay):
		return nil
	}
}

// Observe adopts what the exchange says about the account's remaining
// allowance, which outranks anything predicted locally.
func (l *StandardLimiter) Observe(r Request, statusCode int, header http.Header) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if header.Get("Poly-RateLimit-Warning") == "true" {
		l.warnings++
	}

	if remaining := header.Get("Poly-RateLimit-Remaining"); remaining != "" {
		if v, err := strconv.ParseFloat(remaining, 64); err == nil {
			if b := l.bucketFor(r.Method, r.Path); b != nil {
				b.adopt(v, l.now())
			}
		}
	}

	// Only a refusal freezes anything. On an admitted request the exchange
	// documents Poly-RateLimit-Reset as possibly being the current instant,
	// so treating it as a deadline would stall the client after every
	// successful call.
	if statusCode != http.StatusTooManyRequests {
		return
	}
	until, ok := l.retryAfter(header)
	if !ok {
		return
	}
	if idx, matched := l.match(r.Method, r.Path); matched {
		l.windowFor(idx).freeze(until)
	}
	if b := l.bucketFor(r.Method, r.Path); b != nil {
		b.freeze(until)
	}
}

// maxFreeze is the longest a response header may hold this client back.
//
// The deadline in a refusal is somebody else's arithmetic. A server that is
// broken, hostile, or merely counting from an epoch this client does not
// share can name an instant centuries away — a Retry-After of a few billion
// seconds, a Poly-RateLimit-Reset that is milliseconds rather than seconds —
// and every request after it waits until the process is restarted. Honouring
// the cap instead costs a handful of refusals an hour, which is nothing.
// Waiting longer than this is a decision for the caller and its context, not
// for a header.
const maxFreeze = 5 * time.Minute

// retryAfter reads whichever delay header the response carried, bounded by
// maxFreeze.
func (l *StandardLimiter) retryAfter(header http.Header) (time.Time, bool) {
	now := l.now()
	if v := header.Get("Retry-After"); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs >= 0 {
			// Compared before multiplying: seconds enough to overflow a
			// Duration would otherwise wrap to a deadline in the past and
			// silently cancel the backoff. Sixty-four bits whatever the
			// platform's int is, so the same header means the same thing
			// everywhere.
			d := maxFreeze
			if secs < int64(maxFreeze/time.Second) {
				d = time.Duration(secs) * time.Second
			}
			return now.Add(d), true
		}
		if t, err := http.ParseTime(v); err == nil {
			return clampFreeze(now, t), true
		}
	}
	if v := header.Get("Poly-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			return clampFreeze(now, time.Unix(unix, 0)), true
		}
	}
	return time.Time{}, false
}

// clampFreeze bounds a deadline the server named. See maxFreeze.
func clampFreeze(now, until time.Time) time.Time {
	if latest := now.Add(maxFreeze); until.After(latest) {
		return latest
	}
	return until
}

// match returns the index of the most specific rule for a request. The
// longest matching prefix wins, and a rule naming a method beats one that
// does not, so DELETE /order and POST /order are paced separately while
// /data/orders does not fall into the rule for /orders.
func (l *StandardLimiter) match(method, path string) (int, bool) {
	best, bestLen, found := 0, -1, false
	for i, rule := range l.rules {
		if rule.Method != "" && rule.Method != method {
			continue
		}
		if !strings.HasPrefix(path, rule.Prefix) {
			continue
		}
		score := len(rule.Prefix)
		if rule.Method != "" {
			// Break a tie in favour of the method-specific rule.
			score = score*2 + 1
		} else {
			score = score * 2
		}
		if score > bestLen {
			best, bestLen, found = i, score, true
		}
	}
	return best, found
}

func (l *StandardLimiter) windowFor(idx int) *slidingWindow {
	w, ok := l.window[idx]
	if !ok {
		w = newSlidingWindow(l.rules[idx].Limit)
		l.window[idx] = w
	}
	return w
}

// bucketFor returns the per-signer bucket a request spends from, or nil when
// the request is neither an order nor a cancellation.
func (l *StandardLimiter) bucketFor(method, path string) *tokenBucketState {
	switch {
	case method == http.MethodPost && (path == "/order" || path == "/orders"):
		return l.order
	case method == http.MethodDelete &&
		(path == "/order" || path == "/orders" ||
			path == "/cancel-all" || path == "/cancel-market-orders"):
		return l.cancel
	}
	return nil
}

// A windowSlot is one remembered send: the instant it is scheduled for, and
// the number of the reservation holding it. A zero when is a free slot.
type windowSlot struct {
	when time.Time
	seq  uint64
}

// A windowReservation is one request's claim on a slot, kept so that a request
// that never goes can put the window back exactly as it found it.
//
// It names the slot rather than the instant because two reservations may
// legitimately be scheduled for the same instant — several requests behind one
// freeze all share its deadline — so a time does not identify a claim. It
// carries the slot's previous contents rather than merely blanking on release
// because a claim may have displaced a send that is still inside the window,
// and forgetting that send admits one request too many. And seq settles the
// case where the slot has since been claimed again: the refund is dropped,
// because the new claim's time is never earlier than what would be restored.
type windowReservation struct {
	slot int
	seq  uint64
	prev windowSlot
}

// A slidingWindow admits at most Requests sends in any Window, by remembering
// when the last Requests sends were scheduled for. Reserving takes a free
// slot, or displaces the oldest send and queues behind it.
type slidingWindow struct {
	limit RateLimit
	// slots is an unordered set of scheduled send times. Position carries no
	// meaning, which is what lets an abandoned request hand its own slot back
	// without disturbing any other. Finding the oldest costs a scan of
	// Requests entries, which is nothing beside the network call it paces.
	slots      []windowSlot
	seq        uint64
	frozenTill time.Time
}

func newSlidingWindow(limit RateLimit) *slidingWindow {
	if limit.Requests < 1 {
		limit.Requests = 1
	}
	return &slidingWindow{limit: limit, slots: make([]windowSlot, limit.Requests)}
}

// reserve claims a slot and returns when it may be used, along with the claim
// itself so that an abandoned request can give it back.
func (w *slidingWindow) reserve(now time.Time) (time.Time, windowReservation) {
	// A free slot means the window has room. Otherwise the oldest send is the
	// first to leave the window, so it is the one to queue behind.
	at := 0
	for i, s := range w.slots {
		if s.when.IsZero() {
			at = i
			break
		}
		if s.when.Before(w.slots[at].when) {
			at = i
		}
	}

	when := now
	if oldest := w.slots[at].when; !oldest.IsZero() {
		if t := oldest.Add(w.limit.Window); t.After(when) {
			when = t
		}
	}
	if w.frozenTill.After(when) {
		when = w.frozenTill
	}

	w.seq++
	res := windowReservation{slot: at, seq: w.seq, prev: w.slots[at]}
	w.slots[at] = windowSlot{when: when, seq: w.seq}
	return when, res
}

// release gives a slot back after its request was abandoned, restoring
// whatever the claim displaced. That is exactly the effect of a send that
// never happened: the window forgets the request, and remembers again the
// older one it had been queueing behind.
func (w *slidingWindow) release(res windowReservation) {
	if res.seq == 0 || w.slots[res.slot].seq != res.seq {
		return
	}
	w.slots[res.slot] = res.prev
}

// freeze holds the window shut until t, after a refusal.
func (w *slidingWindow) freeze(t time.Time) {
	if t.After(w.frozenTill) {
		w.frozenTill = t
	}
}

// A tokenBucketState is a bucket that refills continuously. Its balance may
// go negative: reserving more than is available schedules the request for
// when the balance would have recovered, which is what the exchange itself
// does for a cancel-all whose true cost is not known in advance.
type tokenBucketState struct {
	bucket     TokenBucket
	tokens     float64
	updated    time.Time
	frozenTill time.Time
}

func newTokenBucketState(b TokenBucket) *tokenBucketState {
	if b.Rate <= 0 {
		b.Rate = 1
	}
	if b.Burst < 1 {
		b.Burst = 1
	}
	return &tokenBucketState{bucket: b, tokens: float64(b.Burst)}
}

func (b *tokenBucketState) refill(now time.Time) {
	if b.updated.IsZero() {
		b.updated = now
		return
	}
	if elapsed := now.Sub(b.updated); elapsed > 0 {
		b.tokens = math.Min(float64(b.bucket.Burst), b.tokens+elapsed.Seconds()*b.bucket.Rate)
		b.updated = now
	}
}

// reserve spends cost tokens and returns when the request may go.
func (b *tokenBucketState) reserve(now time.Time, cost float64) time.Time {
	b.refill(now)
	when := now
	if b.tokens < cost {
		deficit := cost - b.tokens
		when = now.Add(time.Duration(deficit / b.bucket.Rate * float64(time.Second)))
	}
	b.tokens -= cost
	if b.frozenTill.After(when) {
		when = b.frozenTill
	}
	return when
}

// release refunds a reservation whose request was abandoned.
func (b *tokenBucketState) release(cost float64) {
	b.tokens = math.Min(float64(b.bucket.Burst), b.tokens+cost)
}

// adopt takes the exchange's own count of remaining tokens, which is
// authoritative: it accounts for other processes using the same signer, for
// costs this client cannot predict, and for clock drift.
//
// Authoritative is not the same as unchecked. A balance of NaN would be
// permanent: it survives every later refill, and it compares false against
// every cost, so the bucket would admit everything for the life of the
// process. A balance of minus infinity, or merely an absurd one, would put
// the next order centuries out. The deepest hole worth believing is one
// burst, which the tier's own rate refills in a bounded time.
func (b *tokenBucketState) adopt(remaining float64, now time.Time) {
	if math.IsNaN(remaining) {
		return
	}
	burst := float64(b.bucket.Burst)
	b.tokens = math.Min(burst, math.Max(-burst, remaining))
	b.updated = now
}

// freeze holds the bucket shut until t.
func (b *tokenBucketState) freeze(t time.Time) {
	if t.After(b.frozenTill) {
		b.frozenTill = t
	}
}

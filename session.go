// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A Session is what every Polymarket client is built on: where requests go,
// how they travel, and who is sending them. The clob, gamma and data packages
// each wrap one.
//
// Construct it with NewSession, or let a client package construct its own —
// clob.New and its siblings take the same options.
//
// A Session is safe for concurrent use. Credentials are the exception: they
// are set once, usually by the level-1 handshake, and read thereafter.
type Session struct {
	host       string
	httpClient *http.Client
	signer     Signer
	creds      *APICreds
	l2         L2Authenticator
	chainID    int64
	userAgent  string
	retries    int
	jar        http.CookieJar

	limiter     Limiter
	limiterSet  bool
	tradingTier Tier
	tierSet     bool
}

// An Option configures a Session. The client packages re-export these, so
// clob.WithSigner and polymarket.WithSigner are the same thing.
type Option func(*Session)

// WithHost overrides the API host, for a proxy or a test server.
func WithHost(host string) Option {
	return func(s *Session) { s.host = strings.TrimSuffix(host, "/") }
}

// WithHTTPClient supplies the http.Client that issues requests. Use it to set
// a timeout, a transport, or a proxy. The default allows 30 seconds.
func WithHTTPClient(c *http.Client) Option {
	return func(s *Session) { s.httpClient = c }
}

// WithSigner supplies the wallet key. It is needed to obtain credentials and
// to sign orders, and is unused once credentials exist.
func WithSigner(signer Signer) Option {
	return func(s *Session) { s.signer = signer }
}

// WithCredentials supplies level-2 credentials directly, skipping the
// level-1 handshake. Use it to reuse a key across processes.
func WithCredentials(creds APICreds) Option {
	return func(s *Session) {
		s.creds = &creds
		s.l2 = creds
	}
}

// WithL2Authenticator supplies level-2 authentication without the secret.
//
// It is the option for credentials this process is not allowed to hold: the
// authenticator signs each request wherever the secret actually lives. See
// L2Authenticator, which documents what it can and cannot cover.
//
// Credentials reports nil for a session configured this way, because there
// are none here to report. Everything that needs the key alone — an order's
// attribution — reads it from the authenticator.
func WithL2Authenticator(a L2Authenticator) Option {
	return func(s *Session) { s.l2 = a }
}

// WithCookieJar gives the session a cookie jar.
//
// Only one flow here uses cookies: the sign-in-with-Ethereum handshake that
// mints a relayer API key. Sessions have no jar by default, so nothing stores
// a cookie unless a caller asks for it.
//
// The jar is applied by the session itself rather than by the http.Client, so
// supplying one does not disturb a client passed to WithHTTPClient. Sharing
// one jar between a Gamma session and a relayer session is what carries a
// login from one host to the other: Polymarket scopes the session cookie to
// polymarket.com, so the jar hands it to both.
func WithCookieJar(jar http.CookieJar) Option {
	return func(s *Session) { s.jar = jar }
}

// NewCookieJar returns a jar suitable for WithCookieJar.
func NewCookieJar() (http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("polymarket: creating cookie jar: %w", err)
	}
	return jar, nil
}

// WithChainID selects the chain whose exchange contracts orders are signed
// against. The default is ChainPolygon.
func WithChainID(chainID int64) Option {
	return func(s *Session) { s.chainID = chainID }
}

// WithUserAgent sets the User-Agent header. Identify your application here;
// the default names this package.
func WithUserAgent(ua string) Option {
	return func(s *Session) { s.userAgent = ua }
}

// WithRetries bounds how many extra attempts a read gets after a connection
// failure. A negative value disables retrying. Writes are never retried
// whatever this says.
func WithRetries(n int) Option {
	return func(s *Session) { s.retries = n }
}

const (
	defaultUserAgent = "go-polymarket"
	defaultTimeout   = 30 * time.Second

	// defaultRetries is how many extra attempts a failed read gets.
	//
	// Only reads are retried, and only when the request never reached an
	// answer — a refused dial, a reset, a truncated response. A request the
	// server answered is never retried whatever it answered, and a write is
	// never retried at all: resending an order submission that may already
	// have been received is how an account ends up holding a position it
	// asked for once.
	defaultRetries = 2
)

// NewSession returns a Session for one host.
//
// Callers normally use clob.New, gamma.New or data.New instead, which pick
// the right host and accept these same options.
func NewSession(host string, opts ...Option) *Session {
	s := &Session{
		host:      strings.TrimSuffix(host, "/"),
		chainID:   ChainPolygon,
		userAgent: defaultUserAgent,
		retries:   defaultRetries,
	}
	for _, opt := range opts {
		opt(s)
	}
	if !s.tierSet {
		s.tradingTier = TierStandard
	}
	if !s.limiterSet {
		// Limits are keyed to the host this session was constructed for,
		// not to one a WithHost option may have redirected to: a proxy in
		// front of the CLOB still consumes the CLOB's allowance, and an
		// unrecognised host gets no pacing rather than a guessed one.
		if rules, ok := RateLimitsFor(host); ok {
			s.limiter = NewLimiter(rules, s.tradingTier)
		}
	}
	return s
}

// CookieJar reports the session's cookie jar, or nil when it has none. Pass
// it to another session's WithCookieJar to share one login between hosts.
func (s *Session) CookieJar() http.CookieJar { return s.jar }

// Limiter reports the limiter pacing this session, or nil when pacing is off.
func (s *Session) Limiter() Limiter { return s.limiter }

// Host reports where this session sends requests.
func (s *Session) Host() string { return s.host }

// ChainID reports the chain whose contracts orders are signed against.
func (s *Session) ChainID() int64 { return s.chainID }

// Signer reports the wallet key, or nil when the session has none.
func (s *Session) Signer() Signer { return s.signer }

// Credentials reports the level-2 credentials, or nil when the session has
// none of its own: it never had any, or it authenticates through an
// L2Authenticator that holds them elsewhere.
func (s *Session) Credentials() *APICreds { return s.creds }

// SetCredentials adopts level-2 credentials, as the level-1 handshake does on
// success.
func (s *Session) SetCredentials(creds APICreds) {
	s.creds = &creds
	s.l2 = creds
}

// Authenticator reports what signs this session's level-2 requests, or nil
// when nothing does yet.
func (s *Session) Authenticator() L2Authenticator { return s.l2 }

// SetAuthenticator adopts an L2Authenticator, for a session whose credentials
// arrive after construction.
//
// Call it before the first authenticated request. A Session is otherwise safe
// for concurrent use; this is not, and neither is SetCredentials.
func (s *Session) SetAuthenticator(a L2Authenticator) { s.l2 = a }

// APIKey reports the level-2 key this session authenticates with, or "" when
// it has none. It is not a secret: it identifies which key signed, and an
// order carries it as its owner.
func (s *Session) APIKey() string {
	if s.l2 == nil {
		return ""
	}
	return s.l2.APIKey()
}

// An AuthLevel selects how a request proves who is sending it.
type AuthLevel int

const (
	// AuthNone is a public endpoint.
	AuthNone AuthLevel = iota
	// AuthL1 proves control of the wallet with an EIP-712 signature. Only
	// the key-management endpoints use it.
	AuthL1
	// AuthL2 signs the request with the API credentials.
	AuthL2
)

// A Request is one API call. The client packages build these; it is exported
// so an endpoint this library has not wrapped yet is still reachable.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Path is the path on the session's host, starting with a slash.
	Path string
	// Query is appended to the path. It is deliberately NOT covered by the
	// level-2 signature; see the note on signing below.
	Query url.Values
	// Body is marshalled to JSON when not nil.
	Body any
	// Auth selects the authentication level.
	Auth AuthLevel
	// Out receives the decoded response. Leave it nil to discard the body.
	Out any

	// Headers are extra request headers, for an endpoint that takes one that
	// is not authentication — a builder code on a bridge deposit, say.
	//
	// They are applied before the authentication headers, so a Headers entry
	// can never displace a signature. Like the query string they are outside
	// the level-2 signature.
	Headers map[string]string

	// Cost is what this request spends from the per-signer trading
	// allowance. Zero means one, so an ordinary request needs no thought;
	// a batch costs one per entry, and a cancellation that removes several
	// orders costs one per order removed.
	Cost int

	// Nonce selects between the several key pairs one wallet may hold. It
	// applies to AuthL1 alone — the level-1 struct carries a nonce field and
	// the level-2 HMAC does not — and zero is the default every wallet's
	// first key is minted under.
	//
	// It has to live on the Request and not only in Query because the nonce
	// is SIGNED: it is a uint256 field of the level-1 EIP-712 struct, so the
	// exchange keys off the signed value and ignores what the query says.
	// Setting one without the other is how a caller silently gets the nonce-0
	// key back while believing it asked for another.
	Nonce int64
}

// Do issues a request and decodes its response.
//
// The level-2 signature covers the method, the path and the body, but NOT the
// query string. That is the exchange's rule, not a shortcut: the official
// client signs one set of headers and then reuses them across every page of a
// pagination loop while the cursor changes underneath. Signing the query
// instead returns 401 for every filtered or paginated call.
func (s *Session) Do(ctx context.Context, r Request) error {
	var body []byte
	if r.Body != nil {
		var err error
		if body, err = json.Marshal(r.Body); err != nil {
			return fmt.Errorf("polymarket: encoding request body: %w: %w", err, ErrNotSent)
		}
	}

	headers, err := s.authHeaders(r.Auth, r.Method, r.Path, string(body), r.Nonce)
	if err != nil {
		return err
	}

	target := s.host + r.Path
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, reader)
	if err != nil {
		return fmt.Errorf("polymarket: building request: %w: %w", err, ErrNotSent)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	// Authentication last: a caller's header may add to a request but must
	// never replace the thing that proves who is sending it.
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if s.jar != nil {
		for _, c := range s.jar.Cookies(req.URL) {
			req.AddCookie(c)
		}
	}

	if s.limiter != nil {
		if err := s.limiter.Wait(ctx, r); err != nil {
			return fmt.Errorf("polymarket: %s %s: %w: %w", r.Method, r.Path, err, ErrNotSent)
		}
	}

	resp, err := s.send(ctx, req, body)
	if err != nil {
		return fmt.Errorf("polymarket: %s %s: %w", r.Method, r.Path, err)
	}
	defer resp.Body.Close()

	if s.jar != nil {
		s.jar.SetCookies(req.URL, resp.Cookies())
	}

	if s.limiter != nil {
		s.limiter.Observe(r, resp.StatusCode, resp.Header)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("polymarket: reading %s %s: %w", r.Method, r.Path, err)
	}
	if resp.StatusCode >= 300 {
		return &Error{
			Method:     r.Method,
			URL:        r.Path,
			StatusCode: resp.StatusCode,
			Message:    errorMessage(data),
			Body:       truncate(string(data), 2048),
		}
	}
	if r.Out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, r.Out); err != nil {
		return fmt.Errorf("polymarket: decoding %s %s: %w (body %s)",
			r.Method, r.Path, err, truncate(string(data), 256))
	}
	return nil
}

// Get issues an unauthenticated GET.
func (s *Session) Get(ctx context.Context, path string, q url.Values, out any) error {
	return s.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: q, Out: out})
}

// GetL2 issues a GET signed with the level-2 credentials.
func (s *Session) GetL2(ctx context.Context, path string, q url.Values, out any) error {
	return s.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: q, Auth: AuthL2, Out: out})
}

// PostL2 issues a POST signed with the level-2 credentials.
func (s *Session) PostL2(ctx context.Context, path string, body, out any) error {
	return s.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body, Auth: AuthL2, Out: out})
}

// DeleteL2 issues a DELETE signed with the level-2 credentials.
func (s *Session) DeleteL2(ctx context.Context, path string, body, out any) error {
	return s.Do(ctx, Request{Method: http.MethodDelete, Path: path, Body: body, Auth: AuthL2, Out: out})
}

// send issues a request, retrying a read that never reached an answer.
func (s *Session) send(ctx context.Context, req *http.Request, body []byte) (*http.Response, error) {
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}

	attempts := s.retries
	if attempts < 0 || req.Method != http.MethodGet {
		attempts = 0
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			// A short, growing pause: the failures worth retrying are
			// transient network ones, and hammering helps neither side.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
			if body != nil {
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt >= attempts || ctx.Err() != nil {
			return nil, lastErr
		}
	}
}

// authHeaders builds the headers for a request's authentication level.
// nonce applies to AuthL1 only. The level-1 struct signs it; the level-2 HMAC
// has no such field, so a nonce cannot be expressed at that level and is not
// silently accepted there.
func (s *Session) authHeaders(level AuthLevel, method, path, body string, nonce int64) (map[string]string, error) {
	switch level {
	case AuthNone:
		return nil, nil

	case AuthL1:
		if s.signer == nil {
			return nil, ErrNoSigner
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		h, err := BuildL1Headers(s.signer, s.chainID, ts, nonce)
		if err != nil {
			return nil, err
		}
		return h.Header(), nil

	case AuthL2:
		if s.l2 == nil {
			return nil, ErrNoCredentials
		}
		// POLY_ADDRESS names the wallet, not the credential, so a level-2
		// request needs the Signer even when the secret lives elsewhere.
		if s.signer == nil {
			return nil, ErrNoSigner
		}
		h, err := s.l2.AuthHeaders(s.signer.Address(), method, path, body)
		if err != nil {
			return nil, err
		}
		return h.Header(), nil
	}
	return nil, fmt.Errorf("polymarket: unknown authentication level %d", level)
}

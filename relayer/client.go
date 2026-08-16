// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package relayer is a client for Polymarket's transaction relayer
// (RelayerHost). The relayer puts a user's transaction on chain for them: the
// user signs an operation for their proxy or Safe wallet off chain, and the
// relayer broadcasts it and pays the gas.
//
// This package wraps the relayer's read endpoints. Four of them are public —
// Nonce, RelayPayload, Deployed and Transaction need no credentials at all.
// Two are authenticated: Transactions reports the caller's own transactions,
// and APIKeys lists the caller's relayer API keys.
//
// The relayer has two credential schemes, and they are not interchangeable
// per endpoint:
//
//   - A relayer API key (WithAPIKey) is a static pair — a UUID and the
//     address that owns it — sent unchanged on every request. It works on
//     both authenticated endpoints.
//   - Builder credentials (WithBuilderCredentials) are a key, a secret and a
//     passphrase, and each request carries a fresh HMAC signature. The
//     relayer accepts them on Transactions; it does not list them for
//     APIKeys.
//
// Both are secrets. Keep them in the environment, never in a source file.
//
// Submitting a transaction is not part of this package.
package relayer

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A Client talks to the Polymarket transaction relayer.
type Client struct {
	session *polymarket.Session

	// auth renders the credential headers for the authenticated endpoints.
	// It is nil on a Client built without credentials, which is the normal
	// state for a caller that only reads the public endpoints.
	auth Authenticator
}

// New returns a client for the transaction relayer.
func New(opts ...Option) *Client {
	var cfg config
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	sessionOpts := cfg.session
	if hc := cfg.httpClient(); hc != nil {
		sessionOpts = append(sessionOpts, polymarket.WithHTTPClient(hc))
	}
	return &Client{
		session: polymarket.NewSession(polymarket.RelayerHost, sessionOpts...),
		auth:    cfg.auth,
	}
}

// NewWithSession returns a client that shares an existing Session, so one
// wallet and one http.Client can serve several API packages.
//
// The client it returns reads the public endpoints only. Credentials travel
// on a transport New installs, which a Session built elsewhere does not
// carry, so Transactions and APIKeys report ErrNoCredentials on it. Use New
// with WithAPIKey or WithBuilderCredentials to authenticate.
func NewWithSession(s *polymarket.Session) *Client { return &Client{session: s} }

// An Option configures a Client.
//
// The options every Polymarket client package takes are re-exported below
// under their usual names, so relayer.WithHost and gamma.WithHost do the same
// thing. Option is this package's own type rather than an alias for
// polymarket.Option because the credential options configure the Client
// itself, which a polymarket.Option — a function over a Session — cannot
// reach.
type Option interface {
	apply(*config)
}

// config is the resolved option set New builds a Client from.
type config struct {
	session []polymarket.Option
	client  *http.Client
	auth    Authenticator
}

// httpClient returns the http.Client to install on the session, or nil to
// leave the session its own default.
//
// A Client with credentials needs the header transport wrapped around
// whatever transport it would otherwise use. A caller's own http.Client is
// copied rather than modified: the caller may be sharing it with something
// else that must not start sending relayer credentials.
func (cfg *config) httpClient() *http.Client {
	if cfg.auth == nil {
		return cfg.client
	}
	out := &http.Client{Timeout: defaultTimeout}
	if cfg.client != nil {
		clone := *cfg.client
		out = &clone
	}
	out.Transport = headerTransport{base: out.Transport}
	return out
}

// defaultTimeout matches the timeout the root package gives a session that
// was handed no http.Client. It is repeated here because supplying a client
// at all — which the header transport requires — opts out of that default.
const defaultTimeout = 30 * time.Second

// sessionOption carries one polymarket.Option through to NewSession.
type sessionOption struct {
	opt polymarket.Option
}

func (o sessionOption) apply(cfg *config) { cfg.session = append(cfg.session, o.opt) }

// httpClientOption holds the caller's http.Client until New can wrap it.
type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(cfg *config) { cfg.client = o.client }

// authOption holds the credentials the authenticated endpoints use.
type authOption struct {
	auth Authenticator
}

func (o authOption) apply(cfg *config) { cfg.auth = o.auth }

// WithHost overrides the API host, for a proxy or a test server.
func WithHost(host string) Option { return sessionOption{opt: polymarket.WithHost(host)} }

// WithHTTPClient supplies the http.Client that issues requests. Use it to set
// a timeout, a transport, or a proxy. The default allows 30 seconds.
//
// The client is copied, not modified. On a Client with credentials the copy
// carries an extra transport that adds the credential headers.
func WithHTTPClient(c *http.Client) Option { return httpClientOption{client: c} }

// WithSigner supplies the wallet key. No endpoint in this package uses it;
// it is here so one option set can build every client package.
func WithSigner(signer polymarket.Signer) Option {
	return sessionOption{opt: polymarket.WithSigner(signer)}
}

// WithCredentials supplies the CLOB's level-2 credentials. The relayer does
// not accept them — see WithAPIKey and WithBuilderCredentials — and this
// option is here so one option set can build every client package.
func WithCredentials(creds polymarket.APICreds) Option {
	return sessionOption{opt: polymarket.WithCredentials(creds)}
}

// WithChainID selects the chain whose exchange contracts orders are signed
// against. The default is ChainPolygon.
func WithChainID(chainID int64) Option { return sessionOption{opt: polymarket.WithChainID(chainID)} }

// WithUserAgent sets the User-Agent header. Identify your application here;
// the default names this package.
func WithUserAgent(ua string) Option { return sessionOption{opt: polymarket.WithUserAgent(ua)} }

// WithRetries bounds how many extra attempts a read gets after a connection
// failure. A negative value disables retrying.
func WithRetries(n int) Option { return sessionOption{opt: polymarket.WithRetries(n)} }

// WithAPIKey authenticates with a relayer API key: the static pair a caller
// gets from APIKeys. It is accepted on every authenticated relayer endpoint.
func WithAPIKey(creds APIKeyCredentials) Option { return authOption{auth: creds} }

// WithBuilderCredentials authenticates with builder credentials, signing each
// request. The relayer accepts them on Transactions but does not list them
// for APIKeys, which answers 401 or 403 to a scheme it will not take there.
func WithBuilderCredentials(creds BuilderCredentials) Option { return authOption{auth: creds} }

// WithAuthenticator supplies a custom Authenticator. Use it when the
// credentials live somewhere this package cannot see them — a remote signing
// service that returns the four builder headers, for instance.
func WithAuthenticator(a Authenticator) Option { return authOption{auth: a} }

// ---------------------------------------------------------------------------
// Credentials and header plumbing. This is shared by every authenticated
// relayer call, reads and writes alike.

// An Authenticator renders the headers that prove who is calling an
// authenticated relayer endpoint.
//
// requestPath is the bare path, without a query string, and body is the
// request body or "" when there is none. A static credential ignores both; a
// signing one covers them.
type Authenticator interface {
	AuthHeaders(method, requestPath, body string) (map[string]string, error)
}

// The relayer's credential headers. Both schemes send their whole set
// together: a partial set authenticates as nobody and answers 401.
const (
	headerAPIKey            = "RELAYER_API_KEY"
	headerAPIKeyAddress     = "RELAYER_API_KEY_ADDRESS"
	headerBuilderAPIKey     = "POLY_BUILDER_API_KEY"
	headerBuilderPassphrase = "POLY_BUILDER_PASSPHRASE"
	headerBuilderSignature  = "POLY_BUILDER_SIGNATURE"
	headerBuilderTimestamp  = "POLY_BUILDER_TIMESTAMP"
)

// APIKeyCredentials is a relayer API key and the address that owns it.
//
// The key is a bearer token: it goes on the wire unchanged, with no
// timestamp and no signature, so anyone who reads one can use it until it is
// revoked. Treat it as secret material. Address is not a second secret — it
// tells the relayer which key to look up — but it must be the address the
// key was issued to, or the pair is rejected.
//
// A key can only be created under Polymarket's own account authentication,
// which this package does not implement; APIKeys lists the ones an address
// already has.
type APIKeyCredentials struct {
	Key     string
	Address string
}

// AuthHeaders renders the static header pair. A relayer API key covers
// nothing about the request, so the method, path and body are ignored.
func (c APIKeyCredentials) AuthHeaders(method, requestPath, body string) (map[string]string, error) {
	if c.Key == "" || c.Address == "" {
		return nil, fmt.Errorf("relayer: an API key needs both a key and its owning address")
	}
	return map[string]string{
		headerAPIKey:        c.Key,
		headerAPIKeyAddress: c.Address,
	}, nil
}

// BuilderCredentials are the builder API credentials. All three are secrets.
type BuilderCredentials struct {
	Key        string
	Secret     string
	Passphrase string
}

// AuthHeaders signs one request, timestamped now.
func (c BuilderCredentials) AuthHeaders(method, requestPath, body string) (map[string]string, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h, err := BuildBuilderHeaders(c, ts, method, requestPath, body)
	if err != nil {
		return nil, err
	}
	return h.Header(), nil
}

// BuilderHeaders authenticate one request under the builder scheme.
type BuilderHeaders struct {
	APIKey     string
	Passphrase string
	Signature  string
	Timestamp  string // unix seconds
}

// Header renders the headers under their canonical POLY_BUILDER_ names.
func (h BuilderHeaders) Header() map[string]string {
	return map[string]string{
		headerBuilderAPIKey:     h.APIKey,
		headerBuilderPassphrase: h.Passphrase,
		headerBuilderSignature:  h.Signature,
		headerBuilderTimestamp:  h.Timestamp,
	}
}

// BuildBuilderHeaders signs one request with builder credentials.
//
// The signature is the same construction as the CLOB's level-2 header —
//
//	signature = base64url(HMAC-SHA256(base64-decode(secret), timestamp ‖ method ‖ requestPath ‖ body))
//
// — under different header names. Three details decide whether it verifies:
//
//   - timestamp is unix SECONDS, and the value signed must be the value sent.
//     Passing it in rather than reading the clock is what makes a golden
//     vector possible.
//   - requestPath is the bare path. No host, no query string. A query string
//     is not covered by the signature at all.
//   - the passphrase is sent in the clear and is not part of the signed
//     message. Only the secret enters the HMAC.
func BuildBuilderHeaders(creds BuilderCredentials, timestamp, method, requestPath, body string) (BuilderHeaders, error) {
	if creds.Key == "" || creds.Secret == "" || creds.Passphrase == "" {
		return BuilderHeaders{}, fmt.Errorf("relayer: builder credentials need a key, a secret and a passphrase")
	}
	sig, err := polymarket.SignHMAC(creds.Secret, timestamp, method, requestPath, body)
	if err != nil {
		return BuilderHeaders{}, err
	}
	return BuilderHeaders{
		APIKey:     creds.Key,
		Passphrase: creds.Passphrase,
		Signature:  sig,
		Timestamp:  timestamp,
	}, nil
}

// ErrNoCredentials is returned when an authenticated endpoint is called on a
// Client that has none. It wraps polymarket.ErrNoCredentials, so
// polymarket.Indeterminate reports false: the request never left this
// process, and nothing on the relayer changed.
var ErrNoCredentials = fmt.Errorf(
	"relayer: no relayer credentials: build the client with WithAPIKey or WithBuilderCredentials: %w",
	polymarket.ErrNoCredentials)

// authHeadersKey marks a request context carrying the credential headers for
// one call.
//
// The root Session builds the http.Request itself and accepts no extra
// headers, so a Client hands its headers to its own transport through the
// context instead. Marking each call rather than every request is deliberate:
// a public relayer endpoint then never carries a credential it has no use
// for.
type authHeadersKey struct{}

// headerTransport applies the headers a Client attached to the request
// context. New installs it on a Client that has credentials.
type headerTransport struct {
	base http.RoundTripper
}

// RoundTrip adds the call's credential headers, if it has any, and forwards.
func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	headers, ok := req.Context().Value(authHeadersKey{}).(map[string]string)
	if !ok {
		return base.RoundTrip(req)
	}
	// A RoundTripper must not modify the request it is given.
	req = req.Clone(req.Context())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return base.RoundTrip(req)
}

// authContext returns a context carrying the credential headers for one call.
//
// Every authenticated relayer method goes through it, writes included: pass
// the method, the bare path and the encoded body, and hand the context it
// returns to the session.
func (c *Client) authContext(ctx context.Context, method, requestPath, body string) (context.Context, error) {
	if c.auth == nil {
		return nil, ErrNoCredentials
	}
	headers, err := c.auth.AuthHeaders(method, requestPath, body)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, authHeadersKey{}, headers), nil
}

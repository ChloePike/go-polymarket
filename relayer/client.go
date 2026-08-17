// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package relayer is a client for Polymarket's transaction relayer
// (RelayerHost). The relayer puts a user's transaction on chain for them: the
// user signs an operation for their proxy or Safe wallet off chain, and the
// relayer broadcasts it and pays the gas.
//
// Four read endpoints are public: Nonce, RelayPayload, Deployed and
// Transaction need no credentials at all. Two are authenticated —
// Transactions reports the caller's own transactions, APIKeys lists their
// relayer API keys — and so is Submit.
//
// BuildSafeTransaction, BuildProxyTransaction and BuildWalletBatch sign what
// a wallet should do; Submit hands it to Polymarket to pay for and send.
// Submitting spends money and cannot be taken back, and the relayer answers
// with an id rather than a result: poll Transaction until the state is
// terminal.
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
package relayer

import (
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
	return &Client{
		session: polymarket.NewSession(polymarket.RelayerHost, cfg.session...),
		auth:    cfg.auth,
	}
}

// NewWithSession returns a client that shares an existing Session, so one
// wallet and one http.Client can serve several API packages.
//
// It reads the public endpoints. Give it credentials with SetAuthenticator
// when it also needs the authenticated ones.
func NewWithSession(s *polymarket.Session) *Client { return &Client{session: s} }

// SetAuthenticator adopts the credentials the authenticated endpoints use,
// for a client built by NewWithSession or one whose credentials arrive after
// construction.
//
// Call it before the first authenticated request. A Client is otherwise safe
// for concurrent use; this is not.
func (c *Client) SetAuthenticator(a Authenticator) { c.auth = a }

// An Option configures a Client.
//
// The options every Polymarket client package takes are re-exported below
// under their usual names, so relayer.WithHost and gamma.WithHost do the same
// thing. Option is this package's own type rather than an alias for
// polymarket.Option for one reason: the credential options configure the
// Client, and a polymarket.Option is a function over a Session, which cannot
// reach it. SetAuthenticator is the same thing after construction.
type Option interface {
	apply(*config)
}

// config is the resolved option set New builds a Client from.
type config struct {
	session []polymarket.Option
	auth    Authenticator
}

// sessionOption carries one polymarket.Option through to NewSession.
type sessionOption struct {
	opt polymarket.Option
}

func (o sessionOption) apply(cfg *config) { cfg.session = append(cfg.session, o.opt) }

// authOption holds the credentials the authenticated endpoints use.
type authOption struct {
	auth Authenticator
}

func (o authOption) apply(cfg *config) { cfg.auth = o.auth }

// WithHost overrides the API host, for a proxy or a test server.
func WithHost(host string) Option { return sessionOption{opt: polymarket.WithHost(host)} }

// WithHTTPClient supplies the http.Client that issues requests. Use it to set
// a timeout, a transport, or a proxy. The default allows 30 seconds.
func WithHTTPClient(c *http.Client) Option {
	return sessionOption{opt: polymarket.WithHTTPClient(c)}
}

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

// WithL2Authenticator supplies the CLOB's level-2 authenticator, which the
// relayer does not accept either. The relayer's own equivalent is
// WithAuthenticator, whose Authenticator interface is this package's.
func WithL2Authenticator(a polymarket.L2Authenticator) Option {
	return sessionOption{opt: polymarket.WithL2Authenticator(a)}
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

// authHeaders renders the credential headers for one call.
//
// Every authenticated relayer method goes through it, writes included: pass
// the method, the bare path and the encoded body, and put the result in the
// request's Headers. Rendering them per call rather than per client is
// deliberate — a public relayer endpoint never carries a credential it has no
// use for — and a signing scheme covers the very request it travels on.
func (c *Client) authHeaders(method, requestPath, body string) (map[string]string, error) {
	if c.auth == nil {
		return nil, ErrNoCredentials
	}
	return c.auth.AuthHeaders(method, requestPath, body)
}

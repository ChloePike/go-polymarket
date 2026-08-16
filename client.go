// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package polymarket is a client for the Polymarket APIs.
//
// Polymarket is a prediction market: each outcome is an ERC-1155 token that
// settles at one dollar if it happens and nothing if it does not, so a price
// between zero and one reads directly as a probability. Trading runs through a
// central limit order book whose fills settle on Polygon, and orders are
// authorised by an EIP-712 signature rather than by an on-chain transaction.
//
// The client covers four hosts:
//
//   - the CLOB (DefaultHost): books, prices, orders, trades and rewards
//   - Gamma (GammaHost): market and event metadata
//   - the data API (DataHost): positions, activity and holders
//   - the streaming endpoint (WSHost): see the ws subpackage
//
// The zero Client is ready for every endpoint that needs no credentials:
//
//	var c polymarket.Client
//	book, err := c.OrderBook(ctx, tokenID)
//
// Trading needs a Signer and level-2 credentials:
//
//	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
//	c := &polymarket.Client{Signer: key}
//	creds, err := c.CreateOrDeriveAPIKey(ctx)
//
// # Authentication
//
// Level 1 is an EIP-712 signature proving control of the wallet, used only to
// create or derive an API key. Level 2 is an HMAC over each request, using the
// credentials level 1 returns. Methods state which they need.
//
// # Money
//
// Amounts are exact: prices and sizes are decimal strings, and the integer
// amounts an order carries are computed with rational arithmetic, never
// float64. Signing an order authorises a trade, so the client validates what
// it can before signing rather than after.
package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Errors reported before a request is made.
var (
	// ErrNoSigner is returned when an operation needs a private key and the
	// Client has none.
	ErrNoSigner = errors.New("polymarket: operation needs a Signer")

	// ErrNoCredentials is returned when an operation needs level-2
	// credentials. Call CreateOrDeriveAPIKey, or set Creds directly.
	ErrNoCredentials = errors.New("polymarket: operation needs API credentials")
)

// A Client talks to the Polymarket APIs. The zero value is usable for
// unauthenticated endpoints; set Signer and Creds to trade. A Client is safe
// for concurrent use once its fields are set.
type Client struct {
	// Host is the CLOB host. Empty means DefaultHost.
	Host string

	// Gamma is the metadata host. Empty means GammaHost.
	Gamma string

	// Data is the portfolio host. Empty means DataHost.
	Data string

	// ChainID selects the exchange contracts an order is signed against.
	// Zero means ChainPolygon.
	ChainID int64

	// HTTPClient issues the requests. Nil means a client with a 30-second
	// timeout.
	HTTPClient *http.Client

	// Signer holds the trading key. It is needed for level-1 authentication
	// and for signing orders, and is not used once credentials exist.
	Signer Signer

	// Creds are the level-2 credentials. CreateOrDeriveAPIKey sets them.
	Creds *APICreds

	// UserAgent is sent with every request. Empty means a default naming this
	// package.
	UserAgent string
}

const defaultUserAgent = "go-polymarket"

func (c *Client) clobHost() string {
	if c.Host != "" {
		return strings.TrimSuffix(c.Host, "/")
	}
	return DefaultHost
}

func (c *Client) gammaHost() string {
	if c.Gamma != "" {
		return strings.TrimSuffix(c.Gamma, "/")
	}
	return GammaHost
}

func (c *Client) dataHost() string {
	if c.Data != "" {
		return strings.TrimSuffix(c.Data, "/")
	}
	return DataHost
}

func (c *Client) chainID() int64 {
	if c.ChainID != 0 {
		return c.ChainID
	}
	return ChainPolygon
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return defaultUserAgent
}

// An Error reports a request the API refused. Callers can inspect StatusCode
// to tell a bad request from a rate limit or an outage.
type Error struct {
	Method     string
	URL        string
	StatusCode int
	// Message is the error text the API supplied, when it supplied one.
	Message string
	// Body is the raw response, truncated for legibility.
	Body string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("polymarket: %s %s: %d %s: %s",
			e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode), e.Message)
	}
	return fmt.Sprintf("polymarket: %s %s: %d %s: %s",
		e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode), e.Body)
}

// request describes one API call. Building it in one place keeps the level-2
// signature honest: the HMAC must cover exactly the path and query that are
// sent, so both are derived from the same value.
type request struct {
	method string
	host   string
	path   string
	query  url.Values
	body   any // marshalled to JSON when not nil
	auth   authLevel
	out    any // pointer to decode the response into, or nil to discard
}

type authLevel int

const (
	authNone authLevel = iota
	authL1
	authL2
)

// requestPath renders the path and query exactly as they will be sent. The
// level-2 HMAC covers this string, so it must be produced once and reused.
func (r request) requestPath() string {
	if len(r.query) == 0 {
		return r.path
	}
	return r.path + "?" + r.query.Encode()
}

func (c *Client) do(ctx context.Context, r request) error {
	var body []byte
	if r.body != nil {
		var err error
		if body, err = json.Marshal(r.body); err != nil {
			return fmt.Errorf("polymarket: encoding request body: %w", err)
		}
	}

	path := r.requestPath()
	host := r.host
	if host == "" {
		host = c.clobHost()
	}

	headers, err := c.authHeaders(r.auth, r.method, path, string(body))
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, host+path, reader)
	if err != nil {
		return fmt.Errorf("polymarket: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("polymarket: %s %s: %w", r.method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("polymarket: reading %s %s: %w", r.method, path, err)
	}
	if resp.StatusCode >= 300 {
		return &Error{
			Method:     r.method,
			URL:        path,
			StatusCode: resp.StatusCode,
			Message:    errorMessage(data),
			Body:       truncate(string(data), 2048),
		}
	}
	if r.out == nil {
		return nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, r.out); err != nil {
		return fmt.Errorf("polymarket: decoding %s %s: %w (body %s)",
			r.method, path, err, truncate(string(data), 256))
	}
	return nil
}

// authHeaders builds the headers for a request's authentication level.
func (c *Client) authHeaders(level authLevel, method, path, body string) (map[string]string, error) {
	switch level {
	case authNone:
		return nil, nil

	case authL1:
		if c.Signer == nil {
			return nil, ErrNoSigner
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		h, err := BuildL1Headers(c.Signer, c.chainID(), ts, 0)
		if err != nil {
			return nil, err
		}
		return h.header(), nil

	case authL2:
		if c.Creds == nil {
			return nil, ErrNoCredentials
		}
		address, err := c.address()
		if err != nil {
			return nil, err
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		h, err := BuildL2Headers(*c.Creds, address, ts, method, path, body)
		if err != nil {
			return nil, err
		}
		return h.header(), nil
	}
	return nil, fmt.Errorf("polymarket: unknown authentication level %d", level)
}

// address reports the account the level-2 headers name.
func (c *Client) address() (string, error) {
	if c.Signer == nil {
		return "", ErrNoSigner
	}
	return c.Signer.Address(), nil
}

// get issues an unauthenticated GET against the CLOB host.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, request{method: http.MethodGet, path: path, query: q, out: out})
}

// getL2 issues a GET authenticated with level-2 credentials.
func (c *Client) getL2(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, request{method: http.MethodGet, path: path, query: q, auth: authL2, out: out})
}

// postL2 issues a POST authenticated with level-2 credentials.
func (c *Client) postL2(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, request{method: http.MethodPost, path: path, body: body, auth: authL2, out: out})
}

// deleteL2 issues a DELETE authenticated with level-2 credentials.
func (c *Client) deleteL2(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, request{method: http.MethodDelete, path: path, body: body, auth: authL2, out: out})
}

// errorBody covers the shapes the API uses to report a failure. Different
// endpoints pick different field names for the same thing.
type errorBody struct {
	Error    string `json:"error"`
	ErrorMsg string `json:"errorMsg"`
	Message  string `json:"message"`
	Detail   string `json:"detail"`
}

// errorMessage pulls a human-readable message out of an error body. It is best
// effort: an unparseable body simply yields no message.
func errorMessage(data []byte) string {
	var body errorBody
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	for _, s := range []string{body.Error, body.ErrorMsg, body.Message, body.Detail} {
		if s != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

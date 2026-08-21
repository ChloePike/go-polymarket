// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

import (
	"context"
	"fmt"
	"net/http"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

// The sign-in-with-Ethereum pair. These are the only endpoints in this package
// that are about a caller rather than about a market.
const (
	epNonce = "/nonce"
	epLogin = "/login"
)

// DefaultLoginLifetime is how long a login built by Login is valid for.
const DefaultLoginLifetime = time.Hour

// NonceResponse is the wire shape of the nonce endpoint.
type NonceResponse struct {
	// Nonce is the value the sign-in message must carry.
	Nonce string `json:"nonce"`
}

// LoginResponse is what a successful login returns. It reports the account the
// signature resolved to, which is the only way to confirm that the session now
// held belongs to the address the caller meant.
type LoginResponse struct {
	// Type names the wallet family Polymarket filed the login under. An
	// ordinary key logs in as "metamask"; it describes the login, not this
	// client.
	Type string `json:"type"`
	// Address is the account the signature recovered to.
	Address string `json:"address"`
}

// Nonce returns a fresh sign-in nonce.
//
// The nonce is bound to a cookie the same response sets, so the session needs a
// cookie jar (WithCookieJar) and the login must run on that same session. A
// nonce presented without its cookie is refused.
//
// GET /nonce
func (c *Client) Nonce(ctx context.Context) (string, error) {
	if c.session.CookieJar() == nil {
		return "", errNoJar
	}
	var out NonceResponse
	if err := c.session.Get(ctx, epNonce, nil, &out); err != nil {
		return "", err
	}
	return out.Nonce, nil
}

// Login signs in with Ethereum and leaves the resulting session cookie in the
// session's jar.
//
// It fetches a nonce, signs the EIP-4361 message for the session's signer, and
// presents the bearer token. On success the jar holds the session cookie, and
// sharing that jar with a relayer client is what lets relayer.Client.MintAPIKey
// mint an API key — the one endpoint here that this login exists to reach.
//
// It needs a Signer and a cookie jar. The account it authenticates is the
// signer's own address, the externally owned account, never a proxy or Safe.
//
// GET /nonce then GET /login
func (c *Client) Login(ctx context.Context) (LoginResponse, error) {
	var out LoginResponse
	signer := c.session.Signer()
	if signer == nil {
		return out, fmt.Errorf("gamma: login needs a signer: build the client with WithSigner")
	}
	if c.session.CookieJar() == nil {
		return out, errNoJar
	}
	nonce, err := c.Nonce(ctx)
	if err != nil {
		return out, err
	}
	msg := polymarket.NewSIWEMessage(signer.Address(), nonce, c.session.ChainID(), DefaultLoginLifetime)
	token, err := polymarket.SignSIWE(signer, msg)
	if err != nil {
		return out, err
	}
	// The token travels in the Authorization header and the request carries
	// no body: the login is a GET. A POST is answered 405.
	err = c.session.Do(ctx, polymarket.Request{
		Method:  http.MethodGet,
		Path:    epLogin,
		Headers: map[string]string{"Authorization": "Bearer " + token},
		Out:     &out,
	})
	return out, err
}

// errNoJar is what both sign-in calls answer without a cookie jar. The failure
// is otherwise silent: the requests succeed, the cookies are dropped on the
// floor, and only the eventual mint reports an unauthorized caller.
var errNoJar = fmt.Errorf("gamma: signing in needs a cookie jar: build the client with WithCookieJar")

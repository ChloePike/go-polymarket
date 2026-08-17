// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package gamma is a client for Polymarket's Gamma metadata API (GammaHost).
// Gamma is the metadata behind a Polymarket page — titles, slugs, tags,
// resolution state — and nothing here needs authentication: every endpoint
// this package wraps is a public, unauthenticated request.
//
// Every field that carries money, a price or a size decodes as
// [encoding/json.Number], which keeps the exact decimal text the server
// sent. A float64 does not: 0.29 is not representable in binary floating
// point, and a size read from this API becomes an order size the moment a
// caller trades on the position behind it. Read such a field through
// math/big, and print it as text:
//
//	v, ok := new(big.Rat).SetString(string(m.LiquidityNum))
//	fmt.Println(m.LiquidityNum.String())
//
// An absent or null number decodes to the empty json.Number, not to "0", and
// big.Rat rejects the empty string — treat an empty value as "not reported"
// rather than as zero.
package gamma

import polymarket "github.com/ChloePike/go-polymarket"

// A Client talks to the Gamma metadata API.
type Client struct {
	session *polymarket.Session
}

// New returns a client for the Gamma metadata API.
func New(opts ...Option) *Client {
	return &Client{session: polymarket.NewSession(polymarket.GammaHost, opts...)}
}

// NewWithSession returns a client that shares an existing Session, so one
// wallet and one http.Client can serve several API packages.
func NewWithSession(s *polymarket.Session) *Client { return &Client{session: s} }

// An Option configures a Client. These are the same options every Polymarket
// client package takes.
type Option = polymarket.Option

// Re-exported so a caller needs only this package's import.
var (
	WithHost        = polymarket.WithHost
	WithHTTPClient  = polymarket.WithHTTPClient
	WithSigner      = polymarket.WithSigner
	WithCredentials = polymarket.WithCredentials
	// WithL2Authenticator authenticates without holding the API secret.
	WithL2Authenticator = polymarket.WithL2Authenticator
	WithChainID         = polymarket.WithChainID
	WithUserAgent       = polymarket.WithUserAgent
	WithRetries         = polymarket.WithRetries
)

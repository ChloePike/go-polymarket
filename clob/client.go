// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package clob talks to Polymarket's central limit order book: the order
// book itself, and everything else that needs a signature to touch it.
// Reading market data — books, prices, spreads, market and token metadata —
// needs no credentials at all. Trading needs both: a Signer to prove control
// of the wallet, and the level-2 API credentials that signer obtains, to
// authenticate every ordinary request afterward.
package clob

import polymarket "github.com/ChloePike/go-polymarket"

// A Client talks to the CLOB.
type Client struct {
	session *polymarket.Session
}

// New returns a client for the CLOB.
func New(opts ...Option) *Client {
	return &Client{session: polymarket.NewSession(polymarket.DefaultHost, opts...)}
}

// NewWithSession returns a client that shares an existing Session, so one
// wallet and one http.Client can serve several API packages.
func NewWithSession(s *polymarket.Session) *Client { return &Client{session: s} }

// Option configures a Client. These are the same options every Polymarket
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

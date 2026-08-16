// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package bridge is a client for Polymarket's bridge API (BridgeHost), the
// service that moves value in and out of a Polymarket wallet: which chains and
// tokens are accepted, what a transfer will cost, the addresses to send to,
// and how far along a transfer is.
//
// Every endpoint here is public: none of them takes a Signer or APICreds, and
// none of them signs anything. Two are GETs; three are POSTs that create or
// price a transfer but still authenticate nothing.
//
// A bridge address is not a Polymarket wallet address. Deposit and Withdraw
// each return addresses derived for one wallet, and Status reports on those
// derived addresses — passing a Polymarket wallet address to Status reports
// nothing useful.
package bridge

import polymarket "github.com/ChloePike/go-polymarket"

// A Client talks to the Polymarket bridge API.
type Client struct {
	session *polymarket.Session
}

// New returns a client for the Polymarket bridge API.
func New(opts ...Option) *Client {
	return &Client{session: polymarket.NewSession(polymarket.BridgeHost, opts...)}
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
	WithChainID     = polymarket.WithChainID
	WithUserAgent   = polymarket.WithUserAgent
	WithRetries     = polymarket.WithRetries
)

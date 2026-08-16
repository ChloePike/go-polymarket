// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package data is a client for the Polymarket data API (DataHost): portfolio
// positions, on-chain activity, market holders and the various leaderboards.
//
// Every endpoint here is a public, unauthenticated GET: the data API reports
// any wallet's positions and activity to anyone who asks, so a Client needs
// no Signer and no APICreds.
package data

import polymarket "github.com/ChloePike/go-polymarket"

// A Client talks to the Polymarket data API.
type Client struct {
	session *polymarket.Session
}

// New returns a client for the Polymarket data API.
func New(opts ...Option) *Client {
	return &Client{session: polymarket.NewSession(polymarket.DataHost, opts...)}
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

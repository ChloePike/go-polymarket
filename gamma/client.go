// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package gamma is a client for Polymarket's Gamma metadata API (GammaHost).
// Gamma is the metadata behind a Polymarket page — titles, slugs, tags,
// resolution state — and nothing here needs authentication: every endpoint
// this package wraps is a public, unauthenticated request.
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
	WithChainID     = polymarket.WithChainID
	WithUserAgent   = polymarket.WithUserAgent
	WithRetries     = polymarket.WithRetries
)

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import "time"

// Options are interfaces rather than functions so that the compiler can tell
// which channel an option belongs to. WithURL applies to all three, because
// every channel has an endpoint; WithLevel applies only to the market
// channel, and passing it to DialRTDS does not compile.

// A MarketOption customises a connection created by DialMarket.
type MarketOption interface {
	applyMarket(*marketConfig)
}

// A UserOption customises a connection created by DialUser.
type UserOption interface {
	applyUser(*userConfig)
}

// An RTDSOption customises a connection created by DialRTDS.
type RTDSOption interface {
	applyRTDS(*rtdsConfig)
}

// endpoint holds what every channel has: where to connect and how often to
// prove the connection is alive.
type endpoint struct {
	url          string
	pingInterval time.Duration
}

// marketConfig is the resolved option set for a market-channel dial: the
// shared endpoint plus the market channel's own feature switches.
type marketConfig struct {
	endpoint
	level                int
	customFeatureEnabled bool
	initialDump          bool
}

// userConfig is the resolved option set for a user-channel dial. The user
// channel takes no options beyond the shared endpoint.
type userConfig struct {
	endpoint
}

// rtdsConfig is the resolved option set for an RTDS dial. RTDS takes no
// options beyond the shared endpoint.
type rtdsConfig struct {
	endpoint
}

// urlOption points a connection somewhere other than production.
type urlOption string

func (u urlOption) applyMarket(c *marketConfig) { c.url = string(u) }
func (u urlOption) applyUser(c *userConfig)     { c.url = string(u) }
func (u urlOption) applyRTDS(c *rtdsConfig)     { c.url = string(u) }

// WithURL overrides the endpoint a connection dials.
//
// Use it for a proxy, a recorded fixture, a regional endpoint, or a local
// server in a test. The default is the matching production URL: MarketURL,
// UserURL or RTDSURL.
//
// The scheme decides the transport, so a test server from net/http/httptest
// is reached by rewriting its http:// prefix to ws://:
//
//	srv := httptest.NewServer(handler)
//	conn, err := ws.DialMarket(ctx, ids,
//		ws.WithURL("ws"+strings.TrimPrefix(srv.URL, "http")))
func WithURL(url string) interface {
	MarketOption
	UserOption
	RTDSOption
} {
	return urlOption(url)
}

// pingIntervalOption changes the keepalive cadence.
type pingIntervalOption time.Duration

func (p pingIntervalOption) applyMarket(c *marketConfig) { c.pingInterval = time.Duration(p) }
func (p pingIntervalOption) applyUser(c *userConfig)     { c.pingInterval = time.Duration(p) }
func (p pingIntervalOption) applyRTDS(c *rtdsConfig)     { c.pingInterval = time.Duration(p) }

// WithPingInterval overrides how often the client sends its keepalive frame.
//
// The defaults match what Polymarket documents: ten seconds for the CLOB
// channels and five for the live-data service. Lengthening it risks the
// server closing an idle connection; shortening it costs nothing but traffic.
// A non-positive duration is ignored.
func WithPingInterval(d time.Duration) interface {
	MarketOption
	UserOption
	RTDSOption
} {
	return pingIntervalOption(d)
}

// levelOption sets the market channel's subscription level.
type levelOption int

func (l levelOption) applyMarket(c *marketConfig) { c.level = int(l) }

// WithLevel sets the market channel's subscription level. Valid values are
// 1, 2 (the default), and 3, but Polymarket's own AsyncAPI schema defines no
// behavioural difference between them; only level 2 was exercised live.
func WithLevel(level int) MarketOption { return levelOption(level) }

// customFeatureOption turns the extra market event types on.
type customFeatureOption bool

func (f customFeatureOption) applyMarket(c *marketConfig) { c.customFeatureEnabled = bool(f) }

// WithCustomFeatureEnabled turns on BestBidAskEvent, NewMarketEvent and
// MarketResolvedEvent, none of which are sent otherwise. Not exercised live.
func WithCustomFeatureEnabled(enabled bool) MarketOption { return customFeatureOption(enabled) }

// initialDumpOption controls the snapshot sent on connect.
type initialDumpOption bool

func (d initialDumpOption) applyMarket(c *marketConfig) { c.initialDump = bool(d) }

// WithInitialDump controls whether the server sends an immediate BookEvent
// for every subscribed asset on connect, and on every reconnect. It defaults
// to true, matching the server's own default.
//
// Turning it off is rarely right: without a snapshot after a reconnect there
// is nothing to apply subsequent deltas to.
func WithInitialDump(dump bool) MarketOption { return initialDumpOption(dump) }

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import "time"

// These constants duplicate the two or three host URLs the root
// polymarket package also defines, so this package stays free of a
// dependency on it. See package doc for why.
const (
	// MarketURL is the CLOB market channel: public order-book and trade
	// data, no authentication. See DialMarket.
	MarketURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"

	// UserURL is the CLOB user channel: one account's authenticated order
	// and trade updates. See DialUser.
	UserURL = "wss://ws-subscriptions-clob.polymarket.com/ws/user"

	// RTDSURL is Polymarket's Real-Time Data Service: crypto/equity prices
	// and comment activity, no authentication observed. See DialRTDS.
	RTDSURL = "wss://ws-live-data.polymarket.com"
)

// pingText is the literal text frame every host in this package expects as
// a keepalive, and pongText is the literal text frame the two CLOB
// channels reply with. Both are plain websocket text frames, not the
// protocol-level ping/pong control frames RFC 6455 defines.
const (
	pingText = "PING"
	pongText = "PONG"
)

// Keepalive intervals, one per host. Documented explicitly per package doc.
const (
	clobPingInterval = 10 * time.Second
	rtdsPingInterval = 5 * time.Second
)

// Reconnection policy. Polymarket publishes no numeric backoff schedule
// (see package doc), so this package picks its own: start at
// initialBackoff, double on each successive failure, cap at maxBackoff,
// and add up to backoffJitterPercent% of extra random delay so many
// clients reconnecting at once do not all retry in lockstep.
const (
	initialBackoff       = 500 * time.Millisecond
	maxBackoff           = 30 * time.Second
	backoffJitterPercent = 20

	// maxInitialDialAttempts bounds only the Dial constructors' first
	// handshake; once connected, a Conn's background reconnect loop retries
	// indefinitely using the same backoff.
	maxInitialDialAttempts = 5

	dialTimeout  = 10 * time.Second
	writeTimeout = 5 * time.Second
)

// maxFrameSize is the read-size limit applied to every dialed connection.
// github.com/coder/websocket defaults to 32KiB, comfortably exceeded by a
// market-channel initial_dump snapshot covering more than a couple of
// assets; 8MiB gives generous headroom without being unbounded.
const maxFrameSize = 8 << 20

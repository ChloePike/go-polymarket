// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package ws streams live data from Polymarket's websocket feeds: the CLOB
// market channel (public order books and trades), the CLOB user channel
// (a single account's authenticated order and fill updates), and the
// Real-Time Data Service, or RTDS (crypto/equity prices and comment
// activity). Dial one with DialMarket, DialUser, or DialRTDS; read decoded
// events with Conn.Read.
//
// This package is self-contained: it depends only on the standard library
// and github.com/coder/websocket, and does not import the module's root
// polymarket package.
//
// # Event shape
//
// Every message a Conn can deliver implements Event, a sealed interface
// (its one method is unexported, so only types in this package can
// implement it): BookEvent, PriceChangeEvent, TickSizeChangeEvent,
// LastTradePriceEvent, BestBidAskEvent, NewMarketEvent, MarketResolvedEvent
// (market channel), OrderEvent, TradeEvent (user channel), PriceUpdateEvent,
// CommentEvent, ReactionEvent, RTDSErrorEvent (RTDS), plus two the package
// itself injects into the stream: ReconnectEvent and UnknownEvent. A caller
// dispatches with a type switch:
//
//	ev, err := conn.Read(ctx)
//	switch ev := ev.(type) {
//	case ws.BookEvent:
//		// ev.Bids, ev.Asks, ...
//	case ws.PriceChangeEvent:
//		// ...
//	}
//
// A sealed interface was chosen over map[string]any so every event's field
// names and types are checked by the compiler rather than discovered at
// runtime, and over a single flattened struct covering every possible
// field because the event shapes genuinely differ -- different array
// element types, optional nested objects -- such that one struct would
// need most fields to be pointers with no way to tell "absent" from
// "zero" apart from a nil check on nearly everything. One named struct per
// event keeps each type's required fields required and its own.
//
// Polymarket's live wire format has proven to be a superset of its own
// published schemas (see UnknownEvent), so decoding is permissive: unknown
// fields inside a recognized event are ignored, and a frame whose
// event_type (or, on RTDS, topic/type pair) this package does not
// recognize is delivered as UnknownEvent with the raw JSON attached,
// rather than treated as a decode failure.
//
// # Reconnection
//
// A Conn owns a background goroutine that reconnects automatically when
// its socket drops -- which happens routinely: Polymarket's edge resets a
// large fraction of fresh connections even in normal operation (observed
// live on both the CLOB and RTDS hosts). Reconnection uses exponential
// backoff starting at 500ms, doubling each attempt, capped at 30s, with up
// to 20% jitter added to each wait -- a policy this package chose itself,
// since Polymarket documents no numeric backoff schedule. The Dial
// constructors apply the same backoff to the initial handshake, retrying
// up to 5 times before giving up.
//
// Every successful reconnect resends the connection's full current
// subscription (including any assets or markets added after Dial via
// Subscribe) and is reported to the caller as a ReconnectEvent from Read,
// so the caller can tell a resubscribe happened rather than silently
// losing coverage of some of its subscribed assets. On the market channel
// this is usually transparent: the resent subscribe carries initial_dump,
// so a fresh BookEvent for every asset follows immediately. The user
// channel does not replay missed events across any gap -- after a
// ReconnectEvent from a user-channel Conn, re-fetch open orders and recent
// trades over REST before trusting new stream events (this is Polymarket's
// own documented guidance, not something this package can paper over).
//
// # Keepalive
//
// Each Conn runs an internal goroutine that writes the plain text frame
// "PING" (not a websocket control-frame ping) on a fixed interval: every
// 10 seconds for both CLOB channels, every 5 seconds for RTDS. The CLOB
// hosts reply with the plain text frame "PONG", which Read swallows
// without surfacing an Event. Whether RTDS replies at all is unconfirmed:
// live testing sent PING and saw no reply in an 18-second window on that
// host, though the docs still document the same client-driven interval.
//
// # Order-book hash
//
// See Book and BookHash for the SHA-1 content hash Polymarket serves
// alongside every book snapshot so a client can detect local drift.
//
// # What this package does not cover
//
// Two more websocket hosts are documented but were not implemented here:
// a sports-scores feed (wss://sports-api.polymarket.com/ws, server-driven
// keepalive, a different protocol shape entirely) and an authenticated RFQ
// market-maker gateway (wss://combos-rfq-gateway-quoter.polymarket.com/ws/rfq).
// Both are out of scope for a market-data client and are not built on the
// same request/response shapes as the three channels above, so folding
// them into this package's Conn would not have been an honest fit.
package ws

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// marketSubscribeRequest is the frame sent immediately after the market
// channel's socket opens, and again -- reflecting the then-current asset
// set -- after every automatic reconnect.
type marketSubscribeRequest struct {
	AssetIDs             []string `json:"assets_ids"`
	Type                 string   `json:"type"`
	InitialDump          bool     `json:"initial_dump"`
	Level                int      `json:"level"`
	CustomFeatureEnabled bool     `json:"custom_feature_enabled"`
}

// marketOperationRequest adds or removes assets from an already-open
// market-channel subscription without reconnecting.
type marketOperationRequest struct {
	Operation            string   `json:"operation"`
	AssetIDs             []string `json:"assets_ids"`
	Level                int      `json:"level"`
	CustomFeatureEnabled bool     `json:"custom_feature_enabled"`
}

// MarketOption customizes a connection created by DialMarket.
type MarketOption func(*marketSubscription)

// WithLevel sets the market channel's subscription level. Valid values are
// 1, 2 (the default), and 3, but Polymarket's own AsyncAPI schema defines
// no behavioral difference between them; only level 2 was exercised live.
func WithLevel(level int) MarketOption {
	return func(s *marketSubscription) { s.level = level }
}

// WithCustomFeatureEnabled turns on the BestBidAskEvent, NewMarketEvent,
// and MarketResolvedEvent event types, none of which are sent otherwise.
// Not exercised live.
func WithCustomFeatureEnabled(enabled bool) MarketOption {
	return func(s *marketSubscription) { s.customFeatureEnabled = enabled }
}

// WithInitialDump controls whether the server sends an immediate BookEvent
// for every subscribed asset on connect (and on every reconnect). Defaults
// to true, matching the server's own documented default.
func WithInitialDump(dump bool) MarketOption {
	return func(s *marketSubscription) { s.initialDump = dump }
}

// marketSubscription tracks the market channel's desired asset set and
// implements subscription.
type marketSubscription struct {
	mu                   sync.Mutex
	assetIDs             map[string]struct{}
	level                int
	customFeatureEnabled bool
	initialDump          bool
}

func newMarketSubscription(assetIDs []string) *marketSubscription {
	s := &marketSubscription{
		assetIDs:    make(map[string]struct{}, len(assetIDs)),
		level:       2,
		initialDump: true,
	}
	for _, id := range assetIDs {
		s.assetIDs[id] = struct{}{}
	}
	return s
}

func (s *marketSubscription) initial() []byte {
	s.mu.Lock()
	req := marketSubscribeRequest{
		AssetIDs:             sortedKeys(s.assetIDs),
		Type:                 "market",
		InitialDump:          s.initialDump,
		Level:                s.level,
		CustomFeatureEnabled: s.customFeatureEnabled,
	}
	s.mu.Unlock()
	b, _ := json.Marshal(req) // every field is a plain string, bool, or int; cannot fail.
	return b
}

func (s *marketSubscription) change(add bool, ids []string) ([]byte, error) {
	s.mu.Lock()
	op := "unsubscribe"
	if add {
		op = "subscribe"
		for _, id := range ids {
			s.assetIDs[id] = struct{}{}
		}
	} else {
		for _, id := range ids {
			delete(s.assetIDs, id)
		}
	}
	level, custom := s.level, s.customFeatureEnabled
	s.mu.Unlock()

	req := marketOperationRequest{
		Operation:            op,
		AssetIDs:             append([]string(nil), ids...),
		Level:                level,
		CustomFeatureEnabled: custom,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DialMarket opens the public CLOB market channel and subscribes to
// assetIDs (CLOB token IDs, decimal uint256 strings). It blocks until the
// connection is established and the initial subscribe frame has been
// sent, retrying the handshake itself if it fails: Polymarket's edge
// briefly resets a large fraction of fresh connections in normal
// operation (observed live: several failures before a clean connect was
// typical), so DialMarket retries the dial up to 5 times with the backoff
// documented on Conn before giving up.
//
// With the default options, the server sends one BookEvent per subscribed
// asset immediately after subscribing, and again after every automatic
// reconnect (see WithInitialDump).
//
// Ids passed to Subscribe and Unsubscribe on the returned Conn are CLOB
// token IDs, the same kind as assetIDs here.
func DialMarket(ctx context.Context, assetIDs []string, opts ...MarketOption) (*Conn, error) {
	sub := newMarketSubscription(assetIDs)
	for _, opt := range opts {
		opt(sub)
	}
	return newConn(ctx, MarketURL, sub, decodeMarket, clobPingInterval)
}

// decodeMarket decodes one inbound market-channel text frame into zero or
// more Events. The plain-text "PONG" keepalive reply is swallowed (zero
// events, no error); every other frame is JSON, either a bare object
// (every event type other than the initial snapshot) or an array of
// objects (the initial book-snapshot dump only -- see package doc).
func decodeMarket(frame []byte) ([]Event, error) {
	trimmed := bytes.TrimSpace(frame)
	if string(trimmed) == pongText {
		return nil, nil
	}
	objects, err := splitFrame(trimmed)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(objects))
	for _, obj := range objects {
		ev, err := decodeMarketObject(obj)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func decodeMarketObject(obj json.RawMessage) (Event, error) {
	var env eventTypeEnvelope
	if err := json.Unmarshal(obj, &env); err != nil {
		return nil, fmt.Errorf("ws: decode event envelope: %w", err)
	}
	switch env.EventType {
	case "book":
		var ev BookEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "price_change":
		var ev PriceChangeEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "tick_size_change":
		var ev TickSizeChangeEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "last_trade_price":
		var ev LastTradePriceEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "best_bid_ask":
		var ev BestBidAskEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "new_market":
		var ev NewMarketEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "market_resolved":
		var ev MarketResolvedEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	default:
		return UnknownEvent{EventType: env.EventType, Raw: append(json.RawMessage(nil), obj...)}, nil
	}
}

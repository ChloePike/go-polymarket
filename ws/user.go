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

// Credentials carries the level-2 CLOB API key triple that authenticates
// the user channel. Unlike REST L2 auth, the websocket handshake sends
// these values directly as JSON fields in the subscribe frame's body --
// there is no per-message HMAC signature for the socket itself, per
// Polymarket's documentation. UNVERIFIED live: this package's
// verification did not exercise the user channel, since doing so needs
// real account credentials, out of scope for the task this package was
// built for (see package doc).
type Credentials struct {
	APIKey     string
	Secret     string
	Passphrase string
}

// userAuth is the wire form of Credentials inside a user-channel subscribe
// frame. It exists separately from Credentials so the JSON field names stay
// an implementation detail of the protocol rather than of the public type.
type userAuth struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// userSubscribeRequest is the frame sent immediately after the user
// channel's socket opens, and again after every automatic reconnect.
type userSubscribeRequest struct {
	Auth    userAuth `json:"auth"`
	Type    string   `json:"type"`
	Markets []string `json:"markets,omitempty"`
}

// userOperationRequest adds or removes condition IDs from an already-open
// user-channel subscription without reconnecting.
type userOperationRequest struct {
	Operation string   `json:"operation"`
	Markets   []string `json:"markets"`
}

// userSubscription tracks the user channel's credentials and desired
// condition-ID filter, and implements subscription.
type userSubscription struct {
	mu      sync.Mutex
	creds   Credentials
	markets map[string]struct{}
}

func newUserSubscription(creds Credentials, conditionIDs []string) *userSubscription {
	s := &userSubscription{
		creds:   creds,
		markets: make(map[string]struct{}, len(conditionIDs)),
	}
	for _, id := range conditionIDs {
		s.markets[id] = struct{}{}
	}
	return s
}

func (s *userSubscription) initial() []byte {
	s.mu.Lock()
	req := userSubscribeRequest{
		Auth: userAuth{
			APIKey:     s.creds.APIKey,
			Secret:     s.creds.Secret,
			Passphrase: s.creds.Passphrase,
		},
		Type:    "user",
		Markets: sortedKeys(s.markets),
	}
	s.mu.Unlock()
	b, _ := json.Marshal(req) // every field is a plain string or slice of strings; cannot fail.
	return b
}

func (s *userSubscription) change(add bool, ids []string) ([]byte, error) {
	s.mu.Lock()
	op := "unsubscribe"
	if add {
		op = "subscribe"
		for _, id := range ids {
			s.markets[id] = struct{}{}
		}
	} else {
		for _, id := range ids {
			delete(s.markets, id)
		}
	}
	s.mu.Unlock()

	b, err := json.Marshal(userOperationRequest{Operation: op, Markets: append([]string(nil), ids...)})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DialUser opens the authenticated CLOB user channel and subscribes to
// order and trade events for the account identified by creds, filtered to
// conditionIDs (pass nil to receive events for every market the account
// touches). It blocks until connected, with the same bounded dial retry
// as DialMarket.
//
// UNVERIFIED live: see Credentials. The subscribe frame shape and every
// event type this channel delivers come from Polymarket's documentation
// only.
//
// The user channel does not replay events missed across a disconnect.
// After every ReconnectEvent from Read, re-fetch open orders and recent
// trades over REST before trusting new stream events -- unlike the market
// channel, there is no initial_dump-style resync here.
//
// Ids passed to Subscribe and Unsubscribe on the returned Conn are
// condition IDs, the same kind as conditionIDs here.
func DialUser(ctx context.Context, creds Credentials, conditionIDs []string, opts ...UserOption) (*Conn, error) {
	cfg := userConfig{endpoint: endpoint{url: UserURL, pingInterval: clobPingInterval}}
	for _, opt := range opts {
		opt.applyUser(&cfg)
	}
	sub := newUserSubscription(creds, conditionIDs)
	return newConn(ctx, cfg.url, sub, decodeUser, cfg.pingInterval)
}

// decodeUser decodes one inbound user-channel text frame into zero or more
// Events, following the same PONG-swallowing, array-or-object framing as
// decodeMarket.
func decodeUser(frame []byte) ([]Event, error) {
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
		ev, err := decodeUserObject(obj)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func decodeUserObject(obj json.RawMessage) (Event, error) {
	var env eventTypeEnvelope
	if err := json.Unmarshal(obj, &env); err != nil {
		return nil, fmt.Errorf("ws: decode event envelope: %w", err)
	}
	switch env.EventType {
	case "order":
		var ev OrderEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	case "trade":
		var ev TradeEvent
		err := json.Unmarshal(obj, &ev)
		return ev, err
	default:
		return UnknownEvent{EventType: env.EventType, Raw: append(json.RawMessage(nil), obj...)}, nil
	}
}

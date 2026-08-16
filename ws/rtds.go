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

// Topic identifies an RTDS subscription channel.
type Topic string

// The four RTDS topics documented at
// docs.polymarket.com/market-data/realtime-data. Only TopicCryptoPrices'
// subscribe frame was live-confirmed accepted; see DialRTDS.
const (
	TopicCryptoPrices          Topic = "crypto_prices"
	TopicCryptoPricesChainlink Topic = "crypto_prices_chainlink"
	TopicEquityPrices          Topic = "equity_prices"
	TopicComments              Topic = "comments"
)

// RTDSSubscription is one entry in an RTDS subscribe request.
type RTDSSubscription struct {
	Topic Topic
	// Type selects which message type within Topic to receive, e.g.
	// "update" for a price topic, or "*" for every type (shown in one docs
	// example; not live-tested).
	Type string
	// Filters is passed through verbatim. Its grammar is inconsistent
	// across Polymarket's own documentation examples -- a comma-separated
	// list of symbols for crypto_prices, a JSON-encoded object string for
	// equity_prices -- and live testing did not resolve which is correct,
	// so this package does not parse, validate, or normalize it.
	Filters string
}

// rtdsSubscriptionEntry is RTDSSubscription's wire representation.
type rtdsSubscriptionEntry struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Filters string `json:"filters,omitempty"`
}

// rtdsSubscribeRequest is the frame sent immediately after the RTDS
// socket opens, and again after every automatic reconnect.
type rtdsSubscribeRequest struct {
	Action        string                  `json:"action"`
	Subscriptions []rtdsSubscriptionEntry `json:"subscriptions"`
}

// rtdsSubscription holds RTDS's desired subscription list and implements
// subscription. RTDS has no documented add/remove operation, so change
// always fails with ErrDynamicSubscribeUnsupported.
type rtdsSubscription struct {
	mu   sync.Mutex
	subs []RTDSSubscription
}

func newRTDSSubscription(subs []RTDSSubscription) *rtdsSubscription {
	return &rtdsSubscription{subs: append([]RTDSSubscription(nil), subs...)}
}

func (s *rtdsSubscription) initial() []byte {
	s.mu.Lock()
	entries := make([]rtdsSubscriptionEntry, len(s.subs))
	for i, sub := range s.subs {
		entries[i] = rtdsSubscriptionEntry{Topic: string(sub.Topic), Type: sub.Type, Filters: sub.Filters}
	}
	s.mu.Unlock()
	b, _ := json.Marshal(rtdsSubscribeRequest{Action: "subscribe", Subscriptions: entries}) // cannot fail.
	return b
}

func (s *rtdsSubscription) change(add bool, ids []string) ([]byte, error) {
	return nil, ErrDynamicSubscribeUnsupported
}

// DialRTDS opens Polymarket's Real-Time Data Service (RTDS) and subscribes
// to subs. It blocks until connected, with the same bounded dial retry as
// DialMarket -- RTDS showed the same intermittent immediate-close
// behavior on fresh connections live (roughly 1 in 5-10 attempts
// succeeded during verification).
//
// UNVERIFIED beyond connect and subscribe: sending a subscribe frame for
// TopicCryptoPrices was live-confirmed accepted -- the server replied with
// a single empty text frame and nothing else in an 18-second window; Read
// silently swallows that empty frame, the same way it swallows PONG. No
// populated PriceUpdateEvent, CommentEvent, or ReactionEvent was captured
// live; their shapes come from documentation only, which is itself
// internally inconsistent about some field types (see Number and the
// per-type doc comments below). Whether this host replies to the PING
// keepalive at all is also unconfirmed: a PING sent 2 seconds after
// connecting got no reply in a 16-18 second test window.
//
// A Conn from DialRTDS does not support Subscribe or Unsubscribe: there is
// no documented operation to add or remove topics without reconnecting.
// Calling either returns ErrDynamicSubscribeUnsupported.
func DialRTDS(ctx context.Context, subs []RTDSSubscription, opts ...RTDSOption) (*Conn, error) {
	cfg := rtdsConfig{endpoint: endpoint{url: RTDSURL, pingInterval: rtdsPingInterval}}
	for _, opt := range opts {
		opt.applyRTDS(&cfg)
	}
	sub := newRTDSSubscription(subs)
	return newConn(ctx, cfg.url, sub, decodeRTDS, cfg.pingInterval)
}

// Number is a numeric field whose JSON representation was inconsistent
// across two fetches of the same Polymarket documentation page -- once as
// a JSON number, once as a JSON string. Number unmarshals either
// representation and keeps the original text, so no precision is lost
// either way.
type Number string

// UnmarshalJSON implements json.Unmarshaler, accepting either a bare JSON
// number token or a JSON string.
func (n *Number) UnmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	*n = Number(b)
	return nil
}

// String returns n's original text.
func (n Number) String() string { return string(n) }

// rtdsEnvelope is the outer shape of every RTDS frame: a topic/type pair
// identifying what follows plus a nested payload, or -- when the gateway
// rejects a request -- Message/ConnectionID/RequestID instead.
type rtdsEnvelope struct {
	Topic     string          `json:"topic"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`

	Message      string `json:"message"`
	ConnectionID string `json:"connectionId"`
	RequestID    string `json:"requestId"`
}

// PriceUpdateEvent is an "update" message on an RTDS price topic
// (TopicCryptoPrices, TopicCryptoPricesChainlink, or TopicEquityPrices).
// Documentation-only: no populated update was captured live for any
// topic, only an empty acknowledgment frame (see DialRTDS).
type PriceUpdateEvent struct {
	Topic Topic
	// Type is the message type within Topic, normally "update".
	Type string
	// EnvelopeTimestamp is the outer envelope's timestamp (Unix ms).
	EnvelopeTimestamp int64

	Symbol string `json:"symbol"`
	// Timestamp is the payload's own timestamp (Unix ms), documented as
	// distinct from EnvelopeTimestamp.
	Timestamp int64  `json:"timestamp"`
	Value     Number `json:"value"`
	// FullAccuracyValue is populated for equity_prices only, per docs.
	FullAccuracyValue string `json:"full_accuracy_value"`
}

func (PriceUpdateEvent) event() {}

// CommentEvent is a comment_created or comment_removed message on
// TopicComments. Only comment_created's payload shape is documented;
// comment_removed's is UNVERIFIED -- not found in any fetched
// documentation page, not observed live.
type CommentEvent struct {
	Topic Topic
	// Type is "comment_created" or "comment_removed".
	Type              string
	EnvelopeTimestamp int64

	ID   string `json:"id"`
	Body string `json:"body"`
	// ParentEntityType is e.g. "Event"; the docs example implies comments
	// can attach to other entity types too, without enumerating them.
	ParentEntityType string `json:"parentEntityType"`
}

func (CommentEvent) event() {}

// ReactionEvent is a reaction_created or reaction_removed message on
// TopicComments. Only reaction_created's payload shape is documented;
// reaction_removed's is UNVERIFIED. CommentID is a JSON number in the
// documented example, unlike ID's JSON string on CommentEvent -- kept
// verbatim from the docs rather than normalized to match.
type ReactionEvent struct {
	Topic Topic
	// Type is "reaction_created" or "reaction_removed".
	Type              string
	EnvelopeTimestamp int64

	ID        string `json:"id"`
	CommentID int64  `json:"commentID"`
	// ReactionType is a string enum; only "HEART" is shown in docs, not a
	// full enumeration.
	ReactionType string `json:"reactionType"`
}

func (ReactionEvent) event() {}

// RTDSErrorEvent is delivered when the RTDS gateway rejects a request.
// Live-verified (see DialRTDS's package-level test evidence): sending a
// malformed subscribe topic got back exactly this shape.
// ConnectionID/RequestID look like AWS API-Gateway-managed WebSocket API
// identifiers.
type RTDSErrorEvent struct {
	Message      string
	ConnectionID string
	RequestID    string
}

func (RTDSErrorEvent) event() {}

// decodeRTDS decodes one inbound RTDS text frame into zero or more Events.
// A live-observed empty acknowledgment frame and the plain-text "PONG"
// keepalive reply are both swallowed (zero events, no error).
func decodeRTDS(frame []byte) ([]Event, error) {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 || string(trimmed) == pongText {
		return nil, nil
	}

	var env rtdsEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, fmt.Errorf("ws: decode RTDS envelope: %w", err)
	}

	if env.Topic == "" && env.Message != "" {
		return []Event{RTDSErrorEvent{
			Message:      env.Message,
			ConnectionID: env.ConnectionID,
			RequestID:    env.RequestID,
		}}, nil
	}

	switch {
	case env.Topic == string(TopicComments) && (env.Type == "comment_created" || env.Type == "comment_removed"):
		var c CommentEvent
		if err := json.Unmarshal(env.Payload, &c); err != nil {
			return nil, err
		}
		c.Topic, c.Type, c.EnvelopeTimestamp = Topic(env.Topic), env.Type, env.Timestamp
		return []Event{c}, nil

	case env.Topic == string(TopicComments) && (env.Type == "reaction_created" || env.Type == "reaction_removed"):
		var r ReactionEvent
		if err := json.Unmarshal(env.Payload, &r); err != nil {
			return nil, err
		}
		r.Topic, r.Type, r.EnvelopeTimestamp = Topic(env.Topic), env.Type, env.Timestamp
		return []Event{r}, nil

	case env.Topic != "":
		var p PriceUpdateEvent
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, err
		}
		p.Topic, p.Type, p.EnvelopeTimestamp = Topic(env.Topic), env.Type, env.Timestamp
		return []Event{p}, nil

	default:
		return []Event{UnknownEvent{Raw: append(json.RawMessage(nil), trimmed...)}}, nil
	}
}

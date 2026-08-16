// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
)

func TestRTDSSubscribeFrame(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subs := []RTDSSubscription{
		{Topic: TopicCryptoPrices, Type: "update", Filters: "btcusdt,ethusdt"},
	}
	conn, err := DialRTDS(ctx, subs, WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")

	var got rtdsSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &got); err != nil {
		t.Fatalf("unmarshal subscribe frame: %v", err)
	}
	if got.Action != "subscribe" {
		t.Errorf("Action = %q, want subscribe", got.Action)
	}
	if len(got.Subscriptions) != 1 {
		t.Fatalf("Subscriptions = %v", got.Subscriptions)
	}
	s := got.Subscriptions[0]
	if s.Topic != "crypto_prices" || s.Type != "update" || s.Filters != "btcusdt,ethusdt" {
		t.Errorf("Subscriptions[0] = %+v", s)
	}
}

func TestRTDSDynamicSubscribeUnsupported(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialRTDS(ctx, []RTDSSubscription{{Topic: TopicCryptoPrices, Type: "update"}},
		WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	if err := conn.Subscribe(ctx, []string{"x"}); err != ErrDynamicSubscribeUnsupported {
		t.Errorf("Subscribe error = %v, want ErrDynamicSubscribeUnsupported", err)
	}
	if err := conn.Unsubscribe(ctx, []string{"x"}); err != ErrDynamicSubscribeUnsupported {
		t.Errorf("Unsubscribe error = %v, want ErrDynamicSubscribeUnsupported", err)
	}
}

// TestRTDSEmptyAckSwallowed confirms the live-observed empty acknowledgment
// frame (and the PONG keepalive reply) never surface as an Event, and that
// a real event sent immediately after still arrives.
func TestRTDSEmptyAckSwallowed(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialRTDS(ctx, []RTDSSubscription{{Topic: TopicCryptoPrices, Type: "update"}},
		WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	sendText(t, sc, "") // the live-observed empty ack
	sendText(t, sc, pongText)
	sendText(t, sc, `{"topic":"crypto_prices","type":"update","timestamp":1782753357257,"payload":{"symbol":"btcusdt","timestamp":1782753357213,"value":67234.5}}`)

	ev := readEvent(t, conn)
	p, ok := ev.(PriceUpdateEvent)
	if !ok {
		t.Fatalf("got %T, want PriceUpdateEvent", ev)
	}
	if p.Topic != TopicCryptoPrices || p.Symbol != "btcusdt" || p.Value.String() != "67234.5" {
		t.Errorf("PriceUpdateEvent = %+v", p)
	}
}

// rtdsDecodeCase is one table-driven case for TestRTDSDecodeEventTypes.
type rtdsDecodeCase struct {
	name  string
	frame string
	check func(t *testing.T, ev Event)
}

var rtdsDecodeCases = []rtdsDecodeCase{
	{
		name:  "price update with string value",
		frame: `{"topic":"equity_prices","type":"update","timestamp":1782753357257,"payload":{"symbol":"aapl","timestamp":1782753357213,"value":"189.42","full_accuracy_value":"189.4217"}}`,
		check: func(t *testing.T, ev Event) {
			p, ok := ev.(PriceUpdateEvent)
			if !ok {
				t.Fatalf("got %T, want PriceUpdateEvent", ev)
			}
			if p.Value.String() != "189.42" || p.FullAccuracyValue != "189.4217" {
				t.Errorf("PriceUpdateEvent = %+v", p)
			}
		},
	},
	{
		name:  "comment_created",
		frame: `{"topic":"comments","type":"comment_created","timestamp":1782753357257,"payload":{"id":"1763355","body":"nice","parentEntityType":"Event"}}`,
		check: func(t *testing.T, ev Event) {
			c, ok := ev.(CommentEvent)
			if !ok {
				t.Fatalf("got %T, want CommentEvent", ev)
			}
			if c.ID != "1763355" || c.Body != "nice" || c.ParentEntityType != "Event" {
				t.Errorf("CommentEvent = %+v", c)
			}
		},
	},
	{
		name:  "reaction_created",
		frame: `{"topic":"comments","type":"reaction_created","timestamp":1782753357257,"payload":{"id":"8675309","commentID":1763355,"reactionType":"HEART"}}`,
		check: func(t *testing.T, ev Event) {
			r, ok := ev.(ReactionEvent)
			if !ok {
				t.Fatalf("got %T, want ReactionEvent", ev)
			}
			if r.CommentID != 1763355 || r.ReactionType != "HEART" {
				t.Errorf("ReactionEvent = %+v", r)
			}
		},
	},
	{
		name:  "gateway error",
		frame: `{"message":"Invalid request body","connectionId":"conn-1","requestId":"req-1"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(RTDSErrorEvent)
			if !ok {
				t.Fatalf("got %T, want RTDSErrorEvent", ev)
			}
			if e.Message != "Invalid request body" || e.ConnectionID != "conn-1" || e.RequestID != "req-1" {
				t.Errorf("RTDSErrorEvent = %+v", e)
			}
		},
	},
}

// TestRTDSDecodeEventTypes covers PriceUpdateEvent with a JSON-string
// Value (the doc-inconsistent alternative to the plain-number form
// exercised above), CommentEvent, ReactionEvent, and RTDSErrorEvent (the
// live-verified shape of a rejected subscribe request).
func TestRTDSDecodeEventTypes(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialRTDS(ctx, []RTDSSubscription{{Topic: TopicCryptoPrices, Type: "update"}},
		WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	for _, tc := range rtdsDecodeCases {
		t.Run(tc.name, func(t *testing.T) {
			sendText(t, sc, tc.frame)
			tc.check(t, readEvent(t, conn))
		})
	}
}

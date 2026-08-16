// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
)

func TestUserAuthFrame(t *testing.T) {
	srv := newFakeMarketServer(t) // the fake accept-and-hand-off server is channel-agnostic.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	creds := Credentials{APIKey: "key-1", Secret: "secret-1", Passphrase: "pass-1"}
	conn, err := DialUser(ctx, creds, []string{"0xcond1", "0xcond2"}, WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")

	var got userSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &got); err != nil {
		t.Fatalf("unmarshal subscribe frame: %v", err)
	}
	if got.Type != "user" {
		t.Errorf("Type = %q, want user", got.Type)
	}
	if got.Auth.APIKey != "key-1" || got.Auth.Secret != "secret-1" || got.Auth.Passphrase != "pass-1" {
		t.Errorf("Auth = %+v", got.Auth)
	}
	want := []string{"0xcond1", "0xcond2"}
	if len(got.Markets) != len(want) || got.Markets[0] != want[0] || got.Markets[1] != want[1] {
		t.Errorf("Markets = %v, want %v", got.Markets, want)
	}
}

// userDecodeCase is one table-driven case for TestUserDecodeEventTypes.
type userDecodeCase struct {
	name  string
	frame string
	check func(t *testing.T, ev Event)
}

var userDecodeCases = []userDecodeCase{
	{
		name: "order object",
		frame: `{"event_type":"order","id":"0xff35","owner":"9180014b","market":"0xbd31",` +
			`"asset_id":"52114","side":"SELL","order_owner":"9180014b","original_size":"10",` +
			`"size_matched":"0","price":"0.57","outcome":"YES","type":"PLACEMENT",` +
			`"created_at":"1672290687","order_type":"GTD","status":"LIVE","timestamp":"1672290687"}`,
		check: func(t *testing.T, ev Event) {
			o, ok := ev.(OrderEvent)
			if !ok {
				t.Fatalf("got %T, want OrderEvent", ev)
			}
			if o.ID != "0xff35" || o.Side != "SELL" || o.Type != "PLACEMENT" || o.Status != "LIVE" {
				t.Errorf("OrderEvent = %+v", o)
			}
		},
	},
	{
		name: "trade object",
		frame: `{"event_type":"trade","type":"TRADE","id":"28c4d2eb","taker_order_id":"0x06bc",` +
			`"market":"0xbd31","asset_id":"52114","side":"BUY","size":"10","price":"0.57",` +
			`"status":"MATCHED","owner":"9180014b","trader_side":"TAKER","timestamp":"1672290701",` +
			`"maker_orders":[{"order_id":"0xff35","owner":"9180","matched_amount":"10","price":"0.57","asset_id":"52114","outcome":"YES","side":"SELL"}]}`,
		check: func(t *testing.T, ev Event) {
			tr, ok := ev.(TradeEvent)
			if !ok {
				t.Fatalf("got %T, want TradeEvent", ev)
			}
			if tr.ID != "28c4d2eb" || tr.Status != "MATCHED" || tr.TraderSide != "TAKER" {
				t.Errorf("TradeEvent = %+v", tr)
			}
			if len(tr.MakerOrders) != 1 || tr.MakerOrders[0].OrderID != "0xff35" {
				t.Errorf("TradeEvent.MakerOrders = %+v", tr.MakerOrders)
			}
		},
	},
	{
		name:  "unknown event type",
		frame: `{"event_type":"something_else","x":1}`,
		check: func(t *testing.T, ev Event) {
			u, ok := ev.(UnknownEvent)
			if !ok {
				t.Fatalf("got %T, want UnknownEvent", ev)
			}
			if u.EventType != "something_else" {
				t.Errorf("UnknownEvent = %+v", u)
			}
		},
	},
}

func TestUserDecodeEventTypes(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialUser(ctx, Credentials{APIKey: "k", Secret: "s", Passphrase: "p"}, nil,
		WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial auth/subscribe frame

	for _, tc := range userDecodeCases {
		t.Run(tc.name, func(t *testing.T) {
			sendText(t, sc, tc.frame)
			tc.check(t, readEvent(t, conn))
		})
	}
}

func TestUserSubscribeMarkets(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialUser(ctx, Credentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		[]string{"0xcond1"}, WithURL(srv.url()))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial auth/subscribe frame

	if err := conn.Subscribe(ctx, []string{"0xcond2"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	var op userOperationRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &op); err != nil {
		t.Fatalf("unmarshal operation frame: %v", err)
	}
	if op.Operation != "subscribe" || len(op.Markets) != 1 || op.Markets[0] != "0xcond2" {
		t.Errorf("Subscribe frame = %+v", op)
	}
}

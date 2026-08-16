// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeMarketServer is a minimal stand-in for the CLOB market channel: an
// httptest.Server that upgrades every request to a websocket connection
// and hands each one to the test over a channel, so the test can script
// exactly what the fake server sends and observe exactly what it
// receives.
type fakeMarketServer struct {
	t      *testing.T
	server *httptest.Server
	conns  chan *websocket.Conn
}

func newFakeMarketServer(t *testing.T) *fakeMarketServer {
	t.Helper()
	f := &fakeMarketServer{t: t, conns: make(chan *websocket.Conn, 8)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(maxFrameSize)
		f.conns <- c
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMarketServer) url() string { return f.server.URL }

// nextConn waits for the next accepted server-side connection.
func (f *fakeMarketServer) nextConn(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a connection")
		return nil
	}
}

// readText reads the next text frame from the server side of a
// connection, failing the test on error or timeout.
func readText(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("server read message type = %v, want text", typ)
	}
	return string(data)
}

func sendText(t *testing.T, c *websocket.Conn, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("server write: %v", err)
	}
}

func readEvent(t *testing.T, conn *Conn) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ev, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Conn.Read: %v", err)
	}
	return ev
}

func dialTestMarket(t *testing.T, ctx context.Context, url string, assetIDs []string, opts ...MarketOption) *Conn {
	t.Helper()
	conn, err := DialMarket(ctx, assetIDs, append([]MarketOption{WithURL(url)}, opts...)...)
	if err != nil {
		t.Fatalf("DialMarket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestMarketSubscribeFrame(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialTestMarket(t, ctx, srv.url(), []string{"111", "222"})

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")

	var got marketSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &got); err != nil {
		t.Fatalf("unmarshal subscribe frame: %v", err)
	}
	if got.Type != "market" {
		t.Errorf("Type = %q, want market", got.Type)
	}
	if !got.InitialDump {
		t.Errorf("InitialDump = false, want true (default)")
	}
	if got.Level != 2 {
		t.Errorf("Level = %d, want 2 (default)", got.Level)
	}
	if got.CustomFeatureEnabled {
		t.Errorf("CustomFeatureEnabled = true, want false (default)")
	}
	want := []string{"111", "222"}
	if len(got.AssetIDs) != len(want) || got.AssetIDs[0] != want[0] || got.AssetIDs[1] != want[1] {
		t.Errorf("AssetIDs = %v, want %v", got.AssetIDs, want)
	}
}

func TestMarketSubscribeFrameOptions(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialTestMarket(t, ctx, srv.url(), []string{"1"}, WithLevel(3), WithCustomFeatureEnabled(true), WithInitialDump(false))

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")

	var got marketSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &got); err != nil {
		t.Fatalf("unmarshal subscribe frame: %v", err)
	}
	if got.Level != 3 || !got.CustomFeatureEnabled || got.InitialDump {
		t.Errorf("got %+v, want Level=3 CustomFeatureEnabled=true InitialDump=false", got)
	}
}

// marketDecodeCase is one table-driven case for TestMarketDecodeEventTypes.
type marketDecodeCase struct {
	name  string
	frame string
	check func(t *testing.T, ev Event)
}

var marketDecodeCases = []marketDecodeCase{
	{
		name:  "book array",
		frame: `[{"market":"0xabc","asset_id":"tok1","timestamp":"1","hash":"h1","bids":[{"price":"0.4","size":"10"}],"asks":[{"price":"0.6","size":"5"}],"tick_size":"0.001","event_type":"book","last_trade_price":"0.5"}]`,
		check: func(t *testing.T, ev Event) {
			b, ok := ev.(BookEvent)
			if !ok {
				t.Fatalf("got %T, want BookEvent", ev)
			}
			if b.AssetID != "tok1" || b.Hash != "h1" || len(b.Bids) != 1 || b.Bids[0].Price != "0.4" || b.TickSize != "0.001" || b.LastTradePrice != "0.5" {
				t.Errorf("BookEvent = %+v", b)
			}
		},
	},
	{
		name:  "price_change object",
		frame: `{"market":"0xabc","price_changes":[{"asset_id":"tok1","price":"0.4","size":"10","side":"BUY","hash":"h2","best_bid":"0.4","best_ask":"0.41"}],"timestamp":"2","event_type":"price_change"}`,
		check: func(t *testing.T, ev Event) {
			p, ok := ev.(PriceChangeEvent)
			if !ok {
				t.Fatalf("got %T, want PriceChangeEvent", ev)
			}
			if len(p.PriceChanges) != 1 || p.PriceChanges[0].Side != "BUY" || p.PriceChanges[0].BestAsk != "0.41" {
				t.Errorf("PriceChangeEvent = %+v", p)
			}
		},
	},
	{
		name:  "tick_size_change object",
		frame: `{"event_type":"tick_size_change","asset_id":"tok1","market":"0xabc","old_tick_size":"0.01","new_tick_size":"0.001","timestamp":"3"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(TickSizeChangeEvent)
			if !ok {
				t.Fatalf("got %T, want TickSizeChangeEvent", ev)
			}
			if e.OldTickSize != "0.01" || e.NewTickSize != "0.001" {
				t.Errorf("TickSizeChangeEvent = %+v", e)
			}
		},
	},
	{
		name:  "last_trade_price object",
		frame: `{"event_type":"last_trade_price","asset_id":"tok1","market":"0xabc","price":"0.456","size":"219.2","fee_rate_bps":"0","side":"BUY","timestamp":"4","transaction_hash":"0xdead"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(LastTradePriceEvent)
			if !ok {
				t.Fatalf("got %T, want LastTradePriceEvent", ev)
			}
			if e.Price != "0.456" || e.TransactionHash != "0xdead" {
				t.Errorf("LastTradePriceEvent = %+v", e)
			}
		},
	},
	{
		name:  "best_bid_ask object",
		frame: `{"event_type":"best_bid_ask","market":"0xabc","asset_id":"tok1","best_bid":"0.73","best_ask":"0.77","spread":"0.04","timestamp":"5"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(BestBidAskEvent)
			if !ok {
				t.Fatalf("got %T, want BestBidAskEvent", ev)
			}
			if e.Spread != "0.04" {
				t.Errorf("BestBidAskEvent = %+v", e)
			}
		},
	},
	{
		name:  "new_market object",
		frame: `{"event_type":"new_market","id":"i1","question":"Q?","market":"0xabc","slug":"s","assets_ids":["a","b"],"outcomes":["Yes","No"],"timestamp":"6"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(NewMarketEvent)
			if !ok {
				t.Fatalf("got %T, want NewMarketEvent", ev)
			}
			if len(e.AssetIDs) != 2 || e.Outcomes[0] != "Yes" {
				t.Errorf("NewMarketEvent = %+v", e)
			}
		},
	},
	{
		name:  "market_resolved object",
		frame: `{"event_type":"market_resolved","id":"i1","market":"0xabc","assets_ids":["a","b"],"winning_asset_id":"a","winning_outcome":"Yes","timestamp":"7"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(MarketResolvedEvent)
			if !ok {
				t.Fatalf("got %T, want MarketResolvedEvent", ev)
			}
			if e.WinningAssetID != "a" {
				t.Errorf("MarketResolvedEvent = %+v", e)
			}
		},
	},
	{
		name:  "unknown event type",
		frame: `{"event_type":"something_new","foo":"bar"}`,
		check: func(t *testing.T, ev Event) {
			e, ok := ev.(UnknownEvent)
			if !ok {
				t.Fatalf("got %T, want UnknownEvent", ev)
			}
			if e.EventType != "something_new" || !strings.Contains(string(e.Raw), "foo") {
				t.Errorf("UnknownEvent = %+v", e)
			}
		},
	},
}

func TestMarketDecodeEventTypes(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialTestMarket(t, ctx, srv.url(), []string{"tok1"})
	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	for _, tc := range marketDecodeCases {
		t.Run(tc.name, func(t *testing.T) {
			sendText(t, sc, tc.frame)
			tc.check(t, readEvent(t, conn))
		})
	}
}

// TestMarketPingKeepalive confirms the client sends the literal text frame
// "PING" on its keepalive interval, and that a "PONG" reply is swallowed
// rather than delivered as an Event.
func TestMarketPingKeepalive(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	if got := readText(t, sc); got != pingText {
		t.Fatalf("client sent %q, want %q", got, pingText)
	}
	sendText(t, sc, pongText)

	// A real event sent right after PONG must still arrive: PONG must not
	// have been mistaken for, or blocked delivery of, a real event.
	sendText(t, sc, `{"event_type":"tick_size_change","asset_id":"tok1","market":"0xabc","old_tick_size":"0.01","new_tick_size":"0.001","timestamp":"1"}`)
	ev := readEvent(t, conn)
	if _, ok := ev.(TickSizeChangeEvent); !ok {
		t.Fatalf("got %T after PONG, want TickSizeChangeEvent", ev)
	}
}

// TestMarketReconnect drops the first connection abruptly and confirms the
// Conn reconnects, resends its subscription, and delivers a ReconnectEvent
// before resuming normal event delivery.
func TestMarketReconnect(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialTestMarket(t, ctx, srv.url(), []string{"tok1"})

	sc1 := srv.nextConn(t)
	firstFrame := readText(t, sc1)
	sc1.Close(websocket.StatusInternalError, "simulated drop")

	ev := readEvent(t, conn)
	rec, ok := ev.(ReconnectEvent)
	if !ok {
		t.Fatalf("got %T, want ReconnectEvent", ev)
	}
	if rec.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", rec.Attempt)
	}
	if rec.Cause == nil {
		t.Errorf("Cause = nil, want non-nil")
	}

	sc2 := srv.nextConn(t)
	defer sc2.Close(websocket.StatusNormalClosure, "")
	secondFrame := readText(t, sc2)
	if secondFrame != firstFrame {
		t.Errorf("resent subscribe frame = %s, want identical to first: %s", secondFrame, firstFrame)
	}

	sendText(t, sc2, `{"event_type":"tick_size_change","asset_id":"tok1","market":"0xabc","old_tick_size":"0.01","new_tick_size":"0.001","timestamp":"1"}`)
	ev2 := readEvent(t, conn)
	if _, ok := ev2.(TickSizeChangeEvent); !ok {
		t.Fatalf("got %T after reconnect, want TickSizeChangeEvent", ev2)
	}
}

// TestMarketSubscribeUnsubscribe confirms Subscribe and Unsubscribe send
// the documented operation frame over the live connection.
func TestMarketSubscribeUnsubscribe(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialTestMarket(t, ctx, srv.url(), []string{"tok1"})
	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	if err := conn.Subscribe(ctx, []string{"tok2"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	var op marketOperationRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &op); err != nil {
		t.Fatalf("unmarshal operation frame: %v", err)
	}
	if op.Operation != "subscribe" || len(op.AssetIDs) != 1 || op.AssetIDs[0] != "tok2" {
		t.Errorf("Subscribe frame = %+v", op)
	}

	if err := conn.Unsubscribe(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	var op2 marketOperationRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &op2); err != nil {
		t.Fatalf("unmarshal operation frame: %v", err)
	}
	if op2.Operation != "unsubscribe" || len(op2.AssetIDs) != 1 || op2.AssetIDs[0] != "tok1" {
		t.Errorf("Unsubscribe frame = %+v", op2)
	}
}

// TestMarketContextCancelShutsDown confirms canceling the context passed
// to the Dial constructor makes Read return promptly and Close return
// without hanging.
func TestMarketContextCancelShutsDown(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(clobPingInterval))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")
	readText(t, sc) // discard the initial subscribe frame

	cancel()

	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	if _, err := conn.Read(readCtx); err == nil {
		t.Fatal("Read after context cancellation returned nil error")
	}

	done := make(chan struct{})
	go func() {
		conn.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return within 5s of context cancellation")
	}
}

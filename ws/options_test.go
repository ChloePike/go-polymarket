// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestWithURLReachesSomewhereElse checks the option that makes this package
// usable at all against anything but production: a proxy, a regional
// endpoint, a recorded fixture, or a test server. Every other test in this
// package depends on it, which is the point — the public API is the only way
// in.
func TestWithURLReachesSomewhereElse(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()))
	if err != nil {
		t.Fatalf("DialMarket with an overridden URL: %v", err)
	}
	defer conn.Close()

	// The subscribe frame arriving at the fake server is the proof that the
	// connection went where the option said and not to production.
	sc := srv.nextConn(t)
	defer sc.Close(websocket.StatusNormalClosure, "")

	var got marketSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc)), &got); err != nil {
		t.Fatalf("unmarshal subscribe frame: %v", err)
	}
	if len(got.AssetIDs) != 1 || got.AssetIDs[0] != "tok1" {
		t.Errorf("AssetIDs = %v, want [tok1]", got.AssetIDs)
	}
}

// TestDefaultURLsAreProduction guards the constants a caller gets when they
// pass no URL at all.
func TestDefaultURLsAreProduction(t *testing.T) {
	for _, tc := range []struct{ name, got, want string }{
		{"market", MarketURL, "wss://ws-subscriptions-clob.polymarket.com/ws/market"},
		{"user", UserURL, "wss://ws-subscriptions-clob.polymarket.com/ws/user"},
		{"rtds", RTDSURL, "wss://ws-live-data.polymarket.com"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s URL = %s, want %s", tc.name, tc.got, tc.want)
		}
	}
}

// TestWithPingIntervalApplies checks that the keepalive cadence is settable,
// which a fixture server needs in order to observe a PING at all within a
// test's lifetime.
func TestWithPingIntervalApplies(t *testing.T) {
	cfg := marketConfig{endpoint: endpoint{url: MarketURL, pingInterval: clobPingInterval}}
	WithPingInterval(5 * time.Millisecond).applyMarket(&cfg)
	if cfg.pingInterval != 5*time.Millisecond {
		t.Errorf("pingInterval = %v, want 5ms", cfg.pingInterval)
	}
	WithURL("ws://example.invalid/x").applyMarket(&cfg)
	if cfg.url != "ws://example.invalid/x" {
		t.Errorf("url = %s", cfg.url)
	}

	// The same options reach the other two channels.
	var u userConfig
	WithURL("ws://u").applyUser(&u)
	var r rtdsConfig
	WithURL("ws://r").applyRTDS(&r)
	if u.url != "ws://u" || r.url != "ws://r" {
		t.Errorf("user=%s rtds=%s", u.url, r.url)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Item 4: a subscription added after connect must survive a reconnect.
func TestAdvSubscribeSurvivesReconnect(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialTestMarket(t, ctx, srv.url(), []string{"tok1"})
	sc1 := srv.nextConn(t)
	readText(t, sc1) // initial subscribe

	if err := conn.Subscribe(ctx, []string{"tok2"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	readText(t, sc1) // the dynamic operation frame

	sc1.Close(websocket.StatusInternalError, "drop")

	sc2 := srv.nextConn(t)
	defer sc2.Close(websocket.StatusNormalClosure, "")
	var got marketSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc2)), &got); err != nil {
		t.Fatalf("unmarshal resent subscribe: %v", err)
	}
	if len(got.AssetIDs) != 2 || got.AssetIDs[0] != "tok1" || got.AssetIDs[1] != "tok2" {
		t.Fatalf("resent AssetIDs = %v, want [tok1 tok2]", got.AssetIDs)
	}
}

// Item 8: every goroutine the package starts must stop on Close / ctx cancel.
func TestAdvNoGoroutineLeak(t *testing.T) {
	srv := newFakeMarketServer(t)

	settle := func() int {
		for i := 0; i < 100; i++ {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}
	base := settle()

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		sub := newMarketSubscription([]string{"tok1"})
		conn, err := newConn(ctx, srv.url(), sub, decodeMarket, 20*time.Millisecond)
		if err != nil {
			t.Fatalf("newConn: %v", err)
		}
		sc := srv.nextConn(t)
		readText(t, sc)
		if err := conn.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		cancel()
		sc.CloseNow()
	}

	after := settle()
	if after > base+4 {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutine leak: base=%d after=%d\n%s", base, after, buf[:n])
	}
}

// Item 8b: Close must return promptly even while a reconnect backoff is in
// flight against a server that is gone for good.
func TestAdvCloseDuringReconnectBackoff(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := newMarketSubscription([]string{"tok1"})
	conn, err := newConn(ctx, srv.url(), sub, decodeMarket, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	sc := srv.nextConn(t)
	readText(t, sc)

	srv.server.Close() // whole server gone: reconnect can never succeed
	sc.CloseNow()

	time.Sleep(300 * time.Millisecond)

	done := make(chan struct{})
	go func() { conn.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("Close hung during reconnect backoff\n%s", buf[:n])
	}
}

// Item 9: many concurrent writers (Subscribe/Unsubscribe) racing the
// keepalive goroutine and the reconnect resubscribe.
func TestAdvConcurrentWriters(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := newMarketSubscription([]string{"tok1"})
	conn, err := newConn(ctx, srv.url(), sub, decodeMarket, time.Millisecond)
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()

	sc := srv.nextConn(t)
	// Drain the server side so writes never block on flow control.
	go func() {
		for {
			if _, _, err := sc.Read(context.Background()); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				id := strings.Repeat("x", i+1)
				_ = conn.Subscribe(context.Background(), []string{id})
				_ = conn.Unsubscribe(context.Background(), []string{id})
			}
		}(i)
	}
	wg.Wait()
}

// Item 9b: RTDS Subscribe must not touch the socket at all.
func TestAdvRTDSSubscribeUnsupported(t *testing.T) {
	s := &rtdsSubscription{}
	if _, err := s.change(true, []string{"a"}); err != ErrDynamicSubscribeUnsupported {
		t.Fatalf("change = %v, want ErrDynamicSubscribeUnsupported", err)
	}
}

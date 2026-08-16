// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// drain reads and discards every Event a Conn delivers until it fails, so
// readLoop never blocks handing a ReconnectEvent to a caller that is not
// there. A Conn with nobody calling Read stalls after its first reconnect,
// which would otherwise make the tests below hang rather than fail.
func drain(ctx context.Context, conn *Conn) {
	go func() {
		for {
			if _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
}

// TestSubscribeSurvivesReconnect confirms an asset added with Subscribe
// after the connection was established is present in the subscribe frame
// resent on the next reconnect, not just in the one-off dynamic operation
// frame that added it.
func TestSubscribeSurvivesReconnect(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := dialTestMarket(t, ctx, srv.url(), []string{"tok1"})
	sc1 := srv.nextConn(t)
	readText(t, sc1) // initial subscribe frame

	if err := conn.Subscribe(ctx, []string{"tok2"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	readText(t, sc1) // dynamic operation frame

	sc1.Close(websocket.StatusInternalError, "simulated drop")

	sc2 := srv.nextConn(t)
	defer sc2.Close(websocket.StatusNormalClosure, "")
	var got marketSubscribeRequest
	if err := json.Unmarshal([]byte(readText(t, sc2)), &got); err != nil {
		t.Fatalf("unmarshal resent subscribe frame: %v", err)
	}
	want := []string{"tok1", "tok2"}
	if len(got.AssetIDs) != len(want) || got.AssetIDs[0] != want[0] || got.AssetIDs[1] != want[1] {
		t.Errorf("resent AssetIDs = %v, want %v", got.AssetIDs, want)
	}
}

// lowestFreeFD returns the lowest unused file descriptor number. Descriptors
// are handed out lowest-first, so a value that climbs as a test runs means
// descriptors are accumulating.
func lowestFreeFD(t *testing.T) int {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	n := int(f.Fd())
	f.Close()
	return n
}

// TestReconnectClosesDroppedConnection is a regression test for a
// descriptor leak: github.com/coder/websocket does not close the underlying
// socket when a read fails with a plain I/O error (only when it receives a
// close frame), so a reconnect loop that abandons the dead connection
// without closing it leaks one descriptor per reconnect. Polymarket's edge
// drops connections abruptly and often, so that adds up fast.
func TestReconnectClosesDroppedConnection(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(time.Hour))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()
	drain(ctx, conn)

	sc := srv.nextConn(t)
	readText(t, sc)

	const drops = 12
	var base int
	for i := 0; i < drops; i++ {
		sc.CloseNow() // abrupt TCP close: no websocket close frame
		select {
		case c := <-srv.conns:
			sc = c
		case <-time.After(10 * time.Second):
			t.Fatalf("no reconnect after drop %d", i)
		}
		readText(t, sc) // resent subscribe frame
		if i == 1 {
			base = lowestFreeFD(t)
		}
	}
	sc.CloseNow()
	if got := lowestFreeFD(t); got > base+2 {
		t.Errorf("lowest free fd grew from %d to %d over %d reconnects: dropped connections are not being closed", base, got, drops)
	}
}

// TestCloseStopsEveryGoroutine confirms Close (and cancellation of the Dial
// context) stops both goroutines a Conn starts, leaving none behind.
func TestCloseStopsEveryGoroutine(t *testing.T) {
	srv := newFakeMarketServer(t)

	settle := func() int {
		var n int
		for i := 0; i < 40; i++ {
			runtime.GC()
			time.Sleep(5 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}
	base := settle()

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(20*time.Millisecond))
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

	if after := settle(); after > base+2 {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutines leaked: %d before, %d after\n%s", base, after, buf[:n])
	}
}

// TestCloseDuringReconnectBackoff confirms Close returns promptly even
// while the reconnect loop is waiting out a backoff against a server that
// is never coming back.
func TestCloseDuringReconnectBackoff(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(20*time.Millisecond))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	sc := srv.nextConn(t)
	readText(t, sc)

	srv.server.Close() // the whole server is gone: no reconnect can succeed
	sc.CloseNow()
	time.Sleep(300 * time.Millisecond)

	done := make(chan struct{})
	go func() { conn.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("Close hung while a reconnect backoff was in flight\n%s", buf[:n])
	}
}

// TestConcurrentWriters hammers the connection with Subscribe and
// Unsubscribe from many goroutines while the keepalive goroutine writes on
// its own schedule, since github.com/coder/websocket permits only one open
// writer at a time.
func TestConcurrentWriters(t *testing.T) {
	srv := newFakeMarketServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := DialMarket(ctx, []string{"tok1"}, WithURL(srv.url()), WithPingInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("newConn: %v", err)
	}
	defer conn.Close()
	drain(ctx, conn)

	sc := srv.nextConn(t)
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
			id := strings.Repeat("x", i+1)
			for j := 0; j < 50; j++ {
				_ = conn.Subscribe(context.Background(), []string{id})
				_ = conn.Unsubscribe(context.Background(), []string{id})
			}
		}(i)
	}
	wg.Wait()
}

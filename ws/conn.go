// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ErrDynamicSubscribeUnsupported is returned by Subscribe and Unsubscribe
// on a Conn whose channel has no documented way to add or remove a
// subscription without reconnecting. Currently that is every Conn DialRTDS
// returns.
var ErrDynamicSubscribeUnsupported = errors.New("ws: dynamic subscribe/unsubscribe not supported on this channel")

// subscription builds and mutates the frames a Conn sends to establish and
// adjust its server-side subscription. Each Dial constructor supplies the
// implementation matching its channel's wire protocol; Conn itself knows
// nothing about assets_ids, markets, or RTDS topics. Implementations must
// be safe for concurrent use: initial and change can each be called while
// the other is in progress, from the background reconnect goroutine and a
// caller goroutine respectively.
type subscription interface {
	// initial returns the frame to send right after the socket opens, and
	// again after every reconnect, reflecting the subscription's current
	// desired state.
	initial() []byte
	// change adds (add==true) or removes (add==false) ids from the desired
	// state and returns the dynamic frame to send immediately for that
	// change, or reports that this channel supports no such operation via
	// ErrDynamicSubscribeUnsupported.
	change(add bool, ids []string) ([]byte, error)
}

// Conn is one Polymarket streaming connection: the CLOB market channel,
// the CLOB user channel, or the RTDS live-data feed. Construct one with
// DialMarket, DialUser, or DialRTDS.
//
// A Conn owns two background goroutines for as long as it is open: one
// sends the keepalive text frame on the interval documented for its host,
// and one reads inbound frames, decodes them into Events, and reconnects
// with backoff (resending the current subscription) whenever the
// connection drops. See the package doc for the full reconnection and
// keepalive policy.
//
// Read must be called from one goroutine at a time. Subscribe,
// Unsubscribe, and Close may be called concurrently with Read and with
// each other.
type Conn struct {
	dialURL      string
	sub          subscription
	decode       func([]byte) ([]Event, error)
	pingInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	current    atomic.Pointer[websocket.Conn]
	events     chan Event
	reconnects atomic.Uint64

	wg sync.WaitGroup
}

// connectAndSubscribeOnce dials url a single time, applies the read-size
// limit, and sends sub's current initial frame. On any failure it closes
// the partial connection and returns the error; it does not retry.
func connectAndSubscribeOnce(ctx context.Context, url string, sub subscription) (*websocket.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := websocket.Dial(dctx, url, nil)
	cancel()
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(maxFrameSize)

	wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
	err = conn.Write(wctx, websocket.MessageText, sub.initial())
	wcancel()
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	return conn, nil
}

// backoffDuration returns how long to wait before the given 1-based retry
// attempt, per the policy documented in the package doc: initialBackoff
// doubled once per prior attempt, capped at maxBackoff, plus up to
// backoffJitterPercent% extra.
func backoffDuration(attempt int) time.Duration {
	d := initialBackoff
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(d)*backoffJitterPercent/100 + 1))
	return d + jitter
}

// sleepBackoff waits out backoffDuration(attempt), or returns ctx's error
// early if ctx is done first.
func sleepBackoff(ctx context.Context, attempt int) error {
	t := time.NewTimer(backoffDuration(attempt))
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newConn performs the bounded initial connection attempt (retrying past
// the transient failures documented in the package doc), then starts the
// background reconnect and keepalive loops, and returns a ready Conn.
func newConn(parent context.Context, url string, sub subscription, decode func([]byte) ([]Event, error), pingInterval time.Duration) (*Conn, error) {
	var conn *websocket.Conn
	var err error
	for attempt := 1; attempt <= maxInitialDialAttempts; attempt++ {
		conn, err = connectAndSubscribeOnce(parent, url, sub)
		if err == nil {
			break
		}
		if attempt == maxInitialDialAttempts {
			return nil, fmt.Errorf("ws: dial %s: %w", url, err)
		}
		if serr := sleepBackoff(parent, attempt); serr != nil {
			return nil, serr
		}
	}

	cctx, cancel := context.WithCancel(parent)
	c := &Conn{
		dialURL:      url,
		sub:          sub,
		decode:       decode,
		pingInterval: pingInterval,
		ctx:          cctx,
		cancel:       cancel,
		events:       make(chan Event),
	}
	c.current.Store(conn)

	c.wg.Add(2)
	go c.readLoop()
	go c.pingLoop()
	return c, nil
}

// reconnect waits out the backoff for attempt, then dials and resubscribes
// repeatedly until one succeeds or c.ctx is done.
func (c *Conn) reconnect(attempt int) (*websocket.Conn, error) {
	for {
		if err := sleepBackoff(c.ctx, attempt); err != nil {
			return nil, err
		}
		conn, err := connectAndSubscribeOnce(c.ctx, c.dialURL, c.sub)
		if err == nil {
			return conn, nil
		}
		attempt++
	}
}

// readLoop owns the exclusive Read call on the current underlying
// connection for the Conn's whole lifetime, across reconnects. It is the
// only goroutine that ever calls (*websocket.Conn).Read.
func (c *Conn) readLoop() {
	defer c.wg.Done()
	defer close(c.events)
	defer func() {
		if conn := c.current.Load(); conn != nil {
			conn.CloseNow()
		}
	}()

	attempt := 0
	for {
		conn := c.current.Load()
		typ, data, err := conn.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			// Release the dead connection's descriptor before dialing a
			// replacement. github.com/coder/websocket does not close the
			// underlying socket on a plain I/O read error -- only on a
			// received close frame, or eventually via a finalizer -- and
			// Polymarket's edge drops connections abruptly (no close
			// frame) often enough that leaking one descriptor per
			// reconnect would exhaust a long-lived process.
			conn.CloseNow()
			attempt++
			newConn, rerr := c.reconnect(attempt)
			if rerr != nil {
				// c.ctx was canceled while reconnecting.
				return
			}
			c.current.Store(newConn)
			n := c.reconnects.Add(1)
			select {
			case c.events <- ReconnectEvent{Attempt: n, Cause: err}:
			case <-c.ctx.Done():
				return
			}
			attempt = 0
			continue
		}

		if typ != websocket.MessageText {
			continue
		}
		evs, derr := c.decode(data)
		if derr != nil {
			// A malformed frame is dropped, not fatal: one bad frame
			// should not tear down an otherwise-healthy connection.
			continue
		}
		for _, ev := range evs {
			select {
			case c.events <- ev:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// pingLoop writes the keepalive text frame on c.pingInterval for as long
// as c.ctx is not done. Write is safe to call concurrently with the
// exclusive Read happening in readLoop (see github.com/coder/websocket's
// Conn doc), so this runs as its own goroutine rather than sharing
// readLoop's.
func (c *Conn) pingLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			conn := c.current.Load()
			if conn == nil {
				continue
			}
			wctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			_ = conn.Write(wctx, websocket.MessageText, []byte(pingText))
			cancel()
		}
	}
}

// Read blocks until the next Event arrives, ctx is done, or the Conn is
// closed (via Close, or via cancellation of the context passed to the
// Dial constructor that created it). Once closed, every subsequent Read
// call returns immediately with that closing error.
func (c *Conn) Read(ctx context.Context) (Event, error) {
	select {
	case ev, ok := <-c.events:
		if !ok {
			if err := c.ctx.Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		}
		return ev, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts the connection down: it stops the keepalive and reconnect
// goroutines, closes the underlying websocket, and causes every pending
// and future Read call to return an error. Close is safe to call more
// than once and safe to call concurrently with Read, Subscribe, and
// Unsubscribe.
func (c *Conn) Close() error {
	c.cancel()
	c.wg.Wait()
	return nil
}

// change sends sub's dynamic add/remove frame for ids over the current
// connection, after durably recording the change in sub's desired state
// (via subscription.change) so it is also included in every future
// resubscribe. If sending fails right now -- for example because a
// reconnect is in flight -- the recorded change is not lost: it still
// takes effect on the next successful (re)connect. The returned error
// reflects only whether this immediate dynamic frame was sent.
func (c *Conn) change(ctx context.Context, add bool, ids []string) error {
	frame, err := c.sub.change(add, ids)
	if err != nil {
		return err
	}
	conn := c.current.Load()
	return conn.Write(ctx, websocket.MessageText, frame)
}

// Subscribe adds ids to the connection's subscription. On a Conn from
// DialMarket, ids are CLOB token IDs; on a Conn from DialUser, they are
// condition IDs. A Conn from DialRTDS returns ErrDynamicSubscribeUnsupported.
func (c *Conn) Subscribe(ctx context.Context, ids []string) error {
	return c.change(ctx, true, ids)
}

// Unsubscribe removes ids from the connection's subscription. See
// Subscribe for what ids means per Dial constructor.
func (c *Conn) Unsubscribe(ctx context.Context, ids []string) error {
	return c.change(ctx, false, ids)
}

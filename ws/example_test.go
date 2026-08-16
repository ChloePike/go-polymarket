// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChloePike/go-polymarket/ws"
)

const tokenID = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

// The market channel is public: no wallet, no API key. Dial it, then read
// events until the context is done. The connection reconnects and resubscribes
// on its own; a ReconnectEvent tells you it happened, so a local book can be
// rebuilt rather than silently drifting.
func Example() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ws.DialMarket(ctx, []string{tokenID})
	if err != nil {
		slog.Error("dialing the market channel", "err", err)
		return
	}
	defer conn.Close()

	for {
		event, err := conn.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				slog.Error("reading", "err", err)
			}
			return
		}
		switch e := event.(type) {
		case ws.BookEvent:
			fmt.Printf("book: %d bids, %d asks\n", len(e.Bids), len(e.Asks))
		case ws.PriceChangeEvent:
			fmt.Println("price change")
		case ws.ReconnectEvent:
			// The subscription is already back; the local book is not.
			slog.Info("reconnected, rebuild any local state")
		}
	}
}

// A subscription can change while the connection is up, and it survives a
// reconnect: what is resent after a drop is the current set, not the set the
// connection was opened with.
func ExampleConn_Subscribe() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn, err := ws.DialMarket(ctx, []string{tokenID})
	if err != nil {
		slog.Error("dialing the market channel", "err", err)
		return
	}
	defer conn.Close()

	other := "21742633143463906290569050155826241533067272736897614950488156847949938836455"
	if err := conn.Subscribe(ctx, []string{other}); err != nil {
		slog.Error("subscribing", "err", err)
		return
	}
	if err := conn.Unsubscribe(ctx, []string{tokenID}); err != nil {
		slog.Error("unsubscribing", "err", err)
		return
	}
}

// The user channel reports your own orders and fills. It authenticates with
// the level-2 credentials, carried inside the subscribe message rather than in
// headers — obtain them with the clob package's key handshake.
func ExampleDialUser() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn, err := ws.DialUser(ctx, ws.Credentials{
		APIKey:     "…",
		Secret:     "…",
		Passphrase: "…",
	}, nil) // nil markets means every market
	if err != nil {
		slog.Error("dialing the user channel", "err", err)
		return
	}
	defer conn.Close()

	for {
		event, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch e := event.(type) {
		case ws.OrderEvent:
			fmt.Println("order", e.ID, e.Status)
		case ws.TradeEvent:
			fmt.Println("trade", e.ID, e.Status)
		}
	}
}

// A book snapshot carries a hash so a client keeping its own copy can tell
// whether it has drifted. It is a SHA-1 over the summary with its own hash
// field blanked, which makes the server's field order part of the input.
func ExampleBookHash() {
	book := ws.Book{
		Market:         "0x699d…",
		AssetID:        tokenID,
		Timestamp:      "1786864634000",
		Bids:           []ws.PriceLevel{{Price: "0.49", Size: "100"}},
		Asks:           []ws.PriceLevel{{Price: "0.51", Size: "100"}},
		MinOrderSize:   "5",
		TickSize:       "0.01",
		NegRisk:        false,
		LastTradePrice: "0.50",
	}
	fmt.Println(len(ws.BookHash(book)), "hex characters")
	// Output: 40 hex characters
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command watch streams a Polymarket order book live and prints the top of
// book every time it moves.
//
// It is read-only: the market channel is public, so this needs no wallet and
// no API key.
//
//	go run ./examples/watch -token 71321045679252212594626385532706912750332728571942532289631379312455583992563
//
// With no -token it picks an active sporting fixture, so it runs with no
// arguments at all. Ctrl-C to stop.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ChloePike/go-polymarket/clob"
	"github.com/ChloePike/go-polymarket/ws"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	token := flag.String("token", "", "outcome token id; empty picks a sporting fixture")
	limit := flag.Duration("for", 0, "stop after this long; zero runs until interrupted")
	flag.Parse()

	// Ctrl-C cancels the context, which stops every goroutine the stream owns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *limit)
		defer cancel()
	}

	id := *token
	if id == "" {
		var err error
		if id, err = pickToken(ctx); err != nil {
			slog.Error("choosing a market", "err", err)
			os.Exit(1)
		}
	}

	conn, err := ws.DialMarket(ctx, []string{id})
	if err != nil {
		slog.Error("dialing the market channel", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("watching %s\n\n", id)
	for {
		event, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				fmt.Println("\nstopped")
				return
			}
			slog.Error("reading", "err", err)
			os.Exit(1)
		}
		report(event)
	}
}

// report prints one line per event, which is the whole point: a stream is
// only useful if you can see it move.
func report(event ws.Event) {
	stamp := time.Now().Format("15:04:05")
	switch e := event.(type) {
	case ws.BookEvent:
		bid, bidSize := top(e.Bids)
		ask, askSize := top(e.Asks)
		fmt.Printf("%s  book    %8s x %-12s | %-8s x %s   (%d/%d levels)\n",
			stamp, bid, bidSize, ask, askSize, len(e.Bids), len(e.Asks))
	case ws.PriceChangeEvent:
		fmt.Printf("%s  change  %d level(s)\n", stamp, len(e.PriceChanges))
	case ws.LastTradePriceEvent:
		fmt.Printf("%s  trade   %s x %s\n", stamp, e.Price, e.Size)
	case ws.TickSizeChangeEvent:
		fmt.Printf("%s  tick    %s -> %s\n", stamp, e.OldTickSize, e.NewTickSize)
	case ws.ReconnectEvent:
		// The subscription is already restored; a local book is not.
		fmt.Printf("%s  RECONNECTED (attempt %d, after %v) — rebuild any local state\n",
			stamp, e.Attempt, e.Cause)
	}
}

// top returns the price and size nearest the midpoint. Both sides arrive
// worst-price-first, so that is the last entry.
func top(levels []ws.PriceLevel) (price, size string) {
	if len(levels) == 0 {
		return "-", "-"
	}
	best := levels[len(levels)-1]
	return best.Price, best.Size
}

// pickToken finds an active sporting fixture, for the same reason the book
// example does: the live markets are whatever Polymarket is running today,
// and a demo should show a stream rather than a question.
func pickToken(ctx context.Context) (string, error) {
	c := clob.New()
	scanned := 0
	for m, err := range clob.Pages(ctx, c.SamplingMarkets) {
		if err != nil {
			return "", err
		}
		if scanned++; scanned > 2000 {
			break
		}
		if !m.Active || m.Closed || !isFixture(m.Question) {
			continue
		}
		for _, t := range m.Tokens {
			if t.TokenID != "" {
				slog.Info("chose a market", "question", m.Question)
				return t.TokenID, nil
			}
		}
	}
	return "", fmt.Errorf("no sporting fixture in %d markets; pass -token", scanned)
}

func isFixture(question string) bool {
	return strings.Contains(question, " vs. ") || strings.Contains(question, "O/U")
}

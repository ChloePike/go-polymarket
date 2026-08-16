// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command book prints the order book for one Polymarket outcome token.
//
// It is read-only: no wallet, no API key, no funds. A Polymarket price is a
// probability, so a best bid of 0.52 means the market will pay 52 cents for a
// claim that pays a dollar if the outcome happens.
//
//	go run ./examples/book -token 71321045679252212594626385532706912750332728571942532289631379312455583992563
//
// With no -token it picks an active one from the sampling markets, so it runs
// with no arguments at all.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("book: ")

	token := flag.String("token", "", "outcome token id; empty picks an active market")
	depth := flag.Int("depth", 8, "price levels to show on each side")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var c polymarket.Client

	id := *token
	if id == "" {
		var err error
		if id, err = pickToken(ctx, &c); err != nil {
			log.Fatal(err)
		}
	}

	book, err := c.OrderBook(ctx, id)
	if err != nil {
		log.Fatal(err)
	}
	mid, err := c.Midpoint(ctx, id)
	if err != nil {
		log.Fatal(err)
	}
	spread, err := c.Spread(ctx, id)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("token      %s\n", id)
	fmt.Printf("market     %s\n", book.Market)
	fmt.Printf("tick size  %s   min order %s   neg-risk %v\n",
		book.TickSize, book.MinOrderSize, book.NegRisk)
	fmt.Printf("midpoint   %s   spread %s   last trade %s\n\n",
		mid, spread, book.LastTradePrice)

	// Both sides arrive worst-price-first, so the top of book is the last
	// entry of each slice.
	fmt.Println("            asks")
	for _, level := range topOf(book.Asks, *depth) {
		fmt.Printf("  %8s  %14s\n", level.Price, level.Size)
	}
	fmt.Println("  " + strings.Repeat("-", 24))
	for _, level := range reverse(topOf(book.Bids, *depth)) {
		fmt.Printf("  %8s  %14s\n", level.Price, level.Size)
	}
	fmt.Println("            bids")
}

// pickToken finds an active token so the command runs with no arguments.
//
// It deliberately restricts itself to sporting fixtures. The sampling markets
// are whatever Polymarket is running today, which includes elections and
// other contested subjects; an example command should demonstrate an order
// book, not put a political question in front of the reader. Sports fixtures
// are plentiful, liquid, and beside the point, which is exactly what a demo
// wants.
func pickToken(ctx context.Context, c *polymarket.Client) (string, error) {
	// Fixtures are plentiful but not necessarily on the first page, so walk
	// a few. Pages handles the cursor sentinels.
	const scanLimit = 2000
	scanned := 0
	for m, err := range polymarket.Pages(ctx, c.SamplingMarkets) {
		if err != nil {
			return "", err
		}
		if scanned++; scanned > scanLimit {
			break
		}
		if !m.Active || m.Closed || !isFixture(m.Question) {
			continue
		}
		for _, t := range m.Tokens {
			if t.TokenID != "" {
				fmt.Fprintf(os.Stderr, "using %q\n", m.Question)
				return t.TokenID, nil
			}
		}
	}
	return "", fmt.Errorf("no sporting fixture in %d markets; pass -token to choose one yourself", scanned)
}

// isFixture reports whether a market question reads as a sporting fixture:
// "A vs. B", or a total-points line on one.
func isFixture(question string) bool {
	return strings.Contains(question, " vs. ") || strings.Contains(question, "O/U")
}

// topOf returns the n price levels nearest the midpoint.
func topOf(levels []polymarket.OrderSummary, n int) []polymarket.OrderSummary {
	if len(levels) <= n {
		return levels
	}
	return levels[len(levels)-n:]
}

func reverse(levels []polymarket.OrderSummary) []polymarket.OrderSummary {
	out := make([]polymarket.OrderSummary, len(levels))
	for i, l := range levels {
		out[len(levels)-1-i] = l
	}
	return out
}

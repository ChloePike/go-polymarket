// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command portfolio prints what one wallet holds on Polymarket.
//
// Positions are public: the data API will report any address to anyone, so
// this needs no wallet, no API key and no funds of your own. Point it at your
// own address, or at somebody whose trades you want to read.
//
//	go run ./examples/portfolio -user 0x1234...
//
// With no -user it reads the top trader from the leaderboard, so it runs with
// no arguments at all.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("portfolio: ")

	user := flag.String("user", "", "wallet address; empty reads the leaderboard's top trader")
	limit := flag.Int("limit", 15, "positions to show")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var c polymarket.Client

	address := *user
	if address == "" {
		var err error
		if address, err = topTrader(ctx, &c); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "no -user given; reading %s\n", address)
	}

	positions, err := c.Positions(ctx, polymarket.PositionsParams{
		User:  address,
		Limit: *limit,
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(positions) == 0 {
		fmt.Printf("%s holds nothing\n", address)
		return
	}

	// Biggest position first: that is the one that decides the day.
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].CurrentValue > positions[j].CurrentValue
	})

	var value, pnl float64
	for _, p := range positions {
		value += p.CurrentValue
		pnl += p.CashPnl
	}

	fmt.Printf("%s\n", address)
	fmt.Printf("%d positions, %.2f USDC at market, %+.2f unrealised\n\n", len(positions), value, pnl)
	fmt.Printf("%-10s %10s %8s %10s %10s\n", "OUTCOME", "SHARES", "AVG", "VALUE", "PNL")
	for _, p := range positions {
		fmt.Printf("%-10s %10.2f %8.3f %10.2f %+10.2f   %s\n",
			truncate(p.Outcome, 10), p.Size, p.AvgPrice, p.CurrentValue, p.CashPnl,
			truncate(p.Title, 56))
	}
}

// topTrader picks an address with real positions so the command runs bare.
func topTrader(ctx context.Context, c *polymarket.Client) (string, error) {
	board, err := c.Leaderboard(ctx, polymarket.LeaderboardParams{Limit: 1})
	if err != nil {
		return "", err
	}
	if len(board) == 0 {
		return "", fmt.Errorf("leaderboard is empty")
	}
	return board[0].ProxyWallet, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

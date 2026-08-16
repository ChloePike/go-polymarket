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
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"sort"
	"time"

	"github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/data"
)

// A holding pairs a position with its market value and profit parsed as
// exact rationals. The data API sends both as decimal text, and json.Number
// has string kind: comparing or adding two of them directly is text work, so
// "9.5" sorts above "10000.0". Parsing once keeps the order and the totals
// arithmetic.
type holding struct {
	pos   data.Position
	value *big.Rat
	pnl   *big.Rat
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	user := flag.String("user", "", "wallet address; empty reads the leaderboard's top trader")
	limit := flag.Int("limit", 15, "positions to show")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := data.New()

	address := *user
	if address == "" {
		var err error
		if address, err = topTrader(ctx, c); err != nil {
			slog.Error("reading the leaderboard", "err", err)
			os.Exit(1)
		}
		slog.Info("no -user given; reading the leaderboard's top trader", "address", address)
	}

	positions, err := c.Positions(ctx, data.PositionsParams{
		User:  address,
		Limit: *limit,
	})
	if err != nil {
		slog.Error("fetching positions", "user", address, "err", err)
		os.Exit(1)
	}
	if len(positions) == 0 {
		fmt.Printf("%s holds nothing\n", address)
		return
	}

	holdings := make([]holding, len(positions))
	for i, p := range positions {
		holdings[i] = holding{pos: p, value: rat(p.CurrentValue), pnl: rat(p.CashPnl)}
	}

	// Biggest position first: that is the one that decides the day.
	sort.Slice(holdings, func(i, j int) bool {
		return holdings[i].value.Cmp(holdings[j].value) > 0
	})

	value, pnl := new(big.Rat), new(big.Rat)
	for _, h := range holdings {
		value.Add(value, h.value)
		pnl.Add(pnl, h.pnl)
	}

	fmt.Printf("%s\n", address)
	fmt.Printf("%d positions, %s USDC at market, %s unrealised\n\n",
		len(holdings), value.FloatString(2), signed(pnl, 2))
	fmt.Printf("%-10s %10s %8s %10s %10s\n", "OUTCOME", "SHARES", "AVG", "VALUE", "PNL")
	for _, h := range holdings {
		fmt.Printf("%-10s %10s %8s %10s %10s   %s\n",
			truncate(h.pos.Outcome, 10), rat(h.pos.Size).FloatString(2),
			rat(h.pos.AvgPrice).FloatString(3), h.value.FloatString(2),
			signed(h.pnl, 2), truncate(h.pos.Title, 56))
	}
}

// rat parses an amount the data API sent. A null or absent number decodes to
// the empty json.Number, which big.Rat rejects; that reads as "not reported"
// and counts as zero in a total.
func rat(n json.Number) *big.Rat {
	r, err := polymarket.ParseAmount(string(n))
	if err != nil {
		return new(big.Rat)
	}
	return r
}

// signed renders an amount with an explicit sign, which FloatString supplies
// only for negatives.
func signed(r *big.Rat, prec int) string {
	if r.Sign() >= 0 {
		return "+" + r.FloatString(prec)
	}
	return r.FloatString(prec)
}

// topTrader picks an address with real positions so the command runs bare.
func topTrader(ctx context.Context, c *data.Client) (string, error) {
	board, err := c.Leaderboard(ctx, data.LeaderboardParams{Limit: 1})
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

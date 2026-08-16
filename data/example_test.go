// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package data_test

import (
	"context"
	"fmt"
	"log/slog"

	polymarket "github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/data"
)

// Positions are public. The data API reports any wallet to anyone, so nothing
// here needs a key — point it at your own address or at someone whose trades
// you want to read.
func Example() {
	c := data.New()

	positions, err := c.Positions(context.Background(), data.PositionsParams{
		User:  "0x3dfb153c197d4c19d3b31c1ecd2c7b6860eeabaf",
		Limit: 5,
	})
	if err != nil {
		slog.Error("fetching positions", "err", err)
		return
	}
	for _, p := range positions {
		fmt.Printf("%.2f shares of %s at %.3f, worth %.2f\n",
			p.Size, p.Outcome, p.AvgPrice, p.CurrentValue)
	}
}

// SizeThreshold is a decimal string rather than a float64 on purpose: the
// server defaults to "1.0" when the parameter is absent, so "0" — include the
// dust — has to stay distinguishable from "unset", and a float64 cannot tell
// those two apart.
func ExampleClient_Positions() {
	c := data.New()

	positions, err := c.Positions(context.Background(), data.PositionsParams{
		User:          "0x3dfb153c197d4c19d3b31c1ecd2c7b6860eeabaf",
		SizeThreshold: "0",
		Redeemable:    true,
	})
	if err != nil {
		slog.Error("fetching positions", "err", err)
		return
	}
	fmt.Println(len(positions), "redeemable positions, dust included")
}

// Holders answers the other direction: who owns a market rather than what a
// wallet owns.
func ExampleClient_Holders() {
	c := data.New()

	holders, err := c.Holders(context.Background(), data.HoldersParams{
		Market: []string{"0x699d6ea54f239dedf8ab3820504debeea93854f1f126c8e8d5c6c9d500cd25fa"},
		Limit:  5,
	})
	if err != nil {
		slog.Error("fetching holders", "err", err)
		return
	}
	for _, token := range holders {
		fmt.Printf("token %s has %d holders\n", token.Token, len(token.Holders))
	}
}

// One session can serve several clients, so a shared http.Client, user agent
// and retry policy are configured once.
func ExampleNewWithSession() {
	s := polymarket.NewSession(polymarket.DataHost,
		polymarket.WithUserAgent("my-dashboard/1.0"),
	)
	c := data.NewWithSession(s)

	traded, err := c.TradedCount(context.Background(),
		"0x3dfb153c197d4c19d3b31c1ecd2c7b6860eeabaf")
	if err != nil {
		slog.Error("fetching traded count", "err", err)
		return
	}
	fmt.Println("markets traded:", traded.Traded)
}

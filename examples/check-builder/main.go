// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command check-builder queries a builder code's fee rates and attributed
// trades. Read-only: no wallet, no API key. Works today.
//
//	go run ./examples/check-builder 0x11adfa13...ae5417
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ChloePike/go-polymarket/client"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: check-builder <builder_code>")
		os.Exit(2)
	}
	code := os.Args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := client.New()

	rates, err := c.GetBuilderFeeRates(ctx, code)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fee rates:", err)
		os.Exit(1)
	}
	fmt.Printf("builder code : %s\n", rates.Code)
	fmt.Printf("enabled      : %v\n", rates.Enabled)
	fmt.Printf("maker / taker: %d / %d bps\n", rates.BuilderMakerFeeRateBps, rates.BuilderTakerFeeRateBps)
	if rates.BuilderMakerFeeRateBps == 0 && rates.BuilderTakerFeeRateBps == 0 {
		fmt.Println("⚠️  both rates are 0 — orders will be attributed but earn nothing.")
	}

	trades, err := c.GetBuilderTrades(ctx, code, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "builder trades:", err)
		os.Exit(1)
	}
	fmt.Printf("attributed trades: %d\n", trades.Count)
}

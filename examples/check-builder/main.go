// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command check-builder looks up a builder code's fee rates and counts the
// trades attributed to it. Both endpoints it calls are public, so it needs
// neither a wallet nor an API key: it only reads.
//
// Usage:
//
//	go run ./examples/check-builder <builder-code>
//
// Example:
//
//	go run ./examples/check-builder 0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417
package main

import (
	"context"
	"fmt"
	"os"

	polymarket "github.com/ChloePike/go-polymarket"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-builder <builder-code>")
		os.Exit(2)
	}
	code := os.Args[1]
	ctx := context.Background()
	var c polymarket.Client

	fees, err := c.BuilderFees(ctx, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "builder fees: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("builder   %s\n", fees.Code)
	fmt.Printf("enabled   %v\n", fees.Enabled)
	fmt.Printf("maker fee %d bps (%s)\n", fees.MakerFeeRateBps, formatBps(fees.MakerFeeRateBps))
	fmt.Printf("taker fee %d bps (%s)\n", fees.TakerFeeRateBps, formatBps(fees.TakerFeeRateBps))

	count, err := countBuilderTrades(ctx, &c, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "builder trades: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("trades    %d\n", count)
}

// countBuilderTrades walks every page of GET /builder/trades for code and
// sums the trades each page carries. The response's own Count field is not
// trusted as a running total: nothing observed so far confirms whether it
// means "this page" or "overall", so counting by hand is the only way to be
// sure. A cursor that fails to advance stops the loop rather than spinning.
func countBuilderTrades(ctx context.Context, c *polymarket.Client, code string) (int, error) {
	var total int
	cursor := ""
	for {
		trades, page, err := c.BuilderTrades(ctx, code, polymarket.BuilderTradeParams{NextCursor: cursor})
		if err != nil {
			return total, err
		}
		total += len(trades)
		if page.NextCursor == polymarket.CursorEnd || page.NextCursor == cursor {
			return total, nil
		}
		cursor = page.NextCursor
	}
}

// formatBps renders a basis-point rate as a percentage using integer
// arithmetic only: BuilderFeeBps is 10000, so bps/100 is the whole percent
// and bps%100 is its two-digit fraction.
func formatBps(bps int) string {
	return fmt.Sprintf("%d.%02d%%", bps/100, bps%100)
}

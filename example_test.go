// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A token id identifies one outcome of one market. Every read below takes one.
const tokenID = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

// Reading the market needs no credentials at all, so the zero Client is ready
// to use.
func Example() {
	var c polymarket.Client

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	book, err := c.OrderBook(ctx, tokenID)
	if err != nil {
		log.Fatal(err)
	}
	if len(book.Bids) > 0 && len(book.Asks) > 0 {
		best := book.Bids[len(book.Bids)-1]
		fmt.Printf("best bid %s for %s shares\n", best.Price, best.Size)
	}
}

// A price on Polymarket is a probability: 0.52 means the market puts the
// outcome at 52 percent.
func ExampleClient_Midpoint() {
	var c polymarket.Client

	mid, err := c.Midpoint(context.Background(), tokenID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("the market prices this outcome at %s\n", mid)
}

// The plural price endpoints answer with a map keyed by token id rather than a
// list, so one request covers a whole watchlist.
func ExampleClient_Prices() {
	var c polymarket.Client

	prices, err := c.Prices(context.Background(), []polymarket.BookParams{
		{TokenID: tokenID, Side: polymarket.Buy},
	})
	if err != nil {
		log.Fatal(err)
	}
	for token, bySide := range prices {
		for side, price := range bySide {
			fmt.Printf("%s %s %s\n", token, side, price)
		}
	}
}

// Pages walks a cursor-paginated endpoint to its end, so a caller never has to
// handle the cursor sentinels.
func ExamplePages() {
	var c polymarket.Client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	active := 0
	for market, err := range polymarket.Pages(ctx, c.Markets) {
		if err != nil {
			log.Fatal(err)
		}
		if market.Active {
			active++
		}
	}
	fmt.Printf("%d active markets\n", active)
}

// A private key never leaves the process: the client signs digests locally and
// sends signatures, never the key.
func ExampleNewPrivateKey() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("trading as", key.Address())
}

// Building an order is separate from sending one. BuildOrder resolves a price
// and a size into the integer amounts the exchange verifies, and SignOrder
// authorises them; nothing has been submitted yet at the end of this function.
func ExampleBuildOrder() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// TickSize and NegRisk describe the market and must match it. Client.
	// CreateOrder fetches both for you; this shows the layer underneath.
	opts := polymarket.OrderOptions{TickSize: "0.01", NegRisk: false}

	order, err := polymarket.BuildOrder(polymarket.UserOrder{
		TokenID: tokenID,
		Price:   "0.52",
		Size:    "100",
		Side:    polymarket.Buy,
	}, key.Address(), opts)
	if err != nil {
		log.Fatal(err)
	}

	// A buy pays USDC and receives shares, so the maker amount is the cost:
	// 100 shares at 0.52 is 52 USDC, carried as 52000000 at six decimals.
	fmt.Println(order.MakerAmount, order.TakerAmount)

	signed, err := polymarket.SignOrder(order, polymarket.ChainPolygon, opts, key)
	if err != nil {
		log.Fatal(err)
	}
	_ = signed
}

// A market order is sized by what you are willing to spend rather than by a
// share count, so a buy names an amount of USDC.
func ExampleBuildMarketOrder() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	order, err := polymarket.BuildMarketOrder(polymarket.MarketOrder{
		TokenID: tokenID,
		Amount:  "100", // USDC to spend
		Price:   "0.52",
		Side:    polymarket.Buy,
	}, key.Address(), polymarket.OrderOptions{TickSize: "0.01"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(order.MakerAmount, order.TakerAmount)
}

// Fees are estimated before the trade so the caller can size an order that
// actually fits the balance.
func ExampleAdjustBuyAmountForFees() {
	got, err := polymarket.AdjustBuyAmountForFees(polymarket.FeeParams{
		Amount:      100,
		Price:       0.52,
		USDCBalance: 100,
		FeeRate:     0.02,
		FeeExponent: 1,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("spend %.4f rather than 100, leaving %.4f for the fee\n",
		got.Amount, got.PlatformFee)
	// Output: spend 99.0400 rather than 100, leaving 0.9600 for the fee
}

// Positions come from the data API, which needs no credentials: anyone can
// read any wallet's book.
func ExampleClient_Positions() {
	var c polymarket.Client

	positions, err := c.Positions(context.Background(), polymarket.PositionsParams{
		User:  "0x0000000000000000000000000000000000000000",
		Limit: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range positions {
		fmt.Printf("%s: %.2f shares worth %.2f USDC\n", p.ConditionID, p.Size, p.CurrentValue)
	}
}

// An API error carries the status code, so a rate limit is distinguishable
// from a bad request without parsing text.
func ExampleError() {
	var c polymarket.Client

	_, err := c.OrderBook(context.Background(), "not-a-token-id")
	var apiErr *polymarket.Error
	if errors.As(err, &apiErr) {
		fmt.Println("status", apiErr.StatusCode)
	}
}

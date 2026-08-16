// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	polymarket "github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/clob"
)

// A token id identifies one outcome of one market. Prices are probabilities:
// 0.52 means the market puts the outcome at 52 percent.
const tokenID = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

// Reading the book needs no credentials at all.
func Example() {
	c := clob.New()

	book, err := c.OrderBook(context.Background(), tokenID)
	if err != nil {
		slog.Error("fetching the order book", "err", err)
		return
	}
	// Both sides arrive worst-price-first, so the top of book is last.
	if len(book.Bids) > 0 {
		best := book.Bids[len(book.Bids)-1]
		fmt.Printf("best bid %s for %s shares\n", best.Price, best.Size)
	}
}

// The plural price endpoints answer with a map keyed by token id rather than
// a list, so one round trip covers a whole watchlist. This is not what the
// official types claim; it is what production actually sends.
func ExampleClient_Prices() {
	c := clob.New()

	prices, err := c.Prices(context.Background(), []clob.BookParams{
		{TokenID: tokenID, Side: polymarket.Buy},
	})
	if err != nil {
		slog.Error("fetching prices", "err", err)
		return
	}
	for token, bySide := range prices {
		for side, price := range bySide {
			fmt.Printf("%s %s %s\n", token, side, price)
		}
	}
}

// Pages walks a cursor-paginated endpoint to its end, so a caller never has to
// know about the base64 cursor sentinels.
func ExamplePages() {
	c := clob.New()

	active := 0
	for market, err := range clob.Pages(context.Background(), c.SamplingMarkets) {
		if err != nil {
			slog.Error("walking markets", "err", err)
			return
		}
		if market.Active {
			active++
		}
	}
	fmt.Println(active, "active markets with rewards")
}

// Trading takes two steps. Level 1 proves control of the wallet once and
// returns credentials; every later request is signed with those, and the
// private key is not touched again.
func ExampleClient_CreateOrDeriveAPIKey() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}
	c := clob.New(clob.WithSigner(key))

	creds, err := c.CreateOrDeriveAPIKey(context.Background())
	if err != nil {
		slog.Error("level-1 handshake", "err", err)
		return
	}
	fmt.Println("api key", creds.Key)
}

// CreateOrder looks up the market's tick size and neg-risk flag, sizes the
// order and signs it. Nothing is submitted until PostOrder.
//
// Both lookups matter: the tick size decides the rounding, and the neg-risk
// flag decides which exchange contract the signature is bound to. A wrong
// value produces a well-formed signature the exchange refuses.
func ExampleClient_CreateOrder() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}
	c := clob.New(clob.WithSigner(key))

	ctx := context.Background()
	if _, err := c.CreateOrDeriveAPIKey(ctx); err != nil {
		slog.Error("level-1 handshake", "err", err)
		return
	}

	order, err := c.CreateOrder(ctx, polymarket.UserOrder{
		TokenID: tokenID,
		Price:   "0.52",
		Size:    "100",
		Side:    polymarket.Buy,
	}, polymarket.OrderOptions{})
	if err != nil {
		slog.Error("building the order", "err", err)
		return
	}

	// This is the line that spends money.
	resp, err := c.PostOrder(ctx, order, polymarket.GTC, clob.SubmitOptions{})
	if err != nil {
		slog.Error("submitting the order", "err", err)
		return
	}
	fmt.Println(resp.OrderID, resp.Status)
}

// A market order is sized by what you are willing to spend rather than by a
// share count. Leaving Price empty asks the book what the order would fill at.
func ExampleClient_CreateMarketOrder() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}
	c := clob.New(clob.WithSigner(key))

	ctx := context.Background()
	order, err := c.CreateMarketOrder(ctx, polymarket.MarketOrder{
		TokenID: tokenID,
		Amount:  "25", // USDC to spend
		Side:    polymarket.Buy,
	}, polymarket.OrderOptions{})
	if err != nil {
		slog.Error("building the order", "err", err)
		return
	}

	// FOK fills completely or not at all; FAK takes what is there now.
	resp, err := c.PostOrder(ctx, order, polymarket.FOK, clob.SubmitOptions{})
	if err != nil {
		slog.Error("submitting the order", "err", err)
		return
	}
	fmt.Println(resp.Status)
}

// Fees are estimated before the trade, so an order can be sized to fit the
// balance rather than bounce off it. FeeCurve reads the market's curve; the
// arithmetic is local.
func ExampleAdjustBuyAmountForFees() {
	c := clob.New()

	curve, err := c.FeeCurve(context.Background(), tokenID)
	if err != nil {
		slog.Error("fetching the fee curve", "err", err)
		return
	}
	got, err := clob.AdjustBuyAmountForFees(clob.FeeParams{
		Amount:      100,
		Price:       0.52,
		USDCBalance: 100,
		FeeRate:     curve.Rate,
		FeeExponent: curve.Exponent,
	})
	if err != nil {
		slog.Error("estimating fees", "err", err)
		return
	}
	fmt.Printf("spend %.4f, leaving %.4f for the fee\n", got.Amount, got.PlatformFee)
}

// An API error carries the status code, so a rate limit is distinguishable
// from a bad request without matching on text.
func ExampleClient_OrderBook_error() {
	c := clob.New()

	_, err := c.OrderBook(context.Background(), "not-a-token-id")
	var apiErr *polymarket.Error
	if errors.As(err, &apiErr) {
		fmt.Println("status", apiErr.StatusCode)
	}
}

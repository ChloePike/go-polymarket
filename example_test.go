// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket_test

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A token id identifies one outcome of one market.
const tokenID = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

// The root package holds what every Polymarket client shares: the wallet, the
// order types, the signing, and the session the client packages are built on.
// Endpoints live in the clob, gamma, data and ws packages.
func Example() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}

	// One session can serve several client packages, so a wallet and an
	// http.Client are configured once:
	//
	//	s := polymarket.NewSession(polymarket.DefaultHost, polymarket.WithSigner(key))
	//	c := clob.NewWithSession(s)
	fmt.Println("trading as", key.Address())
}

// A private key never leaves the process. The client signs digests locally and
// sends signatures, never the key itself.
func ExampleNewPrivateKey() {
	key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}
	fmt.Println(key.Address())
}

// Building an order is separate from sending one. BuildOrder turns a price and
// a size into the integer amounts the exchange verifies, and SignOrder
// authorises them. Nothing has been submitted when this returns; clob.Client's
// PostOrder does that.
func ExampleBuildOrder() {
	// The well-known Hardhat development key: public, empty, and used here
	// only so the example produces the same bytes every time.
	key, err := polymarket.NewPrivateKey(
		"0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		slog.Error("reading the key", "err", err)
		return
	}

	// TickSize and NegRisk describe the market and must match it; clob's
	// CreateOrder fetches both for you. Salt and Timestamp are fixed here
	// only to make the output reproducible.
	opts := polymarket.OrderOptions{
		TickSize:  "0.01",
		NegRisk:   false,
		Salt:      479249096354,
		Timestamp: 1740000000000,
	}

	order, err := polymarket.BuildOrder(polymarket.UserOrder{
		TokenID: tokenID,
		Price:   "0.52",
		Size:    "100",
		Side:    polymarket.Buy,
	}, key.Address(), opts)
	if err != nil {
		slog.Error("building the order", "err", err)
		return
	}

	// A buy pays USDC and receives shares, so the maker amount is the cost:
	// 100 shares at 0.52 is 52 USDC, carried as 52000000 at six decimals.
	fmt.Println("maker", order.MakerAmount, "taker", order.TakerAmount)

	signed, err := polymarket.SignOrder(order, polymarket.ChainPolygon, opts, key)
	if err != nil {
		slog.Error("signing the order", "err", err)
		return
	}
	fmt.Println("signature", signed.Signature[:18]+"…")
	// Output:
	// maker 52000000 taker 100000000
	// signature 0xa79802f2f11608f9…
}

// A market order is sized by what you are willing to spend rather than by a
// share count, so a buy names an amount of USDC and the share count follows
// from the price.
func ExampleBuildMarketOrder() {
	order, err := polymarket.BuildMarketOrder(polymarket.MarketOrder{
		TokenID: tokenID,
		Amount:  "100", // USDC to spend
		Price:   "0.52",
		Side:    polymarket.Buy,
	}, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		polymarket.OrderOptions{TickSize: "0.01", Salt: 1, Timestamp: 1})
	if err != nil {
		slog.Error("building the order", "err", err)
		return
	}
	// 100 USDC at 0.52 buys 192.3076 shares once the amount is converged to
	// the four decimals this tick size allows.
	fmt.Println("maker", order.MakerAmount, "taker", order.TakerAmount)
	// Output: maker 100000000 taker 192307600
}

// The digest is what a signature actually covers. It is exported so a wallet
// that signs elsewhere — a hardware device, a remote service — can be handed
// exactly the 32 bytes to sign.
func ExampleOrderDigest() {
	order, err := polymarket.BuildOrder(polymarket.UserOrder{
		TokenID: tokenID,
		Price:   "0.52",
		Size:    "100",
		Side:    polymarket.Buy,
	}, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		polymarket.OrderOptions{TickSize: "0.01", Salt: 479249096354, Timestamp: 1740000000000})
	if err != nil {
		slog.Error("building the order", "err", err)
		return
	}
	digest, err := polymarket.OrderDigest(order, polymarket.ChainPolygon,
		polymarket.OrderOptions{TickSize: "0.01"})
	if err != nil {
		slog.Error("hashing the order", "err", err)
		return
	}
	fmt.Println(hex.EncodeToString(digest[:]))
	// Output: 4278b3fc4e6ce6bd2e2e2ce3361fe010d49f64144d47512361bfa4fc3ad92399
}

// Addresses are rendered in EIP-55 mixed case, which is a checksum: a typo in
// an address almost always breaks the capitalisation pattern.
func ExampleChecksumAddress() {
	fmt.Println(polymarket.ChecksumAddress("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"))
	// Output: 0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed
}

// A Session is the shared foundation: one wallet, one http.Client, one retry
// policy, handed to as many client packages as you need.
func ExampleNewSession() {
	// Add polymarket.WithSigner(key) to trade; reads need no wallet.
	s := polymarket.NewSession(polymarket.DefaultHost,
		polymarket.WithUserAgent("my-trading-bot/1.0"),
	)
	fmt.Println(s.Host(), s.ChainID())
	// Output: https://clob.polymarket.com 137
}

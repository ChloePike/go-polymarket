// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"strconv"
	"testing"
)

// fieldCase compares one order field against the golden value.
type fieldCase struct {
	name string
	got  string
	want string
}

// domainCase is one exchange version and neg-risk flag with the verifying
// contract it must resolve to.
type domainCase struct {
	version int
	negRisk bool
	want    string
}

// rejectCase is one order BuildOrder must refuse to build.
type rejectCase struct {
	name string
	u    UserOrder
	opts OrderOptions
}

// TestBuildAndSignOrder walks the whole path a caller takes — a price, a size
// and a side in, a signed order out — and compares every field against orders
// the official SDK produced from the same inputs.
func TestBuildAndSignOrder(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range g.Orders {
		t.Run(want.Name, func(t *testing.T) {
			salt, err := strconv.ParseInt(want.Order.Salt, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ts, err := strconv.ParseInt(want.Order.Timestamp, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			expiration, err := strconv.ParseInt(want.Order.Expiration, 10, 64)
			if err != nil {
				t.Fatal(err)
			}

			opts := OrderOptions{
				TickSize:  want.Input.TickSize,
				NegRisk:   want.Input.NegRisk,
				Version:   want.Input.Version,
				Salt:      salt,
				Timestamp: ts,
			}
			got, err := BuildOrder(UserOrder{
				TokenID:     want.Input.TokenID,
				Price:       want.Input.Price,
				Size:        want.Input.Size,
				Side:        Side(want.Input.Side),
				BuilderCode: want.Input.BuilderCode,
				Expiration:  expiration,
			}, key.Address(), opts)
			if err != nil {
				t.Fatal(err)
			}

			fields := []fieldCase{
				{"salt", got.Salt, want.Order.Salt},
				{"maker", got.Maker, want.Order.Maker},
				{"signer", got.Signer, want.Order.Signer},
				{"tokenId", got.TokenID, want.Order.TokenID},
				{"makerAmount", got.MakerAmount, want.Order.MakerAmount},
				{"takerAmount", got.TakerAmount, want.Order.TakerAmount},
				{"side", string(got.Side), want.Order.Side},
				{"timestamp", got.Timestamp, want.Order.Timestamp},
				{"expiration", got.Expiration, want.Order.Expiration},
				{"metadata", got.Metadata, want.Order.Metadata},
				{"builder", got.Builder, want.Order.Builder},
			}
			for _, f := range fields {
				if f.got != f.want {
					t.Errorf("%s = %s, want %s", f.name, f.got, f.want)
				}
			}
			if uint8(got.SignatureType) != want.Order.SignatureType {
				t.Errorf("signatureType = %d, want %d", got.SignatureType, want.Order.SignatureType)
			}

			digest, err := OrderDigest(got, g.ChainID, opts)
			if err != nil {
				t.Fatal(err)
			}
			if h := "0x" + hex.EncodeToString(digest[:]); h != want.Digest {
				t.Fatalf("digest = %s, want %s", h, want.Digest)
			}

			signed, err := SignOrder(got, g.ChainID, opts, key)
			if err != nil {
				t.Fatal(err)
			}
			if signed.Signature != want.Signature {
				t.Fatalf("signature = %s, want %s", signed.Signature, want.Signature)
			}
		})
	}
}

// TestOrderDomains pins the verifying contract each version and neg-risk flag
// resolves to. Signing against the wrong contract is invisible locally: the
// signature is well formed and the exchange simply refuses it.
func TestOrderDomains(t *testing.T) {
	g := loadGolden(t)
	cases := []domainCase{
		{V1, false, g.Contracts.Exchange},
		{V1, true, g.Contracts.NegRiskExchange},
		{V2, false, g.Contracts.ExchangeV2},
		{V2, true, g.Contracts.NegRiskExchangeV2},
		{V3, false, g.Contracts.ExchangeV3},
		{V3, true, g.Contracts.ExchangeV3}, // V3 has a single exchange
	}
	for _, tc := range cases {
		d, err := orderDomain(g.ChainID, tc.version, tc.negRisk)
		if err != nil {
			t.Fatalf("v%d negRisk=%v: %v", tc.version, tc.negRisk, err)
		}
		if d.VerifyingContract != tc.want {
			t.Errorf("v%d negRisk=%v contract = %s, want %s",
				tc.version, tc.negRisk, d.VerifyingContract, tc.want)
		}
		if d.Name != "Polymarket CTF Exchange" {
			t.Errorf("v%d domain name = %q", tc.version, d.Name)
		}
	}
	if _, err := orderDomain(g.ChainID, 4, false); err == nil {
		t.Error("unsupported exchange version: got nil error")
	}
	if _, err := orderDomain(999, V2, false); err == nil {
		t.Error("unknown chain: got nil error")
	}
}

// TestRandomSaltFitsJSONNumber guards the reason the salt is bounded: the wire
// body carries it as a JSON number, and a parser reading it as a float64 must
// recover the exact value that was signed.
func TestRandomSaltFitsJSONNumber(t *testing.T) {
	const maxExact = int64(1) << 53
	for range 2000 {
		s, err := randomSalt()
		if err != nil {
			t.Fatal(err)
		}
		if s <= 0 {
			t.Fatalf("salt = %d, want a positive value", s)
		}
		if s >= maxExact {
			t.Fatalf("salt = %d, want below 2^53 so a float64 round-trip is exact", s)
		}
		if int64(float64(s)) != s {
			t.Fatalf("salt %d does not survive a float64 round trip", s)
		}
	}
}

func TestBuildOrderRejects(t *testing.T) {
	const token = "71321045679252212594626385532706912750332728571942532289631379312455583992563"
	addr := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

	cases := []rejectCase{
		{"unknown tick size",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.02"}},
		{"price below tick",
			UserOrder{TokenID: token, Price: "0.005", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		{"price above one minus tick",
			UserOrder{TokenID: token, Price: "0.995", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		{"non-numeric price",
			UserOrder{TokenID: token, Price: "cheap", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		{"non-numeric size",
			UserOrder{TokenID: token, Price: "0.5", Size: "lots", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		{"empty token id",
			UserOrder{Price: "0.5", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		{"non-numeric token id",
			UserOrder{TokenID: "0xdeadbeef", Price: "0.5", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01"}},
		// The pass-through fields are checked here rather than at digest
		// time, so a mistyped builder code is reported while the caller
		// still has the string they typed.
		{"short builder code",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy, BuilderCode: "0xdeadbeef"},
			OrderOptions{TickSize: "0.01"}},
		{"builder code is not hex",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy,
				BuilderCode: "0xzzadfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417"},
			OrderOptions{TickSize: "0.01"}},
		{"short metadata",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy, Metadata: "0x01"},
			OrderOptions{TickSize: "0.01"}},
		{"malformed taker",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy, Taker: "0xnope"},
			OrderOptions{TickSize: "0.01"}},
		{"malformed funder",
			UserOrder{TokenID: token, Price: "0.5", Size: "1", Side: Buy},
			OrderOptions{TickSize: "0.01", Funder: "not-an-address"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildOrder(tc.u, addr, tc.opts); err == nil {
				t.Fatal("got nil error")
			}
		})
	}
}

// TestBuildMarketOrder checks the market-order path end to end against the
// golden market amounts, including that a buy is sized in USDC.
func TestBuildMarketOrder(t *testing.T) {
	addr := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	const token = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

	// BUY 100 USDC at 0.52, tick 0.01: maker is the 100 USDC spent and taker
	// is 100/0.52 = 192.307692... shares, nudged up at eight decimals and then
	// truncated to the four the tick size allows. The golden vectors carry the
	// same pair, so this case also documents what converge does.
	o, err := BuildMarketOrder(MarketOrder{
		TokenID: token, Amount: "100", Price: "0.52", Side: Buy,
	}, addr, OrderOptions{TickSize: "0.01", Salt: 1, Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	if o.MakerAmount != "100000000" {
		t.Errorf("market buy makerAmount = %s, want 100000000", o.MakerAmount)
	}
	if o.TakerAmount != "192307600" {
		t.Errorf("market buy takerAmount = %s, want 192307600", o.TakerAmount)
	}

	// A price that rounds down to zero has no share count.
	if _, err := BuildMarketOrder(MarketOrder{
		TokenID: token, Amount: "100", Price: "0.04", Side: Buy,
	}, addr, OrderOptions{TickSize: "0.1"}); err == nil {
		t.Error("market buy at a price that rounds to zero: got nil error")
	}
}

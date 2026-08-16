// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package amount

import (
	"encoding/json"
	"os"
	"testing"
)

type goldenAmounts struct {
	Amounts []struct {
		Side        string `json:"side"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		TickSize    string `json:"tickSize"`
		MakerAmount string `json:"makerAmount"`
		TakerAmount string `json:"takerAmount"`
	} `json:"amounts"`
	MarketOrders []struct {
		Side        string `json:"side"`
		Amount      string `json:"amount"`
		Price       string `json:"price"`
		TickSize    string `json:"tickSize"`
		MakerAmount string `json:"makerAmount"`
		TakerAmount string `json:"takerAmount"`
	} `json:"marketOrders"`
	RoundingConfig map[string]struct {
		Price  int `json:"price"`
		Size   int `json:"size"`
		Amount int `json:"amount"`
	} `json:"roundingConfig"`
}

func load(t *testing.T) goldenAmounts {
	t.Helper()
	b, err := os.ReadFile("../../testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var g goldenAmounts
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return g
}

// TestByTickSize checks the rounding table against the one the official SDK
// ships, so a change on their side shows up here rather than in a wrong order.
func TestByTickSize(t *testing.T) {
	g := load(t)
	if len(g.RoundingConfig) != len(ByTickSize) {
		t.Fatalf("have %d tick sizes, golden has %d", len(ByTickSize), len(g.RoundingConfig))
	}
	for tick, want := range g.RoundingConfig {
		got, ok := ByTickSize[tick]
		if !ok {
			t.Errorf("tick size %q missing", tick)
			continue
		}
		if got.Price != want.Price || got.Size != want.Size || got.Amount != want.Amount {
			t.Errorf("tick %s = %+v, want price=%d size=%d amount=%d",
				tick, got, want.Price, want.Size, want.Amount)
		}
	}
}

// TestLimit walks the full golden grid: every tick size against every price and
// size, both sides. This is the amount math's whole correctness story.
func TestLimit(t *testing.T) {
	g := load(t)
	if len(g.Amounts) == 0 {
		t.Fatal("golden grid is empty")
	}
	for _, c := range g.Amounts {
		cfg, ok := ByTickSize[c.TickSize]
		if !ok {
			t.Fatalf("unknown tick size %q", c.TickSize)
		}
		price, err := ParseDecimal(c.Price)
		if err != nil {
			t.Fatal(err)
		}
		size, err := ParseDecimal(c.Size)
		if err != nil {
			t.Fatal(err)
		}
		raw := Limit(c.Side == "BUY", size, price, cfg)
		maker, taker := Fixed(raw.Maker), Fixed(raw.Taker)
		if maker != c.MakerAmount || taker != c.TakerAmount {
			t.Errorf("%s tick=%s size=%s price=%s: got maker=%s taker=%s, want maker=%s taker=%s",
				c.Side, c.TickSize, c.Size, c.Price, maker, taker, c.MakerAmount, c.TakerAmount)
		}
	}
	t.Logf("verified %d golden limit-order amounts", len(g.Amounts))
}

func TestMarket(t *testing.T) {
	g := load(t)
	if len(g.MarketOrders) == 0 {
		t.Fatal("golden market orders are empty")
	}
	for _, c := range g.MarketOrders {
		cfg := ByTickSize[c.TickSize]
		price, err := ParseDecimal(c.Price)
		if err != nil {
			t.Fatal(err)
		}
		amt, err := ParseDecimal(c.Amount)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := Market(c.Side == "BUY", amt, price, cfg)
		if err != nil {
			t.Fatalf("%s tick=%s amount=%s price=%s: %v", c.Side, c.TickSize, c.Amount, c.Price, err)
		}
		maker, taker := Fixed(raw.Maker), Fixed(raw.Taker)
		if maker != c.MakerAmount || taker != c.TakerAmount {
			t.Errorf("%s tick=%s amount=%s price=%s: got maker=%s taker=%s, want maker=%s taker=%s",
				c.Side, c.TickSize, c.Amount, c.Price, maker, taker, c.MakerAmount, c.TakerAmount)
		}
	}
	t.Logf("verified %d golden market-order amounts", len(g.MarketOrders))
}

// TestMarketBuyAtZeroPrice pins the one input that has no answer: a price that
// rounds to zero would divide by zero rather than produce a huge order.
func TestMarketBuyAtZeroPrice(t *testing.T) {
	price, err := ParseDecimal("0.04")
	if err != nil {
		t.Fatal(err)
	}
	amt, err := ParseDecimal("100")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Market(true, amt, price, ByTickSize["0.1"]); err == nil {
		t.Fatal("market buy at a price that rounds to zero: got nil error")
	}
}

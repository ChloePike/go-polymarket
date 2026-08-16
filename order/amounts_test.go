// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package order

import (
	"testing"

	"github.com/ChloePike/go-polymarket/types"
)

// TestGetRawAmounts pins the price/size -> maker/taker conversion against the
// official SDK's getOrderRawAmounts. Values below are hand-derived; replace or
// extend with vectors captured from @polymarket/clob-client-v2 (see DESIGN.md
// §Golden tests).
func TestGetRawAmounts(t *testing.T) {
	cases := []struct {
		name              string
		side              types.Side
		size, price, tick string
		wantMaker         string // 6-dp integer string
		wantTaker         string
	}{
		{
			// BUY 5 shares @ 0.5, tick 0.01:
			// taker = 5 shares -> 5.000000 ; maker = 5*0.5 = 2.5 -> 2.500000
			name: "buy_5_at_0.5", side: types.SideBuy,
			size: "5", price: "0.5", tick: "0.01",
			wantMaker: "2500000", wantTaker: "5000000",
		},
		{
			// SELL 5 shares @ 0.5, tick 0.01:
			// maker = 5 shares ; taker = 2.5 USDC
			name: "sell_5_at_0.5", side: types.SideSell,
			size: "5", price: "0.5", tick: "0.01",
			wantMaker: "5000000", wantTaker: "2500000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := types.RoundingByTickSize[tc.tick]
			size, _ := ratFromDecimal(tc.size)
			price, _ := ratFromDecimal(tc.price)
			raw := GetRawAmounts(tc.side, size, price, cfg)
			maker, taker := FixedAmounts(raw)
			if maker != tc.wantMaker || taker != tc.wantTaker {
				t.Fatalf("got maker=%s taker=%s, want maker=%s taker=%s",
					maker, taker, tc.wantMaker, tc.wantTaker)
			}
		})
	}
}

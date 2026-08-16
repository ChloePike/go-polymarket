// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"math"
	"testing"
)

// feeCase is one FeeParams input paired with the buy amount the official
// SDK's adjustBuyAmountForFees computes for the same inputs (dist/fees, run
// live under node against @polymarket/clob-client-v2). wantReduced records
// whether the SDK's answer differs from the requested amount, so a bug that
// returns the right number for the wrong reason still shows up.
type feeCase struct {
	name        string
	p           FeeParams
	want        float64
	wantReduced bool
}

// feeCases are the cross-checked vectors for TestAdjustBuyAmountForFees. Each
// want value is adjustBuyAmountForFees's own output for the same seven
// inputs, taken from a node run against the installed package.
var feeCases = []feeCase{
	{
		name: "basic, ample balance, no slippage",
		p: FeeParams{Amount: 1000, Price: 0.5, USDCBalance: 5000,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0, FeeSlippage: 0},
		want:        1000,
		wantReduced: false,
	},
	{
		name: "slippage at its minimum of 1, tight balance",
		p: FeeParams{Amount: 1000, Price: 0.5, USDCBalance: 1004,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 1},
		want:        999.21249999999998,
		wantReduced: true,
	},
	{
		name: "slippage at its maximum of 100, tight balance",
		p: FeeParams{Amount: 1000, Price: 0.5, USDCBalance: 1010,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 100},
		want:        1000,
		wantReduced: false,
	},
	{
		name: "price near the low end of the range, tight balance",
		p: FeeParams{Amount: 1000, Price: 0.1, USDCBalance: 1030,
			FeeRate: 0.05, FeeExponent: 1, BuilderTakerFeeRate: 0.0005, FeeSlippage: 50},
		want:        962,
		wantReduced: true,
	},
	{
		name: "price near the high end of the range, tight balance",
		p: FeeParams{Amount: 1000, Price: 0.9, USDCBalance: 1030,
			FeeRate: 0.05, FeeExponent: 1, BuilderTakerFeeRate: 0.0005, FeeSlippage: 50},
		want:        1000,
		wantReduced: false,
	},
	{
		name: "balance below the requested amount",
		p: FeeParams{Amount: 500, Price: 0.5, USDCBalance: 50,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 10},
		want:        49.743750000000006,
		wantReduced: true,
	},
	{
		// Dividing by a Price of exactly 0 makes the platform fee NaN, and a
		// NaN comparison is never true, in Go as in JavaScript. The balance
		// check then never fires, so the request goes through unmodified —
		// a protocol quirk this test pins rather than papers over.
		name: "price at the 0 boundary",
		p: FeeParams{Amount: 1000, Price: 0, USDCBalance: 5000,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 10},
		want:        1000,
		wantReduced: false,
	},
	{
		// At Price 1, (price * (1 - price)) is 0, so the platform fee is 0
		// rather than NaN: 1 does not share 0's division-by-zero edge.
		name: "price at the 1 boundary",
		p: FeeParams{Amount: 1000, Price: 1, USDCBalance: 5000,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 10},
		want:        1000,
		wantReduced: false,
	},
	{
		name: "fee exponent of zero, tight balance",
		p: FeeParams{Amount: 1000, Price: 0.5, USDCBalance: 1030,
			FeeRate: 0.03, FeeExponent: 0, BuilderTakerFeeRate: 0.001, FeeSlippage: 0},
		want:        969,
		wantReduced: true,
	},
	{
		name: "balance equal to the requested amount",
		p: FeeParams{Amount: 1000, Price: 0.5, USDCBalance: 1000,
			FeeRate: 0.03, FeeExponent: 2, BuilderTakerFeeRate: 0.001, FeeSlippage: 0},
		want:        995.25,
		wantReduced: true,
	},
	{
		// USDCBalance covers the request plus its fees with nothing to
		// spare: the balance-cap branch fires (Reduced is true), but what
		// it computes — USDCBalance minus the fees — happens to equal the
		// requested amount again. Reduced names which branch ran, not
		// whether the number changed.
		name: "balance exactly at the amount-plus-fees boundary",
		p: FeeParams{Amount: 100, Price: 0.5, USDCBalance: 101,
			FeeRate: 0, FeeExponent: 2, BuilderTakerFeeRate: 0.01, FeeSlippage: 0},
		want:        100,
		wantReduced: true,
	},
}

// feeTolerance bounds how far a Go result may drift from the JavaScript one
// it is checked against. Every case here uses an integer FeeExponent, so
// both runtimes reduce math.Pow to plain multiplication and should agree
// exactly; the tolerance only guards against an unrelated last-bit rounding
// difference, not a wrong formula.
const feeTolerance = 1e-9

func TestAdjustBuyAmountForFees(t *testing.T) {
	for _, tc := range feeCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AdjustBuyAmountForFees(tc.p)
			if err != nil {
				t.Fatalf("AdjustBuyAmountForFees(%+v): %v", tc.p, err)
			}
			if diff := math.Abs(got.Amount - tc.want); diff > feeTolerance {
				t.Errorf("Amount = %.17g, want %.17g (diff %g)", got.Amount, tc.want, diff)
			}
			if got.Reduced != tc.wantReduced {
				t.Errorf("Reduced = %v, want %v", got.Reduced, tc.wantReduced)
			}
		})
	}
}

// slippageCase is one FeeSlippage value paired with whether
// AdjustBuyAmountForFees must accept it.
type slippageCase struct {
	name    string
	slip    float64
	wantErr bool
}

var slippageCases = []slippageCase{
	{name: "zero: no cushion", slip: 0, wantErr: false},
	{name: "one: the minimum nonzero cushion", slip: 1, wantErr: false},
	{name: "one hundred: the maximum cushion", slip: 100, wantErr: false},
	{name: "fifty: mid-range", slip: 50, wantErr: false},
	{name: "half a percent: below the minimum nonzero cushion", slip: 0.5, wantErr: true},
	{name: "negative", slip: -1, wantErr: true},
	{name: "above one hundred", slip: 100.5, wantErr: true},
	{name: "not a number", slip: math.NaN(), wantErr: true},
	{name: "positive infinity", slip: math.Inf(1), wantErr: true},
}

// TestAdjustBuyAmountForFeesSlippageValidation checks the slippage bound the
// SDK's validateFeeSlippage enforces: zero, or a percentage between 1 and
// 100 inclusive.
func TestAdjustBuyAmountForFeesSlippageValidation(t *testing.T) {
	base := FeeParams{Amount: 100, Price: 0.5, USDCBalance: 1000, FeeRate: 0.03, FeeExponent: 2}
	for _, tc := range slippageCases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.FeeSlippage = tc.slip
			got, err := AdjustBuyAmountForFees(p)
			if (err != nil) != tc.wantErr {
				t.Fatalf("AdjustBuyAmountForFees(slippage=%v): err = %v, wantErr %v", tc.slip, err, tc.wantErr)
			}
			if err != nil && got != (FeeAdjustment{}) {
				t.Errorf("AdjustBuyAmountForFees(slippage=%v): result = %+v on error, want zero value", tc.slip, got)
			}
		})
	}
}

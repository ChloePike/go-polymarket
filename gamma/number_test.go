// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

import (
	"encoding/json"
	"math/big"
	"testing"
)

// None of the three values below survives a float64 intact: 0.29 is not a
// binary fraction and is stored as 0.28999999999999998, 1e-8 is likewise
// not one, and 123456789.12345678 needs 17 significant digits and is stored
// as 123456789.12345677614. A decode-and-reprint check through float64
// would still pass, because Go prints the shortest text that reads back as
// the same float64 — the damage only shows once the value is used, where a
// float64 price of 0.29 times a size of 100 yields 28.999999999999996.
// json.Number never converts at all: it holds the wire's own characters
// until a caller parses them exactly.
const (
	inexactBinary  = "0.29"
	tinyExponent   = "1e-8"
	seventeenDigit = "123456789.12345678"
)

// A gammaNumberCase is one decoded json.Number field and the wire text it
// must still hold, character for character.
type gammaNumberCase struct {
	name string
	got  json.Number
	want string
}

// checkNumbers reports every case whose decoded text drifted from the text
// the response carried.
func checkNumbers(t *testing.T, cases []gammaNumberCase) {
	t.Helper()
	for _, tc := range cases {
		if tc.got.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got.String(), tc.want)
		}
	}
}

// marketNumbersJSON is a Market response whose every amount, price and size
// is a value float64 cannot hold.
const marketNumbersJSON = `{
	"id": "1",
	"bestBid": 0.29,
	"bestAsk": 1e-8,
	"lastTradePrice": 123456789.12345678,
	"spread": 0.29,
	"oneDayPriceChange": -0.29,
	"orderMinSize": 0.29,
	"orderPriceMinTickSize": 1e-8,
	"liquidityNum": 123456789.12345678,
	"liquidityClob": 0.29,
	"liquidityAmm": 1e-8,
	"volumeNum": 123456789.12345678,
	"volume24hr": 0.29,
	"volume1yrClob": 1e-8,
	"rewardsMinSize": 0.29,
	"rewardsMaxSpread": 1e-8,
	"feeSchedule": {"exponent": 1, "rate": 0.04, "rebateRate": 0.29, "takerOnly": true},
	"clobRewards": [{"id": "1", "rewardsAmount": 123456789.12345678, "rewardsDailyRate": 1e-8}]
}`

// eventNumbersJSON is the same exercise for Event and its nested Series.
const eventNumbersJSON = `{
	"id": "1",
	"liquidity": 0.29,
	"liquidityClob": 1e-8,
	"openInterest": 1e-8,
	"volume": 123456789.12345678,
	"volume24hr": 0.29,
	"series": [{"id": "1", "liquidity": 123456789.12345678, "volume": 0.29, "volume24hr": 1e-8}]
}`

// profileNumbersJSON and seriesSummaryNumbersJSON cover the two amounts
// declared in extra.go rather than gamma.go.
const (
	profileNumbersJSON       = `{"proxyWallet": "0x1", "weightedVolume": 123456789.12345678}`
	seriesSummaryNumbersJSON = `{"id": "1", "volume": 0.29, "volume24hr": 1e-8}`
)

// TestNumbersKeepTheirWireText decodes responses carrying values no float64
// represents and pins that every money, price and size field still holds
// the exact characters Gamma sent — the property the whole json.Number
// convention exists for.
func TestNumbersKeepTheirWireText(t *testing.T) {
	var m Market
	decodeStrict(t, marketNumbersJSON, &m)
	checkNumbers(t, []gammaNumberCase{
		{"Market.BestBid", m.BestBid, inexactBinary},
		{"Market.BestAsk", m.BestAsk, tinyExponent},
		{"Market.LastTradePrice", m.LastTradePrice, seventeenDigit},
		{"Market.Spread", m.Spread, inexactBinary},
		{"Market.OneDayPriceChange", m.OneDayPriceChange, "-" + inexactBinary},
		{"Market.OrderMinSize", m.OrderMinSize, inexactBinary},
		{"Market.OrderPriceMinTickSize", m.OrderPriceMinTickSize, tinyExponent},
		{"Market.LiquidityNum", m.LiquidityNum, seventeenDigit},
		{"Market.LiquidityCLOB", m.LiquidityCLOB, inexactBinary},
		{"Market.LiquidityAMM", m.LiquidityAMM, tinyExponent},
		{"Market.VolumeNum", m.VolumeNum, seventeenDigit},
		{"Market.Volume24hr", m.Volume24hr, inexactBinary},
		{"Market.Volume1yrCLOB", m.Volume1yrCLOB, tinyExponent},
		{"Market.RewardsMinSize", m.RewardsMinSize, inexactBinary},
		{"Market.RewardsMaxSpread", m.RewardsMaxSpread, tinyExponent},
		{"Market.FeeSchedule.RebateRate", m.FeeSchedule.RebateRate, inexactBinary},
		{"Market.ClobRewards[0].RewardsAmount", m.ClobRewards[0].RewardsAmount, seventeenDigit},
		{"Market.ClobRewards[0].RewardsDailyRate", m.ClobRewards[0].RewardsDailyRate, tinyExponent},
	})

	var e Event
	decodeStrict(t, eventNumbersJSON, &e)
	if len(e.Series) != 1 {
		t.Fatalf("len(Series) = %d, want 1", len(e.Series))
	}
	checkNumbers(t, []gammaNumberCase{
		{"Event.Liquidity", e.Liquidity, inexactBinary},
		{"Event.LiquidityCLOB", e.LiquidityCLOB, tinyExponent},
		{"Event.OpenInterest", e.OpenInterest, tinyExponent},
		{"Event.Volume", e.Volume, seventeenDigit},
		{"Event.Volume24hr", e.Volume24hr, inexactBinary},
		{"Series.Liquidity", e.Series[0].Liquidity, seventeenDigit},
		{"Series.Volume", e.Series[0].Volume, inexactBinary},
		{"Series.Volume24hr", e.Series[0].Volume24hr, tinyExponent},
	})

	var p PublicProfileResponse
	decodeStrict(t, profileNumbersJSON, &p)
	var s SeriesSummary
	decodeStrict(t, seriesSummaryNumbersJSON, &s)
	checkNumbers(t, []gammaNumberCase{
		{"PublicProfileResponse.WeightedVolume", p.WeightedVolume, seventeenDigit},
		{"SeriesSummary.Volume", s.Volume, inexactBinary},
		{"SeriesSummary.Volume24hr", s.Volume24hr, tinyExponent},
	})
}

// TestNumberTextIsExactUnderArithmetic shows what the preserved text buys a
// caller: 0.29 read as a big.Rat times a size of 100 is exactly 29, where
// the same multiplication in float64 gives 28.999999999999996.
func TestNumberTextIsExactUnderArithmetic(t *testing.T) {
	var m Market
	decodeStrict(t, marketNumbersJSON, &m)

	price, ok := new(big.Rat).SetString(string(m.BestBid))
	if !ok {
		t.Fatalf("SetString(%q) failed", m.BestBid)
	}
	notional := new(big.Rat).Mul(price, new(big.Rat).SetInt64(100))
	if notional.Cmp(new(big.Rat).SetInt64(29)) != 0 {
		t.Errorf("0.29 * 100 = %s, want exactly 29", notional.RatString())
	}
}

// TestOmittedNumberIsEmptyNotZero pins the nullability rule this package
// documents: a number Gamma omits or sends as null decodes to the empty
// json.Number, which big.Rat rejects, so a caller must treat it as "not
// reported" rather than as zero.
func TestOmittedNumberIsEmptyNotZero(t *testing.T) {
	var m Market
	decodeStrict(t, `{"id": "1", "bestBid": null}`, &m)
	if m.BestBid != "" {
		t.Errorf("BestBid = %q, want empty for a null wire value", m.BestBid)
	}
	if m.VolumeNum != "" {
		t.Errorf("VolumeNum = %q, want empty for an omitted field", m.VolumeNum)
	}
	if _, ok := new(big.Rat).SetString(string(m.BestBid)); ok {
		t.Error("big.Rat parsed an empty json.Number; callers rely on it failing")
	}
}

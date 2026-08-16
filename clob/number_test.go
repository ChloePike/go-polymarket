// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"encoding/json"
	"testing"
)

// exactNumberCase is one decoded json.Number field and the wire text it must
// still hold. Used by the tests below to pin that decoding preserves a
// value's digits rather than reformatting them.
type exactNumberCase struct {
	name string
	got  json.Number
	want string
}

// check reports every case whose decoded text drifted from the wire text.
func (cases exactNumberCases) check(t *testing.T) {
	t.Helper()
	for _, tc := range cases {
		if tc.got.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got.String(), tc.want)
		}
	}
}

// exactNumberCases is a set of exactNumberCase sharing one decoded response.
type exactNumberCases []exactNumberCase

// None of the three values below survives a float64 intact: 0.29 is stored
// as 0.28999999999999998, 1e-8 is likewise not a binary fraction, and
// 123456789.12345678 needs 17 significant digits and is stored as
// 123456789.12345677614. Go's own marshaller hides this, since it prints the
// shortest text that reads back as the same float64, so a decode-and-reprint
// check would pass while the number was already wrong; the drift only
// surfaces once the value is used, where a float64 price of 0.29 times a
// float64 size of 100 yields 28.999999999999996 (at run time -- Go folds the
// same expression written as constants to exactly 29) and big.Rat.SetFloat64
// pins the error permanently. json.Number never converts
// at all: it holds the wire's own characters until a caller parses them
// exactly.
const (
	inexactBinary = "0.29"
	tinyExponent  = "1e-8"
	seventeenDigs = "123456789.12345678"
)

// marketExactBody is a Market response carrying float64-hostile values in
// every numeric field Market and its nested types declare.
const marketExactBody = `{
	"condition_id": "0xcond",
	"minimum_order_size": 0.29,
	"minimum_tick_size": 1e-8,
	"tokens": [{"token_id": "1", "outcome": "Yes", "price": 123456789.12345678}],
	"rewards": {
		"min_size": 0.29,
		"max_spread": 1e-8,
		"rates": [{"asset_address": "0xasset", "rewards_daily_rate": 123456789.12345678}]
	}
}`

// TestMarketNumbersKeepExactText decodes a market response whose numeric
// fields hold values no float64 represents exactly, and asserts each field
// still reads back character-for-character as the wire wrote it.
func TestMarketNumbersKeepExactText(t *testing.T) {
	var m Market
	if err := json.Unmarshal([]byte(marketExactBody), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Tokens) != 1 || len(m.Rewards.Rates) != 1 {
		t.Fatalf("decoded %d tokens and %d rates, want 1 and 1", len(m.Tokens), len(m.Rewards.Rates))
	}
	exactNumberCases{
		{name: "MinimumOrderSize", got: m.MinimumOrderSize, want: inexactBinary},
		{name: "MinimumTickSize", got: m.MinimumTickSize, want: tinyExponent},
		{name: "Tokens[0].Price", got: m.Tokens[0].Price, want: seventeenDigs},
		{name: "Rewards.MinSize", got: m.Rewards.MinSize, want: inexactBinary},
		{name: "Rewards.MaxSpread", got: m.Rewards.MaxSpread, want: tinyExponent},
		{name: "Rewards.Rates[0].RewardsDailyRate", got: m.Rewards.Rates[0].RewardsDailyRate, want: seventeenDigs},
	}.check(t)
}

// rewardsMarketExactBody is a RewardsMarket response carrying the same
// float64-hostile values.
const rewardsMarketExactBody = `{
	"condition_id": "0xcond",
	"rewards_max_spread": 0.29,
	"rewards_min_size": 1e-8,
	"native_daily_rate": 123456789.12345678,
	"total_daily_rate": 0.29,
	"rewards_config": [{"rate_per_day": 1e-8, "total_rewards": 123456789.12345678}]
}`

// TestRewardsNumbersKeepExactText is TestMarketNumbersKeepExactText for the
// rewards response shapes.
func TestRewardsNumbersKeepExactText(t *testing.T) {
	var r RewardsMarket
	if err := json.Unmarshal([]byte(rewardsMarketExactBody), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.RewardsConfig) != 1 {
		t.Fatalf("decoded %d reward configs, want 1", len(r.RewardsConfig))
	}
	exactNumberCases{
		{name: "RewardsMaxSpread", got: r.RewardsMaxSpread, want: inexactBinary},
		{name: "RewardsMinSize", got: r.RewardsMinSize, want: tinyExponent},
		{name: "NativeDailyRate", got: r.NativeDailyRate, want: seventeenDigs},
		{name: "TotalDailyRate", got: r.TotalDailyRate, want: inexactBinary},
		{name: "RewardsConfig[0].RatePerDay", got: r.RewardsConfig[0].RatePerDay, want: tinyExponent},
		{name: "RewardsConfig[0].TotalRewards", got: r.RewardsConfig[0].TotalRewards, want: seventeenDigs},
	}.check(t)
}

// clobMarketExactBody is a compact ClobMarket response, whose abbreviated
// keys carry the same float64-hostile values.
const clobMarketExactBody = `{
	"c": "0xcond",
	"mts": 1e-8,
	"mos": 0.29,
	"r": {"mi": 0.29, "ma": 1e-8, "moas": 123456789.12345678}
}`

// TestClobMarketNumbersKeepExactText covers the compact market summary and
// the price-history point, the two remaining json.Number carriers.
func TestClobMarketNumbersKeepExactText(t *testing.T) {
	var m ClobMarket
	if err := json.Unmarshal([]byte(clobMarketExactBody), &m); err != nil {
		t.Fatal(err)
	}
	if m.Rewards == nil {
		t.Fatal("decoded nil Rewards, want a value")
	}
	exactNumberCases{
		{name: "TickSize", got: m.TickSize, want: tinyExponent},
		{name: "MinOrderSize", got: m.MinOrderSize, want: inexactBinary},
		{name: "Rewards.MinSize", got: m.Rewards.MinSize, want: inexactBinary},
		{name: "Rewards.MaxSpread", got: m.Rewards.MaxSpread, want: tinyExponent},
		{name: "Rewards.MOAS", got: m.Rewards.MOAS, want: seventeenDigs},
	}.check(t)

	var p PriceHistoryPoint
	if err := json.Unmarshal([]byte(`{"t": 1782753357, "p": 123456789.12345678}`), &p); err != nil {
		t.Fatal(err)
	}
	exactNumberCases{{name: "PriceHistoryPoint.Price", got: p.Price, want: seventeenDigs}}.check(t)
}

// TestMarketRewardsNullNumbersDecode pins that a null reward figure decodes
// to the empty json.Number rather than failing the whole response:
// GET /clob-markets serves null for mi and ma on a market with no reward
// program, live-observed.
func TestMarketRewardsNullNumbersDecode(t *testing.T) {
	var m ClobMarket
	if err := json.Unmarshal([]byte(`{"c": "0xcond", "r": {"mi": null, "ma": null}}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Rewards == nil {
		t.Fatal("decoded nil Rewards, want a value")
	}
	if m.Rewards.MinSize != "" || m.Rewards.MaxSpread != "" {
		t.Errorf("MinSize = %q, MaxSpread = %q, want both empty", m.Rewards.MinSize, m.Rewards.MaxSpread)
	}
}

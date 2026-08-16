// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/json"
	"math/big"
	"testing"
)

// A parseAmountCase is one piece of text and what it should parse to, or ""
// when it should be refused.
type parseAmountCase struct {
	name string
	text string
	want string // FloatString(8) of the result, or "" for an error
}

// The refusals matter as much as the successes. big.Rat's own parser accepts
// forms no Polymarket endpoint sends, and a corrupted field that parses is
// worse than one that does not.
var parseAmountCases = []parseAmountCase{
	{"a price", "0.29", "0.29000000"},
	{"a size", "162963.4451", "162963.44510000"},
	{"a negative profit", "-6997.4491", "-6997.44910000"},
	{"an integer", "20", "20.00000000"},
	{"scientific notation, which the API does send", "1e-8", "0.00000001"},
	{"seventeen significant digits", "123456789.12345678", "123456789.12345678"},
	{"zero", "0", "0.00000000"},

	{"nothing, which is how a null number decodes", "", ""},
	{"a fraction, which big.Rat would accept", "1/3", ""},
	{"a hexadecimal float, which big.Rat would accept", "0x1p-2", ""},
	{"words", "lots", ""},
	{"a number with a comma", "1,000", ""},
	{"trailing text", "20 shares", ""},
}

func TestParseAmount(t *testing.T) {
	for _, tc := range parseAmountCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAmount(tc.text)
			if tc.want == "" {
				if err == nil {
					t.Errorf("%q parsed to %s, want an error", tc.text, got.FloatString(8))
				}
				return
			}
			if err != nil {
				t.Fatalf("%q: %v", tc.text, err)
			}
			if s := got.FloatString(8); s != tc.want {
				t.Errorf("%q parsed to %s, want %s", tc.text, s, tc.want)
			}
		})
	}
}

// TestParseAmountIsExactWhereFloatIsNot is the point of the whole convention.
// The same arithmetic through a float64 gives 28.999999999999996, and an
// order sized that way is signed at the wrong number.
func TestParseAmountIsExactWhereFloatIsNot(t *testing.T) {
	price, err := ParseAmount("0.29")
	if err != nil {
		t.Fatal(err)
	}
	got := new(big.Rat).Mul(price, big.NewRat(100, 1))
	if got.Cmp(big.NewRat(29, 1)) != 0 {
		t.Errorf("0.29 * 100 = %s, want exactly 29", got.FloatString(20))
	}
	if s := FormatAmount(got, 6); s != "29.000000" {
		t.Errorf("formatted as %s, want 29.000000", s)
	}
}

// TestParseAmountReadsAJSONNumber checks the shape a caller actually holds:
// every money field in the response types decodes to json.Number, and its
// text goes straight in.
func TestParseAmountReadsAJSONNumber(t *testing.T) {
	var n json.Number
	if err := json.Unmarshal([]byte(`123456789.12345678`), &n); err != nil {
		t.Fatal(err)
	}
	got, err := ParseAmount(string(n))
	if err != nil {
		t.Fatal(err)
	}
	if s := got.FloatString(8); s != "123456789.12345678" {
		t.Errorf("round-tripped to %s, want the text it arrived as", s)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"encoding/json"
	"testing"
)

// exactNumberCase is one raw JSON token a frame can carry in a numeric
// field, and the exact text the decoded value must still hold.
type exactNumberCase struct {
	name string
	raw  string
	want string
}

// exactNumberCases pairs each value a float64 cannot hold with both wire
// shapes Number accepts. 0.29 is stored as 0.28999999999999998, 1e-8 is
// likewise not a binary fraction, and 123456789.12345678 needs 17
// significant digits and is stored as 123456789.12345677614. Go's marshaller
// prints the shortest text that reads back as the same float64, so the drift
// stays invisible until the value is used in arithmetic. Number converts
// nothing: it keeps the wire's own characters.
var exactNumberCases = []exactNumberCase{
	{name: "bare inexact binary", raw: `0.29`, want: "0.29"},
	{name: "quoted inexact binary", raw: `"0.29"`, want: "0.29"},
	{name: "bare tiny exponent", raw: `1e-8`, want: "1e-8"},
	{name: "quoted tiny exponent", raw: `"1e-8"`, want: "1e-8"},
	{name: "bare seventeen digits", raw: `123456789.12345678`, want: "123456789.12345678"},
	{name: "quoted seventeen digits", raw: `"123456789.12345678"`, want: "123456789.12345678"},
}

// TestNumberKeepsExactText asserts Number preserves a value no float64 holds
// exactly, in both the bare-number and JSON-string shapes the RTDS gateway
// has been seen to use for the same field.
func TestNumberKeepsExactText(t *testing.T) {
	for _, tc := range exactNumberCases {
		t.Run(tc.name, func(t *testing.T) {
			var got Number
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("Number(%s) = %q, want %q", tc.raw, got.String(), tc.want)
			}
		})
	}
}

// TestPriceUpdateValueKeepsExactText runs the same value through a whole
// RTDS frame, so the guarantee is pinned at the decoder rather than only on
// Number itself.
func TestPriceUpdateValueKeepsExactText(t *testing.T) {
	const frame = `{"topic":"crypto_prices","type":"update","timestamp":1782753357257,` +
		`"payload":{"symbol":"btcusdt","timestamp":1782753357257,"value":123456789.12345678}}`

	events, err := decodeRTDS([]byte(frame))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("decoded %d events, want 1", len(events))
	}
	p, ok := events[0].(PriceUpdateEvent)
	if !ok {
		t.Fatalf("got %T, want PriceUpdateEvent", events[0])
	}
	if p.Value.String() != "123456789.12345678" {
		t.Errorf("Value = %q, want %q", p.Value.String(), "123456789.12345678")
	}
}

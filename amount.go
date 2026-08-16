// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"fmt"
	"math/big"
	"strings"
)

// Every amount, price and size in this library is decimal text — a string on
// the wire, a json.Number in a decoded response — and never a float64. A
// price of 0.29 multiplied by 100 in float64 is 28.999999999999996, and an
// order's amounts are covered by a signature, so a rounding artefact is not
// an artefact: it is a signed commitment to the wrong number.
//
// ParseAmount is how that text becomes something to do arithmetic with.

// ParseAmount converts decimal text into an exact rational.
//
// Use it on any amount, price or size a response carried:
//
//	size, err := polymarket.ParseAmount(string(position.Size))
//
// It is stricter than big.Rat's own parser, which also accepts a fraction
// like "1/3" and a hexadecimal float. No Polymarket endpoint sends either, so
// accepting them would only turn a corrupted field into a plausible number.
//
// Empty text is an error rather than zero. A number the server omitted or
// sent as null decodes to the empty json.Number, and "not reported" is not
// the same as "none" — a position with no value and a position whose value
// failed to compute call for different handling.
func ParseAmount(s string) (*big.Rat, error) {
	if s == "" {
		return nil, fmt.Errorf("polymarket: no amount")
	}
	if strings.ContainsAny(s, "/xX") {
		return nil, fmt.Errorf("polymarket: %q is not a decimal amount", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("polymarket: %q is not a decimal amount", s)
	}
	return r, nil
}

// FormatAmount renders a rational as fixed-point decimal text with the given
// number of places, rounding half away from zero.
//
// Six places is what the wire uses for USDC and for the conditional tokens
// alike; Decimals holds that.
func FormatAmount(r *big.Rat, places int) string {
	return r.FloatString(places)
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package order converts a caller's UserOrder into a signed on-wire order:
// price/size -> maker/taker integer amounts, per the market tick size.
package order

import (
	"math/big"
)

// roundNormal rounds x to n decimal places, half-up.
func roundNormal(x *big.Rat, n int) *big.Rat {
	return roundScaled(x, n, roundHalfUp)
}

// roundDown truncates x toward zero to n decimal places.
func roundDown(x *big.Rat, n int) *big.Rat {
	return roundScaled(x, n, roundFloor)
}

// roundUp rounds x away from zero (ceiling for non-negative) to n places.
func roundUp(x *big.Rat, n int) *big.Rat {
	return roundScaled(x, n, roundCeil)
}

type mode int

const (
	roundHalfUp mode = iota
	roundFloor
	roundCeil
)

// roundScaled scales x by 10^n, applies the mode to reach an integer, then
// scales back. Inputs are assumed non-negative (prices, sizes, amounts).
func roundScaled(x *big.Rat, n int, m mode) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	scaled := new(big.Rat).Mul(x, new(big.Rat).SetInt(scale))

	num := scaled.Num()
	den := scaled.Denom()
	q := new(big.Int)
	r := new(big.Int)
	q.QuoRem(num, den, r)

	switch m {
	case roundFloor:
		// q already truncated toward zero; inputs non-negative => floor.
	case roundCeil:
		if r.Sign() != 0 {
			q.Add(q, big.NewInt(1))
		}
	case roundHalfUp:
		twiceR := new(big.Int).Mul(r, big.NewInt(2))
		if twiceR.CmpAbs(den) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	}

	out := new(big.Rat).SetInt(q)
	return out.Quo(out, new(big.Rat).SetInt(scale))
}

// decimalPlaces reports how many decimal places x needs (bounded by cap).
func decimalPlaces(x *big.Rat, cap int) int {
	for n := 0; n <= cap; n++ {
		if roundDown(x, n).Cmp(x) == 0 {
			return n
		}
	}
	return cap + 1
}

// toFixedInt scales a *big.Rat by 10^decimals and returns the integer string,
// truncating any residual fraction (amounts are pre-rounded to <= `amount`
// decimals, and decimals here is 6, so this is exact in practice).
func toFixedInt(x *big.Rat, decimals int) string {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	scaled := new(big.Rat).Mul(x, new(big.Rat).SetInt(scale))
	i := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return i.String()
}

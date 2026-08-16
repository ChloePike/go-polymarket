// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package amount converts a caller's price and size into the integer maker and
// taker amounts an order carries on the wire.
//
// Every value here is an exact rational. The official TypeScript SDK performs
// this arithmetic in float64; this package does not, and a 2520-point grid of
// prices, sizes and tick sizes captured from that SDK agrees with it on every
// point (see testdata/vectors.json). Exact arithmetic is therefore free: it
// costs no wire compatibility and removes a class of rounding artefact that
// would otherwise sign an order for an amount the caller did not intend.
package amount

import (
	"fmt"
	"math/big"
)

// A Config gives the decimal-place limits that apply at one market tick size.
type Config struct {
	Price  int // decimals the price is rounded to
	Size   int // decimals the size is rounded to
	Amount int // decimals a derived amount may carry
}

// ByTickSize maps a market's tick size to its rounding limits.
var ByTickSize = map[string]Config{
	"0.1":    {Price: 1, Size: 2, Amount: 3},
	"0.01":   {Price: 2, Size: 2, Amount: 4},
	"0.005":  {Price: 3, Size: 2, Amount: 5},
	"0.0025": {Price: 4, Size: 2, Amount: 6},
	"0.001":  {Price: 3, Size: 2, Amount: 5},
	"0.0001": {Price: 4, Size: 2, Amount: 6},
}

// Decimals is the fixed-point scale of both USDC and the conditional tokens.
const Decimals = 6

// Raw is a maker/taker amount pair before fixed-point conversion. Maker is
// what the order gives up and Taker is what it asks for, so the units swap
// with the side: a buy pays USDC for shares, a sell pays shares for USDC.
type Raw struct {
	Maker *big.Rat
	Taker *big.Rat
}

// Limit sizes a limit order. size is in shares for both sides.
//
//	buy:  taker = roundDown(size);  maker = taker × roundHalfUp(price)
//	sell: maker = roundDown(size);  taker = maker × roundHalfUp(price)
func Limit(buy bool, size, price *big.Rat, cfg Config) Raw {
	p := roundHalfUp(price, cfg.Price)
	whole := roundDown(size, cfg.Size)
	derived := converge(new(big.Rat).Mul(whole, p), cfg.Amount)
	if buy {
		return Raw{Maker: derived, Taker: whole}
	}
	return Raw{Maker: whole, Taker: derived}
}

// Market sizes a marketable order. It differs from Limit in two ways that are
// easy to miss: the price is rounded DOWN rather than half-up, and a buy is
// sized in USDC, so its share count is a division rather than a product.
//
//	buy:  maker = roundDown(amount in USDC);   taker = maker ÷ roundDown(price)
//	sell: maker = roundDown(amount in shares); taker = maker × roundDown(price)
func Market(buy bool, amt, price *big.Rat, cfg Config) (Raw, error) {
	p := roundDown(price, cfg.Price)
	maker := roundDown(amt, cfg.Size)
	if buy {
		if p.Sign() == 0 {
			return Raw{}, fmt.Errorf("amount: market buy at price %s rounds to zero at %d decimals", price, cfg.Price)
		}
		taker := converge(new(big.Rat).Quo(maker, p), cfg.Amount)
		return Raw{Maker: maker, Taker: taker}, nil
	}
	taker := converge(new(big.Rat).Mul(maker, p), cfg.Amount)
	return Raw{Maker: maker, Taker: taker}, nil
}

// converge pulls a derived amount inside the allowed decimal places. It first
// nudges up four places beyond the limit, which recovers a value that a
// division left just under a representable amount, and only then truncates.
// Both steps are load-bearing: dropping the nudge changes results the exchange
// has already accepted.
func converge(v *big.Rat, limit int) *big.Rat {
	if decimalPlaces(v, limit) <= limit {
		return v
	}
	v = roundUp(v, limit+4)
	if decimalPlaces(v, limit) > limit {
		v = roundDown(v, limit)
	}
	return v
}

// Fixed renders an amount as the integer string the wire carries, scaled by
// 10^Decimals. Amounts reach this point with no more than Decimals places, so
// the conversion is exact.
func Fixed(v *big.Rat) string {
	scale := pow10(Decimals)
	scaled := new(big.Rat).Mul(v, new(big.Rat).SetInt(scale))
	return new(big.Int).Quo(scaled.Num(), scaled.Denom()).String()
}

// ParseDecimal reads a decimal string such as "0.52" exactly.
func ParseDecimal(s string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("amount: invalid decimal %q", s)
	}
	return r, nil
}

// roundHalfUp rounds to n decimal places, ties away from zero.
func roundHalfUp(x *big.Rat, n int) *big.Rat { return round(x, n, halfUp) }

// roundDown truncates toward zero to n decimal places.
func roundDown(x *big.Rat, n int) *big.Rat { return round(x, n, floor) }

// roundUp rounds away from zero to n decimal places.
func roundUp(x *big.Rat, n int) *big.Rat { return round(x, n, ceil) }

type mode int

const (
	halfUp mode = iota
	floor
	ceil
)

// round scales x by 10^n, resolves it to an integer under the given mode, then
// scales back. Inputs are prices, sizes and amounts, so they are non-negative.
func round(x *big.Rat, n int, m mode) *big.Rat {
	scale := pow10(n)
	scaled := new(big.Rat).Mul(x, new(big.Rat).SetInt(scale))

	q, r := new(big.Int), new(big.Int)
	q.QuoRem(scaled.Num(), scaled.Denom(), r)

	switch m {
	case floor:
		// QuoRem already truncated toward zero, which is floor here.
	case ceil:
		if r.Sign() != 0 {
			q.Add(q, big.NewInt(1))
		}
	case halfUp:
		if new(big.Int).Mul(r, big.NewInt(2)).CmpAbs(scaled.Denom()) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	}

	out := new(big.Rat).SetInt(q)
	return out.Quo(out, new(big.Rat).SetInt(scale))
}

// decimalPlaces reports the decimal places x needs, stopping at limit+1 to
// stay finite: a rational such as 1/3 never terminates.
func decimalPlaces(x *big.Rat, limit int) int {
	for n := 0; n <= limit; n++ {
		if roundDown(x, n).Cmp(x) == 0 {
			return n
		}
	}
	return limit + 1
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

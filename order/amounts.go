// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package order

import (
	"fmt"
	"math/big"

	"github.com/ChloePike/go-polymarket/types"
)

// RawAmounts holds the pre-fixed-point maker/taker amounts.
type RawAmounts struct {
	Side  types.Side
	Maker *big.Rat
	Taker *big.Rat
}

// GetRawAmounts mirrors getOrderRawAmounts from the official SDK.
//
// BUY:  taker = roundDown(size); maker = taker*price   (pay USDC, get shares)
// SELL: maker = roundDown(size); taker = maker*price   (pay shares, get USDC)
//
// When the derived amount exceeds `amount` decimals it is nudged up at
// amount+4 places then floored to `amount` — matching the SDK exactly.
func GetRawAmounts(side types.Side, size, price *big.Rat, cfg types.RoundConfig) RawAmounts {
	rawPrice := roundNormal(price, cfg.Price)

	if side == types.SideBuy {
		taker := roundDown(size, cfg.Size)
		maker := new(big.Rat).Mul(taker, rawPrice)
		maker = converge(maker, cfg.Amount)
		return RawAmounts{Side: types.SideBuy, Maker: maker, Taker: taker}
	}

	maker := roundDown(size, cfg.Size)
	taker := new(big.Rat).Mul(maker, rawPrice)
	taker = converge(taker, cfg.Amount)
	return RawAmounts{Side: types.SideSell, Maker: maker, Taker: taker}
}

func converge(v *big.Rat, amount int) *big.Rat {
	if decimalPlaces(v, amount) <= amount {
		return v
	}
	v = roundUp(v, amount+4)
	if decimalPlaces(v, amount) > amount {
		v = roundDown(v, amount)
	}
	return v
}

// FixedAmounts returns the integer (6-dp) maker/taker strings for the wire.
func FixedAmounts(r RawAmounts) (maker, taker string) {
	return toFixedInt(r.Maker, types.CollateralDecimals),
		toFixedInt(r.Taker, types.CollateralDecimals)
}

// ratFromDecimal parses a decimal string like "0.52" into a *big.Rat.
func ratFromDecimal(s string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("go-polymarket/order: invalid decimal %q", s)
	}
	return r, nil
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

// This file uses float64, the one exception CLAUDE.md's no-float64 rule
// allows. The platform fee rate raises (price * (1 - price)) to a fractional
// exponent, an operation math/big.Rat cannot express. The result is an
// advisory pre-trade estimate only: it is never signed, and no amount this
// file computes reaches an order's signed fields. A caller who acts on the
// estimate still passes the amount to BuildMarketOrder as a decimal string,
// which is parsed and computed exactly at signing time.

import (
	"fmt"
	"math"
)

// FeeParams are the inputs to a pre-trade estimate of the fees on a market
// buy.
//
// FeeRate and FeeExponent describe the platform's fee curve for one market: a
// caller reads both from the market's own fee data on the clob-markets
// endpoint (epClobMarket), already scaled as fractions rather than basis
// points. BuilderTakerFeeRate is the attributed builder's own cut, as a
// fraction of the trade rather than basis points: a caller reads
// builder_taker_fee_rate_bps from the builder-fees endpoint (epBuilderFees)
// and divides by BuilderFeeBps.
type FeeParams struct {
	// Amount is the USDC the caller wants to spend on the buy.
	Amount float64

	// Price is the marketable price the buy is estimated against, in (0, 1).
	Price float64

	// USDCBalance is the caller's available USDC balance.
	USDCBalance float64

	// FeeRate is the platform's base fee rate for the market.
	FeeRate float64

	// FeeExponent shapes how the platform fee rate scales with how far the
	// price sits from 0.5. It comes from the same market fee data as
	// FeeRate.
	FeeExponent float64

	// BuilderTakerFeeRate is the attributed builder's taker fee, as a
	// fraction of the trade rather than basis points. Zero means no
	// builder, or a builder with no fee.
	BuilderTakerFeeRate float64

	// FeeSlippage pads the estimated platform fee rate by a percentage
	// cushion, to absorb the price moving between the estimate and the
	// fill. Zero means no cushion; otherwise it must be between 1 and 100.
	FeeSlippage float64
}

// FeeAdjustment is the result of AdjustBuyAmountForFees: the buy amount to
// submit, and the fee estimate behind that number.
type FeeAdjustment struct {
	// Amount is the buy amount to submit. It equals Params.Amount unless
	// the requested amount plus the estimated fees would not fit
	// USDCBalance, in which case it is reduced so that Amount plus
	// PlatformFee plus BuilderFee does fit — see Reduced.
	Amount float64

	// EffectivePlatformFeeRate is the platform fee rate after the
	// FeeSlippage cushion is applied.
	EffectivePlatformFeeRate float64

	// PlatformFee is the estimated platform fee in USDC, on whichever is
	// smaller of the requested amount and USDCBalance.
	PlatformFee float64

	// BuilderFee is the estimated builder fee in USDC, on whichever is
	// smaller of the requested amount and USDCBalance.
	BuilderFee float64

	// Reduced reports whether the balance cap was applied: USDCBalance was
	// not enough to cover the requested amount plus the estimated fees, so
	// Amount was set to whatever USDCBalance leaves after them. This can be
	// true even when the result equals the requested amount, at the exact
	// boundary where USDCBalance covers the request with nothing to spare.
	Reduced bool
}

// AdjustBuyAmountForFees estimates the platform and builder fees on a market
// buy and, when the requested amount would not fit inside the caller's
// balance alongside those fees, reduces the amount so that it does.
//
// This needs no authentication: it is local arithmetic over numbers the
// caller already has, not a request to the CLOB. It mirrors the protocol's
// own pre-trade estimate exactly, including its edge cases — a Price of 0 or
// 1 drives the platform fee's price factor to zero, and dividing by a Price
// of exactly 0 makes the platform fee not-a-number, which this function
// then leaves unresolved by construction: an amount cannot be compared
// against a not-a-number fee, so the requested amount is returned
// unmodified rather than mistakenly reduced.
func AdjustBuyAmountForFees(p FeeParams) (FeeAdjustment, error) {
	if err := validateFeeSlippage(p.FeeSlippage); err != nil {
		return FeeAdjustment{}, err
	}

	effectiveRate := p.FeeRate * math.Pow(p.Price*(1-p.Price), p.FeeExponent) * (1 + p.FeeSlippage/100)
	feeBase := math.Min(p.Amount, p.USDCBalance)
	platformFee := feeBase / p.Price * effectiveRate
	builderFee := feeBase * p.BuilderTakerFeeRate

	result := FeeAdjustment{
		Amount:                   p.Amount,
		EffectivePlatformFeeRate: effectiveRate,
		PlatformFee:              platformFee,
		BuilderFee:               builderFee,
	}
	if p.USDCBalance <= p.Amount+platformFee+builderFee {
		result.Amount = math.Max(p.USDCBalance-platformFee-builderFee, 0)
		result.Reduced = true
	}
	return result, nil
}

// minFeeSlippagePercentage and maxFeeSlippagePercentage bound the nonzero
// range FeeSlippage may take. Below the minimum the cushion is too small to
// be a deliberate choice; above the maximum it no longer describes a
// percentage cushion on a fee rate.
const (
	minFeeSlippagePercentage = 1
	maxFeeSlippagePercentage = 100
)

// validateFeeSlippage rejects a FeeSlippage value the platform fee formula
// cannot use: it must be exactly zero, meaning no cushion, or a finite
// percentage between minFeeSlippagePercentage and maxFeeSlippagePercentage.
func validateFeeSlippage(feeSlippage float64) error {
	if math.IsNaN(feeSlippage) || math.IsInf(feeSlippage, 0) ||
		feeSlippage < 0 || feeSlippage > maxFeeSlippagePercentage ||
		(feeSlippage > 0 && feeSlippage < minFeeSlippagePercentage) {
		return fmt.Errorf("polymarket: fee slippage must be 0 or a percentage between %d and %d, got %v",
			minFeeSlippagePercentage, maxFeeSlippagePercentage, feeSlippage)
	}
	return nil
}

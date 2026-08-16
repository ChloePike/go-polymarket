// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"fmt"
	"net/url"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A builder code is a bytes32 identifier written into an order's signed
// Builder field. It attributes the order to whoever integrated it — a
// front-end, a bot framework — and that builder earns a share of the trading
// fee the exchange charges on any fill the order produces. This file reads
// the two facts a builder needs about its own code: the rate it is paid, and
// which trades it has been paid on.

// BuilderFeeRates are the fee rates the exchange charges for a builder code,
// from GET /fees/builder-fees/{code}. It needs no authentication.
//
// MakerFeeRateBps and TakerFeeRateBps are quoted against BuilderFeeBps: a
// rate of 100 is one percent, and a rate of 10000 is the whole fee.
type BuilderFeeRates struct {
	Code            string `json:"code"`
	MakerFeeRateBps int    `json:"builder_maker_fee_rate_bps"`
	TakerFeeRateBps int    `json:"builder_taker_fee_rate_bps"`
	// Enabled reports whether the exchange currently attributes fills to
	// this code. A disabled code still has rates on file, but new orders
	// carrying it earn nothing.
	Enabled bool `json:"enabled"`
}

// BuilderFees reports the maker and taker fee rates a builder code earns. It
// needs no authentication: anyone can look up any code's rates.
//
// The exchange 404s for a code it has never registered, surfaced as an
// *Error with StatusCode 404.
func (c *Client) BuilderFees(ctx context.Context, code string) (BuilderFeeRates, error) {
	if err := checkBuilderCode(code); err != nil {
		return BuilderFeeRates{}, err
	}
	var out BuilderFeeRates
	err := c.session.Get(ctx, epBuilderFees+code, nil, &out)
	return out, err
}

// BuilderTradeParams filters a BuilderTrades query. All fields are optional;
// an empty BuilderTradeParams matches every trade attributed to the code.
type BuilderTradeParams struct {
	// ID restricts the result to one trade.
	ID string
	// MakerAddress restricts the result to fills made against one address.
	MakerAddress string
	// Market restricts the result to one condition id.
	Market string
	// AssetID restricts the result to one outcome token id.
	AssetID string
	// Before and After bound the match time, as unix-second strings.
	Before string
	After  string
	// NextCursor resumes a paginated query at the page a previous call's
	// Pagination.NextCursor named. Empty starts at the first page.
	NextCursor string
}

// BuilderTrade is one fill the exchange attributed to a builder code.
//
// Unlike most of the CLOB API, these keys are camelCase: the exchange mirrors
// the official SDK's internal BuilderTrade shape here rather than the
// snake_case the rest of the trading endpoints use. ErrMsg is the one
// straggler kept snake_case on the wire.
type BuilderTrade struct {
	ID             string `json:"id"`
	TradeType      string `json:"tradeType"`
	TakerOrderHash string `json:"takerOrderHash"`
	// Builder is the address that registered the code, not the code itself.
	Builder         string `json:"builder"`
	Market          string `json:"market"`
	AssetID         string `json:"assetId"`
	Side            string `json:"side"`
	Size            string `json:"size"`
	SizeUSDC        string `json:"sizeUsdc"`
	Price           string `json:"price"`
	Status          string `json:"status"`
	Outcome         string `json:"outcome"`
	OutcomeIndex    int    `json:"outcomeIndex"`
	Owner           string `json:"owner"`
	Maker           string `json:"maker"`
	TransactionHash string `json:"transactionHash"`
	MatchTime       string `json:"matchTime"`
	BucketIndex     int    `json:"bucketIndex"`
	// Fee is the total fee the taker paid on the fill; FeeUSDC is the same
	// amount priced in USDC. BuilderFee is this builder's cut of Fee.
	Fee        string `json:"fee"`
	FeeUSDC    string `json:"feeUsdc"`
	BuilderFee string `json:"builderFee"`
	// BuilderCode repeats the code the trade was queried by.
	BuilderCode string `json:"builderCode"`
	// ErrMsg carries a settlement error, when the fill failed after matching.
	ErrMsg    string `json:"err_msg,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// builderTradesPage is the pagination envelope GET /builder/trades returns.
type builderTradesPage struct {
	Data []BuilderTrade `json:"data"`
	Pagination
}

// BuilderTrades returns one page of trades attributed to a builder code,
// most recent first. It needs no authentication: attribution is public,
// unlike a trader's own order history.
//
// params.NextCursor pages the result; pass "" for the first page and keep
// requesting with the returned Pagination.NextCursor until it equals
// CursorEnd.
func (c *Client) BuilderTrades(ctx context.Context, code string, params BuilderTradeParams) ([]BuilderTrade, Pagination, error) {
	if err := checkBuilderCode(code); err != nil {
		return nil, Pagination{}, err
	}

	q := url.Values{}
	q.Set("builder_code", code)
	setIfNotEmpty(q, "id", params.ID)
	setIfNotEmpty(q, "maker_address", params.MakerAddress)
	setIfNotEmpty(q, "market", params.Market)
	setIfNotEmpty(q, "asset_id", params.AssetID)
	setIfNotEmpty(q, "before", params.Before)
	setIfNotEmpty(q, "after", params.After)
	q.Set("next_cursor", cursorOrStart(params.NextCursor))

	var page builderTradesPage
	if err := c.session.Get(ctx, epBuilderTrades, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// checkBuilderCode rejects a code the exchange would reject anyway, before a
// request is sent: empty and the all-zero bytes32 both mean "no builder",
// which /builder/trades and /fees/builder-fees/ have nothing to report for.
func checkBuilderCode(code string) error {
	if code == "" || code == polymarket.ZeroBytes32 {
		return fmt.Errorf("polymarket: builder code is required and cannot be zero")
	}
	return nil
}

func setIfNotEmpty(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

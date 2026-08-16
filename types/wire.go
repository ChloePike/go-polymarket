// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package types

// PostOrderRequest is the exact JSON body of POST /order.
//
// Transcribed from orderToJsonV2 in the official SDK. Note that `salt` is an
// integer here (not a string) and the L2 HMAC is computed over the marshalled
// form of this struct — so field presence/order must stay stable.
type PostOrderRequest struct {
	DeferExec bool      `json:"deferExec"`
	PostOnly  bool      `json:"postOnly"`
	Order     WireOrder `json:"order"`
	Owner     string    `json:"owner"` // the L2 API key
	OrderType OrderType `json:"orderType"`
}

// WireOrder is the `order` object inside PostOrderRequest.
type WireOrder struct {
	Salt          int64         `json:"salt"`
	Maker         string        `json:"maker"`
	Signer        string        `json:"signer"`
	Taker         string        `json:"taker"`
	TokenID       string        `json:"tokenId"`
	MakerAmount   string        `json:"makerAmount"`
	TakerAmount   string        `json:"takerAmount"`
	Side          Side          `json:"side"`
	SignatureType SignatureType `json:"signatureType"`
	Timestamp     string        `json:"timestamp"`
	Expiration    string        `json:"expiration"`
	Metadata      string        `json:"metadata"`
	Builder       string        `json:"builder"`
	Signature     string        `json:"signature"`
}

// PostOrderResponse is the response of POST /order (best-effort fields).
type PostOrderResponse struct {
	Success            bool     `json:"success"`
	ErrorMsg           string   `json:"errorMsg,omitempty"`
	OrderID            string   `json:"orderID,omitempty"`
	TransactionsHashes []string `json:"transactionsHashes,omitempty"`
	TradeIDs           []string `json:"tradeIDs,omitempty"`
	Status             string   `json:"status,omitempty"`
}

// BuilderFeeRates is the response of GET /fees/builder-fees/{code}.
type BuilderFeeRates struct {
	Code                   string `json:"code"`
	BuilderMakerFeeRateBps int    `json:"builder_maker_fee_rate_bps"`
	BuilderTakerFeeRateBps int    `json:"builder_taker_fee_rate_bps"`
	Enabled                bool   `json:"enabled"`
}

// BuilderTrade is one attributed fill from GET /builder/trades.
type BuilderTrade struct {
	ID              string `json:"id"`
	Builder         string `json:"builder"`
	BuilderCode     string `json:"builderCode"`
	Market          string `json:"market"`
	AssetID         string `json:"assetId"`
	Side            string `json:"side"`
	Size            string `json:"size"`
	SizeUsdc        string `json:"sizeUsdc"`
	Price           string `json:"price"`
	Status          string `json:"status"`
	Fee             string `json:"fee"`
	FeeUsdc         string `json:"feeUsdc"`
	BuilderFee      string `json:"builderFee"`
	TransactionHash string `json:"transactionHash"`
	MatchTime       string `json:"matchTime"`
}

// BuilderTradesResponse wraps a page of builder trades.
type BuilderTradesResponse struct {
	Data       []BuilderTrade `json:"data"`
	NextCursor string         `json:"next_cursor"`
	Limit      int            `json:"limit"`
	Count      int            `json:"count"`
}

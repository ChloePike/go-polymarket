// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package types

// Chain ids.
const (
	ChainPolygon = 137
	ChainAmoy    = 80002
)

// Default CLOB host.
const DefaultHost = "https://clob.polymarket.com"

// V2 CTF Exchange contracts on Polygon mainnet (verifyingContract for EIP-712).
// Pick by the market's neg-risk flag: false => Exchange, true => NegRiskExchange.
const (
	ExchangeV2Polygon        = "0xE111180000d2663C0091e4f400237545B87B996B"
	NegRiskExchangeV2Polygon = "0xe2222d279d744050d28e00520010520000310F59"
)

// EIP-712 domain for V2 order signing.
const (
	CTFExchangeDomainName    = "Polymarket CTF Exchange"
	CTFExchangeDomainVersion = "2"
)

// EIP-712 domain for L1 ClobAuth (used to create/derive API keys).
const (
	ClobAuthDomainName    = "ClobAuthDomain"
	ClobAuthDomainVersion = "1"
	ClobAuthMessage       = "This message attests that I control the given wallet"
)

// Fixed-point decimals for both collateral (USDC) and conditional tokens.
const (
	CollateralDecimals  = 6
	ConditionalDecimals = 6
)

// The canonical V2 Order EIP-712 type string (11 signed fields; taker and
// expiration are intentionally absent).
const OrderTypeString = "Order(uint256 salt,address maker,address signer," +
	"uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side," +
	"uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

// REST endpoints (paths appended to host).
const (
	EPCreateAPIKey  = "/auth/api-key"
	EPDeriveAPIKey  = "/auth/derive-api-key"
	EPPostOrder     = "/order"
	EPPostOrders    = "/orders"
	EPCancelOrder   = "/order"
	EPGetOrder      = "/data/order/"
	EPOpenOrders    = "/data/orders"
	EPBook          = "/book"
	EPTickSize      = "/tick-size"
	EPNegRisk       = "/neg-risk"
	EPFeeRate       = "/fee-rate"
	EPBuilderFees   = "/fees/builder-fees/"
	EPBuilderTrades = "/builder/trades"
	EPServerTime    = "/time"
)

// RoundConfig gives the decimal-place limits keyed by a market's tick size.
type RoundConfig struct {
	Price  int
	Size   int
	Amount int
}

// RoundingByTickSize mirrors ROUNDING_CONFIG in the official SDK.
var RoundingByTickSize = map[string]RoundConfig{
	"0.1":    {Price: 1, Size: 2, Amount: 3},
	"0.01":   {Price: 2, Size: 2, Amount: 4},
	"0.005":  {Price: 3, Size: 2, Amount: 5},
	"0.0025": {Price: 4, Size: 2, Amount: 6},
	"0.001":  {Price: 3, Size: 2, Amount: 5},
	"0.0001": {Price: 4, Size: 2, Amount: 6},
}

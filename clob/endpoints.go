// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

// Endpoint paths on the CLOB host. Paths ending in a slash take a trailing
// path element.
const (
	epOK   = "/ok"
	epTime = "/time"

	// epVersion reports the order version the exchange currently accepts, as
	// {"version":2}. It has no constant in the official SDK but the SDK calls
	// it: an order signed for a version the exchange has moved off is rejected
	// with order_version_mismatch, and the version is what tells a caller to
	// re-sign rather than retry.
	epVersion = "/version"

	epHeartbeat = "/v1/heartbeats"

	epCreateAPIKey      = "/auth/api-key"
	epGetAPIKeys        = "/auth/api-keys"
	epDeleteAPIKey      = "/auth/api-key"
	epDeriveAPIKey      = "/auth/derive-api-key"
	epClosedOnly        = "/auth/ban-status/closed-only"
	epCreateReadonlyKey = "/auth/readonly-api-key"
	epGetReadonlyKeys   = "/auth/readonly-api-keys"
	epDeleteReadonlyKey = "/auth/readonly-api-key"
	epCreateBuilderKey  = "/auth/builder-api-key"
	epGetBuilderKeys    = "/auth/builder-api-key"
	epRevokeBuilderKey  = "/auth/builder-api-key"

	epMarkets                   = "/markets"
	epMarket                    = "/markets/"
	epMarketByToken             = "/markets-by-token/"
	epClobMarket                = "/clob-markets/"
	epSimplifiedMarkets         = "/simplified-markets"
	epSamplingMarkets           = "/sampling-markets"
	epSamplingSimplifiedMarkets = "/sampling-simplified-markets"
	epMarketTradesEvents        = "/markets/live-activity/"

	epBook             = "/book"
	epBooks            = "/books"
	epMidpoint         = "/midpoint"
	epMidpoints        = "/midpoints"
	epPrice            = "/price"
	epPrices           = "/prices"
	epSpread           = "/spread"
	epSpreads          = "/spreads"
	epLastTradePrice   = "/last-trade-price"
	epLastTradesPrices = "/last-trades-prices"
	epPricesHistory    = "/prices-history"
	epTickSize         = "/tick-size"
	epNegRisk          = "/neg-risk"
	epFeeRate          = "/fee-rate"

	epPostOrder          = "/order"
	epPostOrders         = "/orders"
	epCancelOrder        = "/order"
	epCancelOrders       = "/orders"
	epCancelAll          = "/cancel-all"
	epCancelMarketOrders = "/cancel-market-orders"
	epGetOrder           = "/data/order/"
	epOpenOrders         = "/data/orders"
	epPreMigrationOrders = "/data/pre-migration-orders"
	epTrades             = "/data/trades"
	epOrderScoring       = "/order-scoring"
	epOrdersScoring      = "/orders-scoring"

	epNotifications    = "/notifications"
	epBalanceAllowance = "/balance-allowance"
	epUpdateBalance    = "/balance-allowance/update"

	epRewardsUser            = "/rewards/user"
	epRewardsUserTotal       = "/rewards/user/total"
	epRewardsUserPercentages = "/rewards/user/percentages"
	epRewardsUserMarkets     = "/rewards/user/markets"
	epRewardsMarketsCurrent  = "/rewards/markets/current"
	epRewardsMarkets         = "/rewards/markets/"

	epBuilderTrades = "/builder/trades"
	epBuilderFees   = "/fees/builder-fees/"
)

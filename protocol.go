// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

// Polymarket serves its public API from four hosts. The CLOB host carries the
// order book and everything that needs a signature; the others are read-only.
const (
	// DefaultHost is the central limit order book: markets, books, orders,
	// trades, rewards and authentication.
	DefaultHost = "https://clob.polymarket.com"

	// GammaHost serves market and event metadata: the objects behind a
	// Polymarket page, including titles, slugs, tags and resolution state.
	GammaHost = "https://gamma-api.polymarket.com"

	// DataHost serves portfolio and analytics data: positions, activity and
	// holders, keyed by wallet address.
	DataHost = "https://data-api.polymarket.com"

	// WSHost is the CLOB streaming endpoint. The market and user channels
	// hang off it; see the ws package.
	WSHost = "wss://ws-subscriptions-clob.polymarket.com/ws"
)

// Supported chains. Polymarket runs on Polygon; Amoy is its testnet.
const (
	ChainPolygon int64 = 137
	ChainAmoy    int64 = 80002
)

// Contracts holds the on-chain addresses for one chain. The exchange contracts
// double as the EIP-712 verifying contract when signing an order, so picking
// the wrong one produces a signature the exchange rejects.
type Contracts struct {
	Exchange          string // V1 exchange
	NegRiskExchange   string // V1 exchange for neg-risk markets
	ExchangeV2        string
	NegRiskExchangeV2 string
	ExchangeV3        string
	NegRiskAdapter    string
	Collateral        string // USDC
	ConditionalTokens string // ERC-1155 outcome tokens
}

var (
	polygonContracts = Contracts{
		Exchange:          "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
		NegRiskExchange:   "0xC5d563A36AE78145C45a50134d48A1215220f80a",
		ExchangeV2:        "0xE111180000d2663C0091e4f400237545B87B996B",
		NegRiskExchangeV2: "0xe2222d279d744050d28e00520010520000310F59",
		ExchangeV3:        "0xe3333700cA9d93003F00f0F71f8515005F6c00Aa",
		NegRiskAdapter:    "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
		Collateral:        "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
		ConditionalTokens: "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045",
	}
	amoyContracts = Contracts{
		Exchange:          "0xdFE02Eb6733538f8Ea35D585af8DE5958AD99E40",
		NegRiskExchange:   "0xC5d563A36AE78145C45a50134d48A1215220f80a",
		ExchangeV2:        "0xE111180000d2663C0091e4f400237545B87B996B",
		NegRiskExchangeV2: "0xe2222d279d744050d28e00520010520000310F59",
		ExchangeV3:        "0x9fE6e61422AdB6F610d8597F9684b16912D50C3D",
		NegRiskAdapter:    "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
		Collateral:        "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB",
		ConditionalTokens: "0x69308FB512518e39F9b16112fA8d994F4e2Bf8bB",
	}
)

// ContractsFor returns the addresses for a chain, and reports whether the
// chain is known.
func ContractsFor(chainID int64) (Contracts, bool) {
	switch chainID {
	case ChainPolygon:
		return polygonContracts, true
	case ChainAmoy:
		return amoyContracts, true
	}
	return Contracts{}, false
}

// EIP-712 domain for order signing. The name is shared by every exchange
// version; the version string and the verifying contract are what separate
// them, so an order signed for V2 will not verify on V3.
const (
	exchangeDomainName = "Polymarket CTF Exchange"
	exchangeV1Version  = "1"
	exchangeV2Version  = "2"
	exchangeV3Version  = "3"
)

// EIP-712 domain for level-1 authentication. It deliberately carries no
// verifying contract: the field is absent from the type string, not zeroed.
const (
	clobAuthDomainName    = "ClobAuthDomain"
	clobAuthDomainVersion = "1"
	clobAuthMessage       = "This message attests that I control the given wallet"

	clobAuthTypeString = "ClobAuth(address address,string timestamp,uint256 nonce,string message)"
)

// orderTypeString is the V2 and V3 Order struct. Exactly eleven fields are
// signed. The wire JSON also carries taker and expiration, and neither appears
// here — signing them produces a signature the exchange rejects.
const orderTypeString = "Order(uint256 salt,address maker,address signer," +
	"uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side," +
	"uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

// Fixed-point scale of USDC and of the conditional tokens alike.
const Decimals = 6

// ZeroAddress is the taker of an order open to anyone.
const ZeroAddress = "0x0000000000000000000000000000000000000000"

// ZeroBytes32 is the default metadata and builder value.
const ZeroBytes32 = "0x0000000000000000000000000000000000000000000000000000000000000000"

// Cursor sentinels for the paginated CLOB endpoints. A page request starts at
// CursorStart, and a response repeats CursorEnd once the last page is served.
// Both are base64: "MA==" is "0" and "LTE=" is "-1".
const (
	CursorStart = "MA=="
	CursorEnd   = "LTE="
)

// BuilderFeeBps is the denominator builder fee rates are quoted against, so a
// rate of 100 is one percent.
const BuilderFeeBps = 10000

// CLOB endpoint paths. Paths ending in a slash take a trailing path element.
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

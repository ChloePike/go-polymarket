// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package data

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	polymarket "github.com/ChloePike/go-polymarket"
)

// This file covers the Polymarket data API (DataHost): portfolio positions,
// on-chain activity, market holders and the various leaderboards. Every
// endpoint here is a public, unauthenticated GET — none needs a Signer or
// APICreds.
//
// The root polymarket package declares only the host addresses, not the
// per-endpoint paths, so the data API's paths are declared below, next to the
// methods that use them.
const (
	epDataHealth              = "/"
	epDataPositions           = "/positions"
	epDataClosedPositions     = "/closed-positions"
	epDataTrades              = "/trades"
	epDataActivity            = "/activity"
	epDataHolders             = "/holders"
	epDataMarketPositions     = "/v1/market-positions"
	epDataValue               = "/value"
	epDataTraded              = "/traded"
	epDataOpenInterest        = "/oi"
	epDataLiveVolume          = "/live-volume"
	epDataOther               = "/other"
	epDataLeaderboard         = "/v1/leaderboard"
	epDataBuildersLeaderboard = "/v1/builders/leaderboard"
	epDataBuildersVolume      = "/v1/builders/volume"
)

// Query-building helpers. A zero value (empty string, 0, false, nil slice)
// means "omit the parameter and let the server apply its default" throughout
// this file, matching the zero-means-default convention polymarket.OrderOptions uses.

func dataSetStr(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func dataSetInt(q url.Values, key string, val int) {
	if val != 0 {
		q.Set(key, strconv.Itoa(val))
	}
}

func dataSetInt64(q url.Values, key string, val int64) {
	if val != 0 {
		q.Set(key, strconv.FormatInt(val, 10))
	}
}

func dataSetBool(q url.Values, key string, val bool) {
	if val {
		q.Set(key, "true")
	}
}

func dataSetCSV(q url.Values, key string, vals []string) {
	if len(vals) > 0 {
		q.Set(key, strings.Join(vals, ","))
	}
}

func dataSetInt64CSV(q url.Values, key string, vals []int64) {
	if len(vals) == 0 {
		return
	}
	strs := make([]string, len(vals))
	for i, v := range vals {
		strs[i] = strconv.FormatInt(v, 10)
	}
	q.Set(key, strings.Join(strs, ","))
}

// dataHealthResponse decodes GET /.
type dataHealthResponse struct {
	Data string `json:"data"`
}

// DataHealth reports the data API's own health check. It needs no
// authentication. The name predates the package split: it disambiguates from
// the CLOB's own /ok health check, back when one Client served all four
// hosts, and is kept as-is across the move.
//
// GET /
func (c *Client) DataHealth(ctx context.Context) (string, error) {
	var out dataHealthResponse
	if err := c.session.Get(ctx, epDataHealth, nil, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// A Position is one open position a wallet holds: how many shares of an
// outcome token it owns, what it paid, and what those shares are worth now.
//
// Numeric fields arrive as JSON numbers and are kept float64: these are
// analytics readings (mark-to-market value, PnL) rather than amounts that
// flow through order construction or get signed, so the no-float64 rule in
// CLAUDE.md does not apply to them.
type Position struct {
	ProxyWallet string `json:"proxyWallet"`
	// Asset is the ERC-1155 token id of the outcome held, as a decimal string.
	Asset              string  `json:"asset"`
	ConditionID        string  `json:"conditionId"`
	Size               float64 `json:"size"`
	AvgPrice           float64 `json:"avgPrice"`
	InitialValue       float64 `json:"initialValue"`
	GrossInitialValue  float64 `json:"grossInitialValue"`
	EntryFeesUSDC      float64 `json:"entryFeesUsdc"`
	CurrentValue       float64 `json:"currentValue"`
	CashPnl            float64 `json:"cashPnl"`
	PercentPnl         float64 `json:"percentPnl"`
	TotalBought        float64 `json:"totalBought"`
	RealizedPnl        float64 `json:"realizedPnl"`
	PercentRealizedPnl float64 `json:"percentRealizedPnl"`
	CurPrice           float64 `json:"curPrice"`
	Redeemable         bool    `json:"redeemable"`
	Mergeable          bool    `json:"mergeable"`
	Title              string  `json:"title"`
	Slug               string  `json:"slug"`
	Icon               string  `json:"icon"`
	// EventID is the Gamma event id. It is a numeric-looking string on the
	// wire, not a JSON number.
	EventID         string `json:"eventId"`
	EventSlug       string `json:"eventSlug"`
	Outcome         string `json:"outcome"`
	OutcomeIndex    int    `json:"outcomeIndex"`
	OppositeOutcome string `json:"oppositeOutcome"`
	OppositeAsset   string `json:"oppositeAsset"`
	EndDate         string `json:"endDate"`
	NegativeRisk    bool   `json:"negativeRisk"`
}

// PositionsParams filters and sorts GET /positions.
type PositionsParams struct {
	// User is the proxy wallet to report positions for. Required.
	User string
	// Market restricts results to these condition ids. Mutually exclusive
	// with Event; the server rejects both being set.
	Market []string
	// Event restricts results to these Gamma event ids. Mutually exclusive
	// with Market.
	Event []int64
	// SizeThreshold hides positions smaller than this many shares. It is a
	// decimal string, not a float64: the server defaults to "1.0" when the
	// parameter is absent, and "0" (include dust) must stay distinguishable
	// from "unset" — a float64 cannot tell those apart.
	SizeThreshold string
	Redeemable    bool
	Mergeable     bool
	// Limit is 0-500; zero omits the parameter and takes the server default
	// of 100.
	Limit int
	// Offset is 0-10000.
	Offset int
	// SortBy is one of CURRENT, INITIAL, TOKENS, CASHPNL, PERCENTPNL, TITLE,
	// RESOLVING, PRICE, AVGPRICE. Empty means the server default, TOKENS.
	SortBy string
	// SortDirection is ASC or DESC. Empty means the server default, DESC.
	SortDirection string
	// Title substring-filters on the market title, case-insensitively, up to
	// 100 characters.
	Title string
}

// Positions reports a wallet's current open positions. It needs no
// authentication.
//
// GET /positions
func (c *Client) Positions(ctx context.Context, p PositionsParams) ([]Position, error) {
	q := url.Values{}
	dataSetStr(q, "user", p.User)
	dataSetCSV(q, "market", p.Market)
	dataSetInt64CSV(q, "eventId", p.Event)
	dataSetStr(q, "sizeThreshold", p.SizeThreshold)
	dataSetBool(q, "redeemable", p.Redeemable)
	dataSetBool(q, "mergeable", p.Mergeable)
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	dataSetStr(q, "sortBy", p.SortBy)
	dataSetStr(q, "sortDirection", p.SortDirection)
	dataSetStr(q, "title", p.Title)
	var out []Position
	if err := c.session.Get(ctx, epDataPositions, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A ClosedPosition is a position a wallet has fully exited or that resolved,
// with the realized profit or loss it left behind.
type ClosedPosition struct {
	ProxyWallet     string  `json:"proxyWallet"`
	Asset           string  `json:"asset"`
	ConditionID     string  `json:"conditionId"`
	AvgPrice        float64 `json:"avgPrice"`
	TotalBought     float64 `json:"totalBought"`
	RealizedPnl     float64 `json:"realizedPnl"`
	CurPrice        float64 `json:"curPrice"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Icon            string  `json:"icon"`
	EventSlug       string  `json:"eventSlug"`
	Outcome         string  `json:"outcome"`
	OutcomeIndex    int     `json:"outcomeIndex"`
	OppositeOutcome string  `json:"oppositeOutcome"`
	OppositeAsset   string  `json:"oppositeAsset"`
	EndDate         string  `json:"endDate"`
	// Timestamp is when the position closed, Unix seconds.
	Timestamp int64 `json:"timestamp"`
}

// ClosedPositionsParams filters and sorts GET /closed-positions.
type ClosedPositionsParams struct {
	// User is the proxy wallet to report closed positions for. Required.
	User string
	// Market restricts results to these condition ids. Mutually exclusive
	// with Event.
	Market []string
	// Event restricts results to these Gamma event ids. Mutually exclusive
	// with Market.
	Event []int64
	Title string
	// Limit is 0-50; zero omits the parameter and takes the server default
	// of 10.
	Limit int
	// Offset is 0-100000.
	Offset int
	// SortBy is one of REALIZEDPNL, TITLE, PRICE, AVGPRICE, TIMESTAMP. Empty
	// means the server default, REALIZEDPNL.
	SortBy string
	// SortDirection is ASC or DESC. Empty means the server default, DESC.
	SortDirection string
}

// ClosedPositions reports a wallet's fully exited or resolved positions. It
// needs no authentication.
//
// GET /closed-positions
func (c *Client) ClosedPositions(ctx context.Context, p ClosedPositionsParams) ([]ClosedPosition, error) {
	q := url.Values{}
	dataSetStr(q, "user", p.User)
	dataSetCSV(q, "market", p.Market)
	dataSetInt64CSV(q, "eventId", p.Event)
	dataSetStr(q, "title", p.Title)
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	dataSetStr(q, "sortBy", p.SortBy)
	dataSetStr(q, "sortDirection", p.SortDirection)
	var out []ClosedPosition
	if err := c.session.Get(ctx, epDataClosedPositions, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A Fill is one executed trade reported by the data API: a settled fill,
// joined server-side with the trader's public profile. Named Fill rather than
// Trade because the CLOB's own trading surface (trading.go, which owns
// epTrades = /data/trades) is expected to claim that name for its own rows,
// and "fill" is the accurate term for what this endpoint reports.
type Fill struct {
	ProxyWallet string          `json:"proxyWallet"`
	Side        polymarket.Side `json:"side"`
	Asset       string          `json:"asset"`
	ConditionID string          `json:"conditionId"`
	Size        float64         `json:"size"`
	Price       float64         `json:"price"`
	// Timestamp is Unix seconds.
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Icon      string `json:"icon"`
	EventSlug string `json:"eventSlug"`
	Outcome   string `json:"outcome"`
	// OutcomeIndex is 999 on some rows of the global (no-user) feed, as a
	// sentinel for "not applicable" rather than a real outcome index.
	OutcomeIndex          int    `json:"outcomeIndex"`
	Name                  string `json:"name"`
	Pseudonym             string `json:"pseudonym"`
	Bio                   string `json:"bio"`
	ProfileImage          string `json:"profileImage"`
	ProfileImageOptimized string `json:"profileImageOptimized"`
	TransactionHash       string `json:"transactionHash"`
}

// FillsParams filters GET /trades. Leaving User, Market and Event all empty
// requests the platform-wide feed of recent fills rather than one wallet's.
type FillsParams struct {
	// Limit is 0-10000; zero omits the parameter and takes the server
	// default of 100.
	Limit int
	// Offset is 0-10000.
	Offset int
	// IncludeMakerFills, when true, sends takerOnly=false so the feed also
	// carries maker-side fills. False (the zero value) omits the parameter
	// and takes the server default, takerOnly=true.
	IncludeMakerFills bool
	// FilterType is CASH or TOKENS; it must be set together with
	// FilterAmount or not at all.
	FilterType string
	// FilterAmount is the minimum trade size in FilterType's units, kept as
	// a decimal string rather than a float64 for the same reason as
	// PositionsParams.SizeThreshold: it is a threshold, not a signed amount,
	// but "0" and "unset" must stay distinguishable.
	FilterAmount string
	// Market restricts results to these condition ids. Mutually exclusive
	// with Event.
	Market []string
	// Event restricts results to these Gamma event ids. Mutually exclusive
	// with Market.
	Event []int64
	// User restricts results to one wallet's fills. Empty requests the
	// platform-wide feed.
	User string
	// Side is BUY or SELL. Empty means both.
	Side polymarket.Side
}

// Fills reports executed trade fills: one wallet's, or, with every filter
// left empty, the platform-wide feed of recent fills across all markets. It
// needs no authentication.
//
// GET /trades
func (c *Client) Fills(ctx context.Context, p FillsParams) ([]Fill, error) {
	q := url.Values{}
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	if p.IncludeMakerFills {
		q.Set("takerOnly", "false")
	}
	dataSetStr(q, "filterType", p.FilterType)
	dataSetStr(q, "filterAmount", p.FilterAmount)
	dataSetCSV(q, "market", p.Market)
	dataSetInt64CSV(q, "eventId", p.Event)
	dataSetStr(q, "user", p.User)
	dataSetStr(q, "side", string(p.Side))
	var out []Fill
	if err := c.session.Get(ctx, epDataTrades, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ActivityType is the kind of on-chain event one Activity row reports.
type ActivityType string

const (
	ActivityTrade       ActivityType = "TRADE"
	ActivitySplit       ActivityType = "SPLIT"
	ActivityMerge       ActivityType = "MERGE"
	ActivityRedeem      ActivityType = "REDEEM"
	ActivityReward      ActivityType = "REWARD"
	ActivityConversion  ActivityType = "CONVERSION"
	ActivityMakerRebate ActivityType = "MAKER_REBATE"
)

// An Activity row is one entry in a wallet's full on-chain activity feed: a
// superset of Fill that also carries splits, merges, redemptions, rewards and
// maker rebates.
type Activity struct {
	ProxyWallet string `json:"proxyWallet"`
	// Timestamp is Unix seconds.
	Timestamp   int64        `json:"timestamp"`
	ConditionID string       `json:"conditionId"`
	Type        ActivityType `json:"type"`
	Size        float64      `json:"size"`
	// USDCSize is the cash-equivalent size in USDC, 6-decimal precision.
	USDCSize        float64 `json:"usdcSize"`
	TransactionHash string  `json:"transactionHash"`
	// Price and Side are meaningful only when Type is ActivityTrade: on any
	// other row Price is 0 and Side is the empty string.
	Price                 float64         `json:"price"`
	Asset                 string          `json:"asset"`
	Side                  polymarket.Side `json:"side"`
	OutcomeIndex          int             `json:"outcomeIndex"`
	Title                 string          `json:"title"`
	Slug                  string          `json:"slug"`
	Icon                  string          `json:"icon"`
	EventSlug             string          `json:"eventSlug"`
	Outcome               string          `json:"outcome"`
	Name                  string          `json:"name"`
	Pseudonym             string          `json:"pseudonym"`
	Bio                   string          `json:"bio"`
	ProfileImage          string          `json:"profileImage"`
	ProfileImageOptimized string          `json:"profileImageOptimized"`
}

// ActivityParams filters and sorts GET /activity.
type ActivityParams struct {
	// User is the proxy wallet to report activity for. Required.
	User string
	// Limit is 0-500; zero omits the parameter and takes the server default
	// of 100.
	Limit int
	// Offset is 0-10000.
	Offset int
	// Market restricts results to these condition ids. Mutually exclusive
	// with Event.
	Market []string
	// Event restricts results to these Gamma event ids. Mutually exclusive
	// with Market.
	Event []int64
	// Type restricts results to these activity types. Empty means all types.
	Type []ActivityType
	// Start and End bound the feed by Unix-seconds timestamp. Zero omits the
	// bound.
	Start int64
	End   int64
	// SortBy is one of TIMESTAMP, TOKENS, CASH. Empty means the server
	// default, TIMESTAMP.
	SortBy string
	// SortDirection is ASC or DESC. Empty means the server default, DESC.
	SortDirection string
	// Side is BUY or SELL. Empty means both; meaningful only for TRADE rows.
	Side polymarket.Side
}

// Activity reports a wallet's full on-chain activity feed: trades, splits,
// merges, redemptions, rewards and maker rebates. It needs no authentication.
//
// GET /activity
func (c *Client) Activity(ctx context.Context, p ActivityParams) ([]Activity, error) {
	q := url.Values{}
	dataSetStr(q, "user", p.User)
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	dataSetCSV(q, "market", p.Market)
	dataSetInt64CSV(q, "eventId", p.Event)
	if len(p.Type) > 0 {
		types := make([]string, len(p.Type))
		for i, t := range p.Type {
			types[i] = string(t)
		}
		dataSetCSV(q, "type", types)
	}
	dataSetInt64(q, "start", p.Start)
	dataSetInt64(q, "end", p.End)
	dataSetStr(q, "sortBy", p.SortBy)
	dataSetStr(q, "sortDirection", p.SortDirection)
	dataSetStr(q, "side", string(p.Side))
	var out []Activity
	if err := c.session.Get(ctx, epDataActivity, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A Holder is one wallet's stake in a single outcome token, as reported by
// GET /holders.
type Holder struct {
	ProxyWallet           string  `json:"proxyWallet"`
	Bio                   string  `json:"bio"`
	Asset                 string  `json:"asset"`
	Pseudonym             string  `json:"pseudonym"`
	Amount                float64 `json:"amount"`
	DisplayUsernamePublic bool    `json:"displayUsernamePublic"`
	OutcomeIndex          int     `json:"outcomeIndex"`
	Name                  string  `json:"name"`
	ProfileImage          string  `json:"profileImage"`
	ProfileImageOptimized string  `json:"profileImageOptimized"`
	Verified              bool    `json:"verified"`
}

// TokenHolders is the top holders of one outcome token: GET /holders returns
// one of these per outcome token across the requested markets.
type TokenHolders struct {
	// Token is the ERC-1155 token id, as a decimal string.
	Token   string   `json:"token"`
	Holders []Holder `json:"holders"`
}

// HoldersParams filters GET /holders.
type HoldersParams struct {
	// Market is the condition ids to report holders for. Required.
	Market []string
	// Limit is 0-20, per outcome token; the server hard-caps at 20 and
	// cannot be raised. Zero omits the parameter and takes the server
	// default of 20.
	Limit int
	// MinBalance is the minimum token balance to be listed, 0-999999. Zero
	// omits the parameter and takes the server default of 1.
	MinBalance int
}

// Holders reports the top token holders for one or more markets. It needs no
// authentication.
//
// GET /holders
func (c *Client) Holders(ctx context.Context, p HoldersParams) ([]TokenHolders, error) {
	q := url.Values{}
	dataSetCSV(q, "market", p.Market)
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "minBalance", p.MinBalance)
	var out []TokenHolders
	if err := c.session.Get(ctx, epDataHolders, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A MarketPosition is one wallet's position within a single market, as
// reported by GET /v1/market-positions — the market-centric counterpart to
// Position, which is wallet-centric.
//
// Its price field is spelled CurrPrice, without the "e" that Position.CurPrice
// has. That asymmetry is the live API's own field naming, not a transcription
// slip: the two endpoints simply chose different spellings.
type MarketPosition struct {
	ProxyWallet  string  `json:"proxyWallet"`
	Name         string  `json:"name"`
	ProfileImage string  `json:"profileImage"`
	Verified     bool    `json:"verified"`
	Asset        string  `json:"asset"`
	ConditionID  string  `json:"conditionId"`
	AvgPrice     float64 `json:"avgPrice"`
	Size         float64 `json:"size"`
	CurrPrice    float64 `json:"currPrice"`
	CurrentValue float64 `json:"currentValue"`
	CashPnl      float64 `json:"cashPnl"`
	TotalBought  float64 `json:"totalBought"`
	RealizedPnl  float64 `json:"realizedPnl"`
	TotalPnl     float64 `json:"totalPnl"`
	Outcome      string  `json:"outcome"`
	OutcomeIndex int     `json:"outcomeIndex"`
}

// TokenMarketPositions groups every wallet's position in one outcome token:
// GET /v1/market-positions returns one of these per outcome token in the
// requested market.
type TokenMarketPositions struct {
	// Token is the ERC-1155 token id, as a decimal string.
	Token     string           `json:"token"`
	Positions []MarketPosition `json:"positions"`
}

// MarketPositionsParams filters and sorts GET /v1/market-positions.
type MarketPositionsParams struct {
	// Market is the single condition id to report positions for. Required.
	Market string
	// User restricts results to one wallet within the market.
	User string
	// Status is OPEN, CLOSED or ALL. OPEN means size > 0.01, CLOSED means
	// size <= 0.01. Empty means the server default, ALL.
	Status string
	// SortBy is one of TOKENS, CASH_PNL, REALIZED_PNL, TOTAL_PNL. Empty
	// means the server default, TOTAL_PNL.
	SortBy string
	// SortDirection is ASC or DESC. Empty means the server default, DESC.
	SortDirection string
	// Limit and Offset apply per outcome token. Limit is 0-500; zero omits
	// the parameter and takes the server default of 50.
	Limit  int
	Offset int
}

// MarketPositions reports every wallet's position in one market, grouped by
// outcome token. It needs no authentication.
//
// GET /v1/market-positions
func (c *Client) MarketPositions(ctx context.Context, p MarketPositionsParams) ([]TokenMarketPositions, error) {
	q := url.Values{}
	dataSetStr(q, "market", p.Market)
	dataSetStr(q, "user", p.User)
	dataSetStr(q, "status", p.Status)
	dataSetStr(q, "sortBy", p.SortBy)
	dataSetStr(q, "sortDirection", p.SortDirection)
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	var out []TokenMarketPositions
	if err := c.session.Get(ctx, epDataMarketPositions, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A PortfolioValue is the total mark-to-market USD value of a wallet's open
// positions, as reported by GET /value.
type PortfolioValue struct {
	User  string  `json:"user"`
	Value float64 `json:"value"`
}

// Value reports the total mark-to-market USD value of a wallet's open
// positions, optionally scoped to one or more markets. It needs no
// authentication.
//
// GET /value
func (c *Client) Value(ctx context.Context, user string, market []string) ([]PortfolioValue, error) {
	q := url.Values{}
	dataSetStr(q, "user", user)
	dataSetCSV(q, "market", market)
	var out []PortfolioValue
	if err := c.session.Get(ctx, epDataValue, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Traded is the count of distinct positions a wallet has ever traded, as
// reported by GET /traded.
type Traded struct {
	User   string `json:"user"`
	Traded int    `json:"traded"`
}

// TradedCount reports how many distinct positions a wallet has ever traded.
// It needs no authentication.
//
// GET /traded
func (c *Client) TradedCount(ctx context.Context, user string) (Traded, error) {
	q := url.Values{}
	dataSetStr(q, "user", user)
	var out Traded
	if err := c.session.Get(ctx, epDataTraded, q, &out); err != nil {
		return Traded{}, err
	}
	return out, nil
}

// OpenInterest is one market's open interest in USD, as reported by GET /oi.
type OpenInterest struct {
	Market string  `json:"market"`
	Value  float64 `json:"value"`
}

// OpenInterest reports open interest for one or more markets, or, with market
// left empty, for every market. It needs no authentication.
//
// GET /oi
func (c *Client) OpenInterest(ctx context.Context, market []string) ([]OpenInterest, error) {
	q := url.Values{}
	dataSetCSV(q, "market", market)
	var out []OpenInterest
	if err := c.session.Get(ctx, epDataOpenInterest, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarketVolume is one market's share of an event's live trading volume.
type MarketVolume struct {
	Market string  `json:"market"`
	Value  float64 `json:"value"`
}

// LiveVolume is an event's live trading volume, broken down per market within
// it, as reported by GET /live-volume.
type LiveVolume struct {
	Total   float64        `json:"total"`
	Markets []MarketVolume `json:"markets"`
}

// LiveVolume reports live trading volume for one event, broken down per
// market within it. The endpoint always answers with exactly one result, even
// for an id with no volume (Total 0, Markets empty) — it does not use an
// empty response to signal an unknown event id. It needs no authentication.
//
// GET /live-volume
func (c *Client) LiveVolume(ctx context.Context, eventID int64) (LiveVolume, error) {
	q := url.Values{}
	dataSetInt64(q, "id", eventID)
	var out []LiveVolume
	if err := c.session.Get(ctx, epDataLiveVolume, q, &out); err != nil {
		return LiveVolume{}, err
	}
	if len(out) == 0 {
		return LiveVolume{}, nil
	}
	return out[0], nil
}

// An OtherSize is the size of the synthetic "Other" outcome bucket a wallet
// holds in an augmented negative-risk event — a Polymarket construct for
// neg-risk markets with a residual catch-all outcome. This endpoint is marked
// internal in the OpenAPI spec (excluded from the public docs nav) but is
// live and answers real requests.
type OtherSize struct {
	ID   int64   `json:"id"`
	User string  `json:"user"`
	Size float64 `json:"size"`
}

// OtherSizes reports the size of the "Other" outcome bucket a wallet holds in
// one augmented negative-risk event. It needs no authentication.
//
// GET /other
func (c *Client) OtherSizes(ctx context.Context, eventID int64, user string) ([]OtherSize, error) {
	q := url.Values{}
	dataSetInt64(q, "id", eventID)
	dataSetStr(q, "user", user)
	var out []OtherSize
	if err := c.session.Get(ctx, epDataOther, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LeaderboardCategory scopes a leaderboard query to one market category.
type LeaderboardCategory string

const (
	LeaderboardCategoryOverall   LeaderboardCategory = "OVERALL"
	LeaderboardCategoryPolitics  LeaderboardCategory = "POLITICS"
	LeaderboardCategorySports    LeaderboardCategory = "SPORTS"
	LeaderboardCategoryCrypto    LeaderboardCategory = "CRYPTO"
	LeaderboardCategoryCulture   LeaderboardCategory = "CULTURE"
	LeaderboardCategoryMentions  LeaderboardCategory = "MENTIONS"
	LeaderboardCategoryWeather   LeaderboardCategory = "WEATHER"
	LeaderboardCategoryEconomics LeaderboardCategory = "ECONOMICS"
	LeaderboardCategoryTech      LeaderboardCategory = "TECH"
	LeaderboardCategoryFinance   LeaderboardCategory = "FINANCE"
)

// LeaderboardPeriod windows a leaderboard or builder-volume query. It is
// shared by GET /v1/leaderboard, GET /v1/builders/leaderboard and
// GET /v1/builders/volume, which all use the same four windows.
type LeaderboardPeriod string

const (
	LeaderboardPeriodDay   LeaderboardPeriod = "DAY"
	LeaderboardPeriodWeek  LeaderboardPeriod = "WEEK"
	LeaderboardPeriodMonth LeaderboardPeriod = "MONTH"
	LeaderboardPeriodAll   LeaderboardPeriod = "ALL"
)

// LeaderboardOrderBy ranks GET /v1/leaderboard by PnL or by volume.
type LeaderboardOrderBy string

const (
	LeaderboardOrderByPnl LeaderboardOrderBy = "PNL"
	LeaderboardOrderByVol LeaderboardOrderBy = "VOL"
)

// A TraderLeaderboardEntry is one trader's rank on GET /v1/leaderboard.
type TraderLeaderboardEntry struct {
	// Rank is a decimal string, such as "1", not a JSON number.
	Rank          string  `json:"rank"`
	ProxyWallet   string  `json:"proxyWallet"`
	UserName      string  `json:"userName"`
	XUsername     string  `json:"xUsername"`
	VerifiedBadge bool    `json:"verifiedBadge"`
	Vol           float64 `json:"vol"`
	Pnl           float64 `json:"pnl"`
	ProfileImage  string  `json:"profileImage"`
}

// LeaderboardParams filters GET /v1/leaderboard.
type LeaderboardParams struct {
	// Category scopes the ranking to one market category. Empty means the
	// server default, LeaderboardCategoryOverall.
	Category LeaderboardCategory
	// TimePeriod windows the ranking. Empty means the server default,
	// LeaderboardPeriodDay.
	TimePeriod LeaderboardPeriod
	// OrderBy ranks by PnL or volume. Empty means the server default,
	// LeaderboardOrderByPnl.
	OrderBy LeaderboardOrderBy
	// Limit is 1-50. Zero omits the parameter and takes the server default
	// of 25.
	Limit int
	// Offset is 0-1000.
	Offset int
	// User restricts the result to one wallet's rank.
	User string
	// UserName restricts the result to one username's rank.
	UserName string
}

// Leaderboard reports the trader PnL/volume leaderboard. It needs no
// authentication.
//
// GET /v1/leaderboard
func (c *Client) Leaderboard(ctx context.Context, p LeaderboardParams) ([]TraderLeaderboardEntry, error) {
	q := url.Values{}
	dataSetStr(q, "category", string(p.Category))
	dataSetStr(q, "timePeriod", string(p.TimePeriod))
	dataSetStr(q, "orderBy", string(p.OrderBy))
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	dataSetStr(q, "user", p.User)
	dataSetStr(q, "userName", p.UserName)
	var out []TraderLeaderboardEntry
	if err := c.session.Get(ctx, epDataLeaderboard, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A BuilderLeaderboardEntry is one builder code's rank on
// GET /v1/builders/leaderboard, from Polymarket's Builder Program: front-end
// integrations bake a builder code into the orders they submit, and this
// ranks them by the volume attributed to that code.
type BuilderLeaderboardEntry struct {
	// Rank is a decimal string, such as "1", not a JSON number.
	Rank    string `json:"rank"`
	Builder string `json:"builder"`
	// BuilderCode is the onchain builder identifier, 0x + 64 hex.
	BuilderCode string  `json:"builderCode"`
	Volume      float64 `json:"volume"`
	ActiveUsers int     `json:"activeUsers"`
	Verified    bool    `json:"verified"`
	BuilderLogo string  `json:"builderLogo"`
}

// BuilderLeaderboardParams filters GET /v1/builders/leaderboard.
type BuilderLeaderboardParams struct {
	// TimePeriod windows the ranking. Empty means the server default,
	// LeaderboardPeriodDay.
	TimePeriod LeaderboardPeriod
	// Limit is 0-50; zero omits the parameter and takes the server default
	// of 25.
	Limit int
	// Offset is 0-1000.
	Offset int
}

// BuilderLeaderboard reports the builder-code volume leaderboard. It needs no
// authentication.
//
// GET /v1/builders/leaderboard
func (c *Client) BuilderLeaderboard(ctx context.Context, p BuilderLeaderboardParams) ([]BuilderLeaderboardEntry, error) {
	q := url.Values{}
	dataSetStr(q, "timePeriod", string(p.TimePeriod))
	dataSetInt(q, "limit", p.Limit)
	dataSetInt(q, "offset", p.Offset)
	var out []BuilderLeaderboardEntry
	if err := c.session.Get(ctx, epDataBuildersLeaderboard, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// A BuilderVolumeEntry is one builder code's attributed volume for one day,
// as reported by GET /v1/builders/volume.
type BuilderVolumeEntry struct {
	// Date is the day this row covers, RFC3339 UTC (the wire key is "dt").
	Date        string  `json:"dt"`
	Builder     string  `json:"builder"`
	BuilderCode string  `json:"builderCode"`
	BuilderLogo string  `json:"builderLogo"`
	Verified    bool    `json:"verified"`
	Volume      float64 `json:"volume"`
	ActiveUsers int     `json:"activeUsers"`
	// Rank is a decimal string, such as "1", not a JSON number.
	Rank string `json:"rank"`
}

// BuilderVolume reports the daily time series of builder-code volume: one row
// per builder per day in the window, with no pagination. It needs no
// authentication.
//
// GET /v1/builders/volume
func (c *Client) BuilderVolume(ctx context.Context, period LeaderboardPeriod) ([]BuilderVolumeEntry, error) {
	q := url.Values{}
	dataSetStr(q, "timePeriod", string(period))
	var out []BuilderVolumeEntry
	if err := c.session.Get(ctx, epDataBuildersVolume, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

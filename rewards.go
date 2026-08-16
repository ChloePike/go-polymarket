// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// Liquidity rewards pay market makers for resting orders close to the
// midpoint. Two of the six endpoints here — CurrentRewardsMarkets and
// RewardsMarkets — describe the reward program itself and need no
// authentication; the other four report one account's earnings in it and
// need level-2 credentials, scoped by the API key's maker address.
//
// The four user-scoped types (RewardsUserEarning, RewardsUserTotalEarning,
// RewardsPercentages, RewardsUserMarketEarning) are transcribed from the
// official SDK's TypeScript interfaces and have not been checked against a
// live response: exercising them needs level-2 credentials this client has
// not run against the live API. The two market-scoped types
// (RewardsMarket, RewardsMarketDetail) are live-observed instead, and both
// disagree with the SDK's declared (and apparently stale) MarketReward
// interface — see their doc comments.
//
// Every fractional or money-shaped field below is json.Number, never
// float64: the wire sends both integers (rewards_min_size as 20) and
// decimals (rewards_max_spread as 4.5) for what are really the same kind of
// field, and json.Number preserves either without rounding.
//
// Paging below calls cursorOrStart, a package-level helper declared once (in
// market.go) and shared with builder.go's BuilderTrades, not redeclared
// here: it resolves a caller's cursor to what the wire actually sends, since
// the official SDK always sends next_cursor, seeded with CursorStart on the
// first page, rather than omitting it.

// setSignatureType adds signature_type to a rewards query. Every user-scoped
// rewards endpoint takes it: the same wallet trades under different maker
// addresses as a plain EOA versus a Polymarket proxy or Gnosis Safe, so
// earnings are scoped by which one signed.
func setSignatureType(q url.Values, t SignatureType) {
	q.Set("signature_type", strconv.Itoa(int(t)))
}

// RewardsToken is one outcome token as the rewards endpoints describe it:
// live-observed on GET /rewards/markets/{condition_id}.
type RewardsToken struct {
	TokenID string      `json:"token_id"`
	Outcome string      `json:"outcome"`
	Price   json.Number `json:"price"`
}

// RewardsConfig is one asset's reward schedule for a market: the rate paid
// per day and the window it runs over. Live-observed on both
// GET /rewards/markets/current and GET /rewards/markets/{condition_id};
// TotalDays was seen only on the latter, so it may be genuinely absent from
// the former rather than merely omitted-when-zero.
type RewardsConfig struct {
	ID           int         `json:"id"`
	AssetAddress string      `json:"asset_address"`
	StartDate    string      `json:"start_date"` // "YYYY-MM-DD"
	EndDate      string      `json:"end_date"`   // "YYYY-MM-DD"
	RatePerDay   json.Number `json:"rate_per_day"`
	TotalRewards json.Number `json:"total_rewards"`
	TotalDays    int         `json:"total_days,omitempty"`
}

// RewardsEarning is one asset's contribution to a RewardsUserMarketEarning
// row. Transcribed from the SDK's Earning interface; not observed live.
type RewardsEarning struct {
	AssetAddress string      `json:"asset_address"`
	Earnings     json.Number `json:"earnings"`
	AssetRate    json.Number `json:"asset_rate"`
}

// RewardsUserEarning is one row of GET /rewards/user: a user's
// liquidity-reward earnings in one market for one UTC day. Transcribed from
// the SDK's UserEarning interface; not observed live.
type RewardsUserEarning struct {
	Date         string      `json:"date"` // "YYYY-MM-DD"
	ConditionID  string      `json:"condition_id"`
	AssetAddress string      `json:"asset_address"`
	MakerAddress string      `json:"maker_address"`
	Earnings     json.Number `json:"earnings"`
	AssetRate    json.Number `json:"asset_rate"`
}

// rewardsUserPage is the pagination envelope GET /rewards/user returns.
type rewardsUserPage struct {
	Data []RewardsUserEarning `json:"data"`
	Pagination
}

// RewardsUserTotalEarning is one row of GET /rewards/user/total: a user's
// total liquidity-reward earnings across every market for one UTC day.
// Transcribed from the SDK's TotalUserEarning interface; not observed live.
type RewardsUserTotalEarning struct {
	Date         string      `json:"date"` // "YYYY-MM-DD"
	AssetAddress string      `json:"asset_address"`
	MakerAddress string      `json:"maker_address"`
	Earnings     json.Number `json:"earnings"`
	AssetRate    json.Number `json:"asset_rate"`
}

// RewardsPercentages maps a market's condition id to the caller's share of
// that market's liquidity rewards, as GET /rewards/user/percentages returns
// it. Transcribed from the SDK's RewardsPercentages interface; not observed
// live.
type RewardsPercentages map[string]json.Number

// RewardsUserMarketEarning is one row of GET /rewards/user/markets: one
// market's reward configuration together with the caller's share and
// earnings in it for one UTC day. Transcribed from the SDK's
// UserRewardsEarning interface; not observed live.
type RewardsUserMarketEarning struct {
	ConditionID           string           `json:"condition_id"`
	Question              string           `json:"question"`
	MarketSlug            string           `json:"market_slug"`
	EventSlug             string           `json:"event_slug"`
	Image                 string           `json:"image"`
	RewardsMaxSpread      json.Number      `json:"rewards_max_spread"`
	RewardsMinSize        json.Number      `json:"rewards_min_size"`
	MarketCompetitiveness json.Number      `json:"market_competitiveness"`
	Tokens                []RewardsToken   `json:"tokens"`
	RewardsConfig         []RewardsConfig  `json:"rewards_config"`
	MakerAddress          string           `json:"maker_address"`
	EarningPercentage     json.Number      `json:"earning_percentage"`
	Earnings              []RewardsEarning `json:"earnings"`
}

// rewardsUserMarketsPage is the pagination envelope
// GET /rewards/user/markets returns.
type rewardsUserMarketsPage struct {
	Data []RewardsUserMarketEarning `json:"data"`
	Pagination
}

// RewardsMarket is one row of GET /rewards/markets/current: a market
// currently earning liquidity rewards, with its reward schedule and combined
// daily rate. Live-observed. It does not match the SDK's declared
// MarketReward interface: the live response has no question, market_slug,
// event_slug, image or tokens, and carries two fields the interface lacks,
// NativeDailyRate and TotalDailyRate.
type RewardsMarket struct {
	ConditionID      string          `json:"condition_id"`
	RewardsConfig    []RewardsConfig `json:"rewards_config"`
	RewardsMaxSpread json.Number     `json:"rewards_max_spread"`
	RewardsMinSize   json.Number     `json:"rewards_min_size"`
	NativeDailyRate  json.Number     `json:"native_daily_rate"`
	TotalDailyRate   json.Number     `json:"total_daily_rate"`
}

// rewardsMarketsCurrentPage is the pagination envelope
// GET /rewards/markets/current returns.
type rewardsMarketsCurrentPage struct {
	Data []RewardsMarket `json:"data"`
	Pagination
}

// RewardsMarketDetail is one row of GET /rewards/markets/{condition_id}: the
// full reward configuration for a single market. Live-observed. It shares
// no distinguishing shape with either the SDK's declared MarketReward
// interface or with RewardsMarket, the type /rewards/markets/current
// actually returns: the two endpoints under the epRewardsMarkets* path
// prefix do not share a response shape, despite both wrapping it in the same
// {data, next_cursor, limit, count} envelope.
type RewardsMarketDetail struct {
	ConditionID           string          `json:"condition_id"`
	Question              string          `json:"question"`
	MarketSlug            string          `json:"market_slug"`
	EventSlug             string          `json:"event_slug"`
	Image                 string          `json:"image"`
	Tokens                []RewardsToken  `json:"tokens"`
	RewardsConfig         []RewardsConfig `json:"rewards_config"`
	RewardsMaxSpread      json.Number     `json:"rewards_max_spread"`
	RewardsMinSize        json.Number     `json:"rewards_min_size"`
	MarketCompetitiveness json.Number     `json:"market_competitiveness"`
}

// rewardsMarketsPage is the pagination envelope
// GET /rewards/markets/{condition_id} returns.
type rewardsMarketsPage struct {
	Data []RewardsMarketDetail `json:"data"`
	Pagination
}

// UserRewards returns one page of a user's liquidity-reward earnings, broken
// out by market, for one UTC day. It needs level-2 credentials: the
// caller's own API key determines whose earnings come back.
//
// date is "YYYY-MM-DD", the format every date the rewards API itself emits
// on the wire (live-observed in RewardsConfig.StartDate and EndDate).
// sigType picks which of the account's maker addresses to report on, as
// described on setSignatureType. cursor pages the result; pass "" for the
// first page and keep requesting with the returned Pagination.NextCursor
// until it equals CursorEnd.
func (c *Client) UserRewards(ctx context.Context, date string, sigType SignatureType, cursor string) ([]RewardsUserEarning, Pagination, error) {
	q := url.Values{}
	q.Set("date", date)
	setSignatureType(q, sigType)
	q.Set("next_cursor", cursorOrStart(cursor))

	var page rewardsUserPage
	if err := c.getL2(ctx, epRewardsUser, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// UserRewardsTotal returns a user's total liquidity-reward earnings across
// every market for one UTC day. It needs level-2 credentials. Unlike
// UserRewards the response is not paginated: the API returns the whole
// result in one call.
//
// date and sigType are as in UserRewards.
func (c *Client) UserRewardsTotal(ctx context.Context, date string, sigType SignatureType) ([]RewardsUserTotalEarning, error) {
	q := url.Values{}
	q.Set("date", date)
	setSignatureType(q, sigType)

	var out []RewardsUserTotalEarning
	if err := c.getL2(ctx, epRewardsUserTotal, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UserRewardsPercentages returns the caller's liquidity-reward percentage in
// every market it holds one, keyed by condition id. It needs level-2
// credentials.
//
// sigType is as in UserRewards.
func (c *Client) UserRewardsPercentages(ctx context.Context, sigType SignatureType) (RewardsPercentages, error) {
	q := url.Values{}
	setSignatureType(q, sigType)

	var out RewardsPercentages
	if err := c.getL2(ctx, epRewardsUserPercentages, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RewardsMarketsParams filters GET /rewards/user/markets, the per-market
// breakdown of a user's liquidity-reward earnings for one UTC day.
type RewardsMarketsParams struct {
	// Date is the UTC day to report, "YYYY-MM-DD".
	Date string

	// SignatureType picks which of the account's maker addresses to report
	// on, as described on setSignatureType.
	SignatureType SignatureType

	// OrderBy sorts the result. The API defines the accepted values; this
	// client passes the string through unvalidated. Empty omits the
	// parameter.
	OrderBy string

	// Position filters to one side of a market. The API defines the accepted
	// values; this client passes the string through unvalidated. Empty
	// omits the parameter.
	Position string

	// NoCompetition excludes markets with no reward competition when true.
	NoCompetition bool

	// Cursor pages the result; empty starts at the first page.
	Cursor string
}

// UserRewardsMarkets returns one page of a user's per-market
// liquidity-reward breakdown for p.Date, filtered and ordered by p. It needs
// level-2 credentials.
func (c *Client) UserRewardsMarkets(ctx context.Context, p RewardsMarketsParams) ([]RewardsUserMarketEarning, Pagination, error) {
	q := url.Values{}
	q.Set("date", p.Date)
	setSignatureType(q, p.SignatureType)
	q.Set("next_cursor", cursorOrStart(p.Cursor))
	if p.OrderBy != "" {
		q.Set("order_by", p.OrderBy)
	}
	if p.Position != "" {
		q.Set("position", p.Position)
	}
	if p.NoCompetition {
		q.Set("no_competition", "true")
	}

	var page rewardsUserMarketsPage
	if err := c.getL2(ctx, epRewardsUserMarkets, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// CurrentRewardsMarkets returns one page of the markets currently earning
// liquidity rewards, with each market's reward schedule and combined daily
// rate. It needs no authentication: the program's configuration is public.
//
// cursor pages the result; pass "" for the first page and keep requesting
// with the returned Pagination.NextCursor until it equals CursorEnd.
func (c *Client) CurrentRewardsMarkets(ctx context.Context, cursor string) ([]RewardsMarket, Pagination, error) {
	q := url.Values{}
	q.Set("next_cursor", cursorOrStart(cursor))

	var page rewardsMarketsCurrentPage
	if err := c.get(ctx, epRewardsMarketsCurrent, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// RewardsMarkets returns one page of the raw liquidity-reward configuration
// for a single market, named by its condition id. It needs no
// authentication.
//
// cursor pages the result as in CurrentRewardsMarkets. The rows this
// returns are shaped differently from CurrentRewardsMarkets's — see
// RewardsMarketDetail.
func (c *Client) RewardsMarkets(ctx context.Context, conditionID, cursor string) ([]RewardsMarketDetail, Pagination, error) {
	q := url.Values{}
	q.Set("next_cursor", cursorOrStart(cursor))

	var page rewardsMarketsPage
	if err := c.get(ctx, epRewardsMarkets+conditionID, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

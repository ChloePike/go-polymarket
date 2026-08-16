// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

// This file implements the CLOB's read-only market-data endpoints: service
// health, market and token metadata, order books, prices in their various
// forms, and the marketable-price walk that feeds MarketOrder.Price. None of
// it needs authentication, so the zero Client is enough to call any method
// here.
//
// Every fractional or money-shaped field below is json.Number, never
// float64, matching the convention rewards.go also follows: the wire sends
// both integers and decimals for what are really the same kind of field
// (a market's minimum_tick_size arrives as 0.01 or as 5, say), and
// json.Number preserves either exactly. The one field observed to change
// shape entirely — GET /tick-size's minimum_tick_size arrives as a bare
// number in most deployments and as a JSON string in others — needs
// looseNumber instead, since json.Number itself only accepts a JSON number
// token. ClobMarketFee's Rate and Exponent stay float64: they are the
// fractional-exponent pre-trade fee estimate CLAUDE.md's no-float64 rule
// exempts, the same figures fees.go's fee model consumes.

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	polymarket "github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/internal/amount"
)

// Pagination is the cursor metadata every cursor-paginated CLOB list
// endpoint returns alongside its data. Each paginated method in this
// package decodes into its own private envelope embedding Pagination next
// to a Data field typed for that endpoint's rows — see e.g. marketsPage
// below, or builder.go's builderTradesPage — and returns the rows and the
// Pagination separately.
type Pagination struct {
	Limit      int    `json:"limit"`
	Count      int    `json:"count"`
	NextCursor string `json:"next_cursor"`
}

// cursorOrStart resolves a caller's cursor to what the wire actually sends:
// the official SDK always sends next_cursor, seeded with CursorStart on the
// first page, rather than omitting it. Every paginated method in this
// package calls this, declared once here, to turn a caller's "" (first
// page) into CursorStart.
func cursorOrStart(cursor string) string {
	if cursor == "" {
		return polymarket.CursorStart
	}
	return cursor
}

// Pages returns an iterator over every item a cursor-paginated endpoint
// serves, flattening as many pages as get produces. get performs one page's
// request for a cursor, matching the (rows, Pagination, error) shape this
// package's paginated methods return — Markets below, or builder.go's
// BuilderTrades and rewards.go's UserRewards through a closure that supplies
// their other arguments. Pass "" as the first cursor. Pages stops once a
// page's NextCursor repeats CursorEnd or stops advancing; if get returns an
// error, Pages yields it once, with a zero T, and stops.
//
//	for m, err := range polymarket.Pages(ctx, c.Markets) {
//		if err != nil { ... }
//	}
func Pages[T any](ctx context.Context, get func(ctx context.Context, cursor string) ([]T, Pagination, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := ""
		for {
			items, page, err := get(ctx, cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if page.NextCursor == polymarket.CursorEnd || page.NextCursor == "" || page.NextCursor == cursor {
				return
			}
			cursor = page.NextCursor
		}
	}
}

// Ping checks that the CLOB is reachable: no authentication, GET /ok. The
// endpoint returns the literal JSON string "OK" when the service is
// healthy; Ping reports any decode failure or non-2xx status as an error
// and otherwise returns nil.
func (c *Client) Ping(ctx context.Context) error {
	var out string
	return c.session.Get(ctx, epOK, nil, &out)
}

// Time returns the CLOB server's clock: no authentication, GET /time, which
// answers with a bare unix-seconds number. A caller with clock skew can
// align a request's timestamp — an order's Timestamp, or a GTD order's
// Expiration — against this rather than its own clock.
func (c *Client) Time(ctx context.Context) (int64, error) {
	var sec int64
	err := c.session.Get(ctx, epTime, nil, &sec)
	return sec, err
}

// A MarketToken is one outcome of a market: the ERC-1155 id that trades on
// the CLOB, its label, and the CLOB's own last-seen price for it.
type MarketToken struct {
	TokenID string      `json:"token_id"`
	Outcome string      `json:"outcome"`
	Price   json.Number `json:"price"`
	Winner  bool        `json:"winner"`
}

// A MarketRewardRate is one asset's daily liquidity-reward rate for a
// market.
type MarketRewardRate struct {
	AssetAddress     string      `json:"asset_address"`
	RewardsDailyRate json.Number `json:"rewards_daily_rate"`
}

// MarketRewards describes a market's liquidity-reward program: which assets
// pay out, and the spread and size a resting order must keep to qualify.
// Rates is nil when no reward program is configured.
type MarketRewards struct {
	Rates     []MarketRewardRate `json:"rates"`
	MinSize   json.Number        `json:"min_size"`
	MaxSpread json.Number        `json:"max_spread"`
}

// A Market is a CLOB market: one binary question with two MarketTokens, its
// trading state, and its reward configuration. It is the response shape of
// Markets, Market and SamplingMarkets alike.
type Market struct {
	EnableOrderBook         bool          `json:"enable_order_book"`
	Active                  bool          `json:"active"`
	Closed                  bool          `json:"closed"`
	Archived                bool          `json:"archived"`
	AcceptingOrders         bool          `json:"accepting_orders"`
	AcceptingOrderTimestamp string        `json:"accepting_order_timestamp"`
	MinimumOrderSize        json.Number   `json:"minimum_order_size"`
	MinimumTickSize         json.Number   `json:"minimum_tick_size"`
	ConditionID             string        `json:"condition_id"`
	QuestionID              string        `json:"question_id"`
	Question                string        `json:"question"`
	Description             string        `json:"description"`
	MarketSlug              string        `json:"market_slug"`
	EndDateISO              string        `json:"end_date_iso"`
	GameStartTime           string        `json:"game_start_time"`
	SecondsDelay            int           `json:"seconds_delay"`
	FPMM                    string        `json:"fpmm"`
	MakerBaseFee            int           `json:"maker_base_fee"`
	TakerBaseFee            int           `json:"taker_base_fee"`
	NotificationsEnabled    bool          `json:"notifications_enabled"`
	NegRisk                 bool          `json:"neg_risk"`
	NegRiskMarketID         string        `json:"neg_risk_market_id"`
	NegRiskRequestID        string        `json:"neg_risk_request_id"`
	Icon                    string        `json:"icon"`
	Image                   string        `json:"image"`
	Rewards                 MarketRewards `json:"rewards"`
	Is5050Outcome           bool          `json:"is_50_50_outcome"`
	Tokens                  []MarketToken `json:"tokens"`
	Tags                    []string      `json:"tags"`
}

// marketsPage is the pagination envelope GET /markets and GET
// /sampling-markets share.
type marketsPage struct {
	Data []Market `json:"data"`
	Pagination
}

// A SimplifiedMarket is the reduced form of a Market that SimplifiedMarkets
// and SamplingSimplifiedMarkets serve: trading state and reward
// configuration without the descriptive fields.
type SimplifiedMarket struct {
	ConditionID     string        `json:"condition_id"`
	Rewards         MarketRewards `json:"rewards"`
	Tokens          []MarketToken `json:"tokens"`
	Active          bool          `json:"active"`
	Closed          bool          `json:"closed"`
	Archived        bool          `json:"archived"`
	AcceptingOrders bool          `json:"accepting_orders"`
}

// simplifiedMarketsPage is the pagination envelope GET /simplified-markets
// and GET /sampling-simplified-markets both return.
type simplifiedMarketsPage struct {
	Data []SimplifiedMarket `json:"data"`
	Pagination
}

// Markets lists every CLOB market, one cursor page at a time: no
// authentication, GET /markets. Pass "" for the first page and keep
// requesting with the returned Pagination.NextCursor until it equals
// CursorEnd, or range over Pages(ctx, c.Markets) to walk every page.
func (c *Client) Markets(ctx context.Context, cursor string) ([]Market, Pagination, error) {
	q := url.Values{"next_cursor": {cursorOrStart(cursor)}}
	var page marketsPage
	if err := c.session.Get(ctx, epMarkets, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// Market fetches one market by its condition id: no authentication, GET
// /markets/{conditionID}.
func (c *Client) Market(ctx context.Context, conditionID string) (Market, error) {
	var out Market
	err := c.session.Get(ctx, epMarket+conditionID, nil, &out)
	return out, err
}

// SimplifiedMarkets lists every CLOB market in its reduced form, one cursor
// page at a time: no authentication, GET /simplified-markets. Paging works
// as described on Markets.
func (c *Client) SimplifiedMarkets(ctx context.Context, cursor string) ([]SimplifiedMarket, Pagination, error) {
	q := url.Values{"next_cursor": {cursorOrStart(cursor)}}
	var page simplifiedMarketsPage
	if err := c.session.Get(ctx, epSimplifiedMarkets, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// SamplingMarkets lists the markets the CLOB is currently sampling order
// books for, one cursor page at a time: no authentication, GET
// /sampling-markets. Paging works as described on Markets.
func (c *Client) SamplingMarkets(ctx context.Context, cursor string) ([]Market, Pagination, error) {
	q := url.Values{"next_cursor": {cursorOrStart(cursor)}}
	var page marketsPage
	if err := c.session.Get(ctx, epSamplingMarkets, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// SamplingSimplifiedMarkets lists the same markets as SamplingMarkets in
// their reduced form, one cursor page at a time: no authentication, GET
// /sampling-simplified-markets. Paging works as described on Markets.
func (c *Client) SamplingSimplifiedMarkets(ctx context.Context, cursor string) ([]SimplifiedMarket, Pagination, error) {
	q := url.Values{"next_cursor": {cursorOrStart(cursor)}}
	var page simplifiedMarketsPage
	if err := c.session.Get(ctx, epSamplingSimplifiedMarkets, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// A MarketByToken names the market a token belongs to, and its sibling
// token on the other side of the same binary question.
type MarketByToken struct {
	ConditionID      string `json:"condition_id"`
	PrimaryTokenID   string `json:"primary_token_id"`
	SecondaryTokenID string `json:"secondary_token_id"`
}

// MarketByToken looks up the market a token id belongs to: no
// authentication, GET /markets-by-token/{tokenID}.
func (c *Client) MarketByToken(ctx context.Context, tokenID string) (MarketByToken, error) {
	var out MarketByToken
	err := c.session.Get(ctx, epMarketByToken+tokenID, nil, &out)
	return out, err
}

// A ClobMarketToken is one outcome as GET /clob-markets/{conditionID}
// reports it: just the id and label, without price or winner state.
type ClobMarketToken struct {
	TokenID string `json:"t"`
	Outcome string `json:"o"`
}

// ClobMarketFee is a market's platform-fee curve: the fee a marketable buy
// pays is feeRate × (price × (1 − price)) ^ feeExponent, so Rate and
// Exponent are advisory inputs to that estimate rather than traded amounts —
// the fractional-exponent case CLAUDE.md's no-float64 rule exempts, and the
// same figures fees.go's fee model consumes as FeeRate and FeeExponent.
type ClobMarketFee struct {
	Rate     float64 `json:"r"`
	Exponent float64 `json:"e"`
	// TakerOnly's meaning is not confirmed against docs or a differing live
	// value; kept for completeness under its wire name "to".
	TakerOnly bool `json:"to"`
}

// ClobMarketRewards is a market's reward configuration in the compact form
// GET /clob-markets/{conditionID} reports it. MinSize and MaxSpread line up
// with MarketRewards' min_size and max_spread on the verbose Market
// response; SingleMarketOfferAmount and MOAS are unconfirmed guesses — see
// ClobMarket's doc comment.
type ClobMarketRewards struct {
	MinSize   json.Number `json:"mi"`
	MaxSpread json.Number `json:"ma"`
	Enabled   bool        `json:"e"`
	// SingleMarketOfferAmount is an unconfirmed guess at "smoa"'s meaning: it
	// was never observed with a non-null value live.
	SingleMarketOfferAmount bool `json:"smoa"`
	// MOAS has no confirmed meaning or name; kept under its wire key.
	MOAS json.Number `json:"moas"`
}

// A ClobMarket is the compact market summary GET /clob-markets/{conditionID}
// serves, keyed by abbreviated field names distinct from Market's. C, T,
// TickSize, NegRisk, MakerBaseFee, TakerBaseFee, MinOrderSize,
// AcceptingOrders and AcceptingOrderTimestamp are confirmed against the
// matching fields on Market for the same condition id; SecondsDelay,
// GameStartTime, ClearBookOnStart, RFQEnabled and
// InstantTradeOnDeployEnabled are this package's best-effort guess at what
// their one- or two-letter wire keys ("sd", "gst", "cbos", "rfqe", "itode")
// mean and are not otherwise confirmed. "ibce" has no guessed meaning at all
// and is omitted.
type ClobMarket struct {
	ConditionID                 string             `json:"c"`
	Tokens                      []ClobMarketToken  `json:"t"`
	TickSize                    json.Number        `json:"mts"`
	NegRisk                     bool               `json:"nr"`
	Fee                         *ClobMarketFee     `json:"fd"`
	MakerBaseFee                int                `json:"mbf"`
	TakerBaseFee                int                `json:"tbf"`
	Rewards                     *ClobMarketRewards `json:"r"`
	AcceptingOrders             bool               `json:"ao"`
	MinOrderSize                json.Number        `json:"mos"`
	SecondsDelay                int                `json:"sd"`
	GameStartTime               string             `json:"gst"`
	ClearBookOnStart            bool               `json:"cbos"`
	AcceptingOrderTimestamp     string             `json:"aot"`
	RFQEnabled                  bool               `json:"rfqe"`
	InstantTradeOnDeployEnabled bool               `json:"itode"`
}

// ClobMarket fetches the compact market summary GET /clob-markets/{condition
// ID} serves: no authentication. It carries the fee curve (ClobMarketFee)
// that fees.go's fee model needs, in a shape distinct from Market's.
func (c *Client) ClobMarket(ctx context.Context, conditionID string) (ClobMarket, error) {
	var out ClobMarket
	err := c.session.Get(ctx, epClobMarket+conditionID, nil, &out)
	return out, err
}

// MarketLiveActivity is a market's live-activity summary. The official TS
// SDK's clob.d.ts declares GET /markets/live-activity/{conditionID} as
// returning MarketTradeEvent[], an array of individual fills; live testing
// against markets from no activity to the platform's highest 24-hour volume
// consistently returned this single metadata object instead, with none of
// MarketTradeEvent's fields present. This type matches what the endpoint
// actually sends.
type MarketLiveActivity struct {
	ConditionID string   `json:"condition_id"`
	ID          int64    `json:"id"`
	Question    string   `json:"question"`
	MarketSlug  string   `json:"market_slug"`
	EventSlug   string   `json:"event_slug"`
	SeriesSlug  string   `json:"series_slug"`
	Icon        string   `json:"icon"`
	Image       string   `json:"image"`
	Tags        []string `json:"tags"`
}

// MarketTradesEvents fetches a market's live-activity summary: no
// authentication, GET /markets/live-activity/{conditionID}. See
// MarketLiveActivity for how this differs from the SDK's declared type.
func (c *Client) MarketTradesEvents(ctx context.Context, conditionID string) (MarketLiveActivity, error) {
	var out MarketLiveActivity
	err := c.session.Get(ctx, epMarketTradesEvents+conditionID, nil, &out)
	return out, err
}

// An OrderSummary is one price level of an OrderBook: the price it sits at
// and the total size resting there. Both fields are always JSON strings on
// the wire, unlike Market's numeric fields, so they need no json.Number.
type OrderSummary struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// An OrderBook is one token's order book. Bids and Asks are both ordered
// worst price first: Bids ascend and Asks descend, so in both slices the
// last element is the top of book — the price closest to the midpoint.
// MarketPrice relies on this ordering.
type OrderBook struct {
	Market         string         `json:"market"`
	AssetID        string         `json:"asset_id"`
	Timestamp      string         `json:"timestamp"`
	Hash           string         `json:"hash"`
	Bids           []OrderSummary `json:"bids"`
	Asks           []OrderSummary `json:"asks"`
	MinOrderSize   string         `json:"min_order_size"`
	TickSize       string         `json:"tick_size"`
	NegRisk        bool           `json:"neg_risk"`
	LastTradePrice string         `json:"last_trade_price"`
}

// OrderBook fetches one token's order book: no authentication, GET /book.
func (c *Client) OrderBook(ctx context.Context, tokenID string) (OrderBook, error) {
	var out OrderBook
	err := c.session.Get(ctx, epBook, url.Values{"token_id": {tokenID}}, &out)
	return out, err
}

// BookParams selects one token and, where the endpoint uses it, one side of
// the book. It is the request-body element for the plural market-data
// endpoints below: Books, Midpoints, Prices, Spreads and LastTradesPrices.
// Side is ignored by every one of them except Prices.
type BookParams struct {
	TokenID string          `json:"token_id"`
	Side    polymarket.Side `json:"side,omitempty"`
}

// Books fetches order books for several tokens in one round trip: no
// authentication, POST /books. Despite being a POST, it only reads; the
// verb is the API's choice, driven by the token list riding in the body
// rather than the query string. It is called through c.do rather than
// c.postL2, since postL2 would require level-2 credentials this endpoint
// does not need.
func (c *Client) Books(ctx context.Context, params []BookParams) ([]OrderBook, error) {
	var out []OrderBook
	err := c.session.Do(ctx, polymarket.Request{Method: http.MethodPost, Path: epBooks, Body: params, Out: &out})
	return out, err
}

// midpointResponse is the GET /midpoint envelope.
type midpointResponse struct {
	Mid string `json:"mid"`
}

// Midpoint reports a token's midpoint price — the average of the best bid
// and best ask: no authentication, GET /midpoint.
func (c *Client) Midpoint(ctx context.Context, tokenID string) (string, error) {
	var out midpointResponse
	err := c.session.Get(ctx, epMidpoint, url.Values{"token_id": {tokenID}}, &out)
	return out.Mid, err
}

// Midpoints reports midpoint prices for several tokens in one round trip,
// keyed by token id: no authentication, POST /midpoints.
func (c *Client) Midpoints(ctx context.Context, params []BookParams) (map[string]string, error) {
	var out map[string]string
	err := c.session.Do(ctx, polymarket.Request{Method: http.MethodPost, Path: epMidpoints, Body: params, Out: &out})
	return out, err
}

// priceResponse is the GET /price envelope.
type priceResponse struct {
	Price string `json:"price"`
}

// Price reports the best price available on one side of a token's book: no
// authentication, GET /price. side selects BUY (the best ask, what a buyer
// would pay) or SELL (the best bid, what a seller would receive).
func (c *Client) Price(ctx context.Context, tokenID string, side polymarket.Side) (string, error) {
	var out priceResponse
	err := c.session.Get(ctx, epPrice, url.Values{"token_id": {tokenID}, "side": {string(side)}}, &out)
	return out.Price, err
}

// Prices reports Price for several (token, side) pairs in one round trip,
// keyed first by token id and then by side: no authentication, POST
// /prices. Unlike Books, Midpoints and Spreads, this plural endpoint reads
// Side out of each BookParams.
func (c *Client) Prices(ctx context.Context, params []BookParams) (map[string]map[polymarket.Side]string, error) {
	var out map[string]map[polymarket.Side]string
	err := c.session.Do(ctx, polymarket.Request{Method: http.MethodPost, Path: epPrices, Body: params, Out: &out})
	return out, err
}

// spreadResponse is the GET /spread envelope.
type spreadResponse struct {
	Spread string `json:"spread"`
}

// Spread reports the gap between a token's best bid and best ask: no
// authentication, GET /spread.
func (c *Client) Spread(ctx context.Context, tokenID string) (string, error) {
	var out spreadResponse
	err := c.session.Get(ctx, epSpread, url.Values{"token_id": {tokenID}}, &out)
	return out.Spread, err
}

// Spreads reports Spread for several tokens in one round trip, keyed by
// token id: no authentication, POST /spreads.
func (c *Client) Spreads(ctx context.Context, params []BookParams) (map[string]string, error) {
	var out map[string]string
	err := c.session.Do(ctx, polymarket.Request{Method: http.MethodPost, Path: epSpreads, Body: params, Out: &out})
	return out, err
}

// A LastTradePrice reports the price and side of a token's most recent
// trade. TokenID is empty when the value came from LastTradePrice's
// single-token endpoint, which does not repeat it; LastTradesPrices fills
// it in.
type LastTradePrice struct {
	TokenID string          `json:"token_id,omitempty"`
	Price   string          `json:"price"`
	Side    polymarket.Side `json:"side"`
}

// LastTradePrice reports the price and side of a token's most recent trade:
// no authentication, GET /last-trade-price.
func (c *Client) LastTradePrice(ctx context.Context, tokenID string) (LastTradePrice, error) {
	var out LastTradePrice
	err := c.session.Get(ctx, epLastTradePrice, url.Values{"token_id": {tokenID}}, &out)
	return out, err
}

// LastTradesPrices reports LastTradePrice for several tokens in one round
// trip: no authentication, POST /last-trades-prices.
func (c *Client) LastTradesPrices(ctx context.Context, params []BookParams) ([]LastTradePrice, error) {
	var out []LastTradePrice
	err := c.session.Do(ctx, polymarket.Request{Method: http.MethodPost, Path: epLastTradesPrices, Body: params, Out: &out})
	return out, err
}

// A PriceHistoryPoint is one sample of a token's price over time.
type PriceHistoryPoint struct {
	Time  int64       `json:"t"`
	Price json.Number `json:"p"`
}

// priceHistoryResponse is the GET /prices-history envelope.
type priceHistoryResponse struct {
	History []PriceHistoryPoint `json:"history"`
}

// PriceHistoryParams selects the window PricesHistory samples. Set either
// Interval alone or both StartTs and EndTs; the endpoint requires one form
// or the other.
type PriceHistoryParams struct {
	// Interval is a preset window ending now: "max", "1w", "1d", "6h" or
	// "1h".
	Interval string
	// StartTs and EndTs bound an explicit unix-seconds window. Both are
	// required together, and only when Interval is empty.
	StartTs int64
	EndTs   int64
	// Fidelity is the resolution between samples, in minutes. Zero leaves it
	// to the API's default.
	Fidelity int
}

// PricesHistory reports a token's price over time: no authentication, GET
// /prices-history. Despite the query parameter's name, market takes a CLOB
// token id, not a condition id — confirmed live.
func (c *Client) PricesHistory(ctx context.Context, tokenID string, params PriceHistoryParams) ([]PriceHistoryPoint, error) {
	if params.Interval == "" && (params.StartTs == 0 || params.EndTs == 0) {
		return nil, fmt.Errorf("polymarket: PricesHistory needs Interval, or both StartTs and EndTs")
	}
	q := url.Values{"market": {tokenID}}
	if params.Interval != "" {
		q.Set("interval", params.Interval)
	} else {
		q.Set("startTs", strconv.FormatInt(params.StartTs, 10))
		q.Set("endTs", strconv.FormatInt(params.EndTs, 10))
	}
	if params.Fidelity != 0 {
		q.Set("fidelity", strconv.Itoa(params.Fidelity))
	}
	var out priceHistoryResponse
	err := c.session.Get(ctx, epPricesHistory, q, &out)
	return out.History, err
}

// looseNumber decodes a JSON string or a bare JSON number into its exact
// text. Unlike json.Number, which only accepts a JSON number token,
// looseNumber tolerates either — it exists solely for GET /tick-size's
// minimum_tick_size, the one field in this file observed live to change
// shape between deployments.
type looseNumber string

// UnmarshalJSON accepts a JSON string or a bare JSON number and keeps its
// exact text. A JSON null decodes to the empty string.
func (n *looseNumber) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		*n = ""
		return nil
	}
	if len(s) > 0 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return fmt.Errorf("polymarket: decoding tick size: %w", err)
		}
		*n = looseNumber(str)
		return nil
	}
	*n = looseNumber(s)
	return nil
}

// tickSizeResponse is the GET /tick-size envelope.
type tickSizeResponse struct {
	MinimumTickSize looseNumber `json:"minimum_tick_size"`
}

// TickSize reports a token's minimum price increment: no authentication,
// GET /tick-size. The result is normalised to the canonical form
// internal/amount's tick-size table keys on ("0.01", not "0.010"),
// regardless of which shape the wire used.
func (c *Client) TickSize(ctx context.Context, tokenID string) (string, error) {
	var out tickSizeResponse
	if err := c.session.Get(ctx, epTickSize, url.Values{"token_id": {tokenID}}, &out); err != nil {
		return "", err
	}
	return canonicalTick(string(out.MinimumTickSize)), nil
}

// canonicalTick trims a decimal string's insignificant trailing zeros, so
// "0.010" and "0.01" both normalise to the same form.
func canonicalTick(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// negRiskResponse is the GET /neg-risk envelope.
type negRiskResponse struct {
	NegRisk bool `json:"neg_risk"`
}

// NegRisk reports whether a token belongs to a neg-risk market, which picks
// the exchange contract an order for it must be signed against: no
// authentication, GET /neg-risk.
func (c *Client) NegRisk(ctx context.Context, tokenID string) (bool, error) {
	var out negRiskResponse
	err := c.session.Get(ctx, epNegRisk, url.Values{"token_id": {tokenID}}, &out)
	return out.NegRisk, err
}

// feeRateResponse is the GET /fee-rate envelope.
type feeRateResponse struct {
	BaseFee int `json:"base_fee"`
}

// FeeRate reports a token's flat base fee rate in basis points against
// BuilderFeeBps: no authentication, GET /fee-rate. This is a separate,
// independently maintained figure from ClobMarketFee's curved rate — the
// two are not derivable from one another; see ClobMarketFee.
func (c *Client) FeeRate(ctx context.Context, tokenID string) (int, error) {
	var out feeRateResponse
	err := c.session.Get(ctx, epFeeRate, url.Values{"token_id": {tokenID}}, &out)
	return out.BaseFee, err
}

// MarketPrice computes the price a market order for size would clear at, by
// walking OrderBook's opposite side: a buy walks the asks and a sell walks
// the bids. size is USDC for a buy and shares for a sell, matching
// MarketOrder.Amount — MarketOrder.Price is usually this value. MarketPrice
// needs no authentication and makes one OrderBook request.
//
// It mirrors calculateMarketPrice in the official SDK: a FOK order that the
// visible book cannot fill completely is an error, since a fill-or-kill
// order priced beyond the book would simply be rejected; any other
// OrderType instead returns the worst price the walk reached, which is
// marketable against the whole visible book.
func (c *Client) MarketPrice(ctx context.Context, tokenID string, side polymarket.Side, size string, orderType polymarket.OrderType) (string, error) {
	book, err := c.OrderBook(ctx, tokenID)
	if err != nil {
		return "", err
	}
	return marketPrice(book, side, size, orderType)
}

// marketPrice is MarketPrice's pure computation, kept separate so it can be
// tested against canned books without a network round trip.
//
// Bids and asks both come back worst price first (see OrderBook), so the
// walk runs from the end of the slice toward the start: that is where the
// most competitive price is.
func marketPrice(book OrderBook, side polymarket.Side, size string, orderType polymarket.OrderType) (string, error) {
	amt, err := amount.ParseDecimal(size)
	if err != nil {
		return "", err
	}
	levels := book.Asks
	if side == polymarket.Sell {
		levels = book.Bids
	}
	if len(levels) == 0 {
		return "", fmt.Errorf("polymarket: MarketPrice: no match: the book has no %s levels", side)
	}

	sum := new(big.Rat)
	for i := len(levels) - 1; i >= 0; i-- {
		lvl := levels[i]
		levelSize, err := amount.ParseDecimal(lvl.Size)
		if err != nil {
			return "", err
		}
		if side == polymarket.Buy {
			levelPrice, err := amount.ParseDecimal(lvl.Price)
			if err != nil {
				return "", err
			}
			sum.Add(sum, new(big.Rat).Mul(levelSize, levelPrice))
		} else {
			sum.Add(sum, levelSize)
		}
		if sum.Cmp(amt) >= 0 {
			return lvl.Price, nil
		}
	}
	if orderType == polymarket.FOK {
		return "", fmt.Errorf("polymarket: MarketPrice: no match: the book cannot fill %s at FOK", size)
	}
	return levels[0].Price, nil
}

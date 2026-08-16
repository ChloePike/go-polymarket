// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/ChloePike/go-polymarket/internal/amount"
)

// bodyRequest captures an HTTP request's method, path, header and raw body.
// recordedRequest (rewards_test.go) does not capture a body, which the
// plural POST market-data endpoints need asserted.
type bodyRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// bodyRecordingServer starts an httptest.Server that fills rec in from the
// request it receives, including its body, and replies with status and
// respBody. It closes itself when the test ends.
func bodyRecordingServer(t *testing.T, rec *bodyRequest, status int, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Method = r.Method
		rec.Path = r.URL.Path
		rec.Header = r.Header.Clone()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rec.Body = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// decodeBookParams unmarshals a recorded POST body into []BookParams, so a
// test can compare it against the params a method was called with.
func decodeBookParams(t *testing.T, body []byte) []BookParams {
	t.Helper()
	var got []BookParams
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding request body: %v (body %s)", err, body)
	}
	return got
}

func TestPing(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, `"OK"`)
	c := &Client{Host: srv.URL}

	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", rec.Method)
	}
	if rec.Path != epOK {
		t.Errorf("path = %s, want %s", rec.Path, epOK)
	}
	assertNoAuthHeaders(t, rec.Header)
}

func TestPingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer srv.Close()
	c := &Client{Host: srv.URL}

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", apiErr.StatusCode)
	}
}

func TestTime(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, `1786863613`)
	c := &Client{Host: srv.URL}

	got, err := c.Time(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != 1786863613 {
		t.Errorf("Time() = %d, want 1786863613", got)
	}
	if rec.Path != epTime {
		t.Errorf("path = %s, want %s", rec.Path, epTime)
	}
}

// marketBody is a live capture of GET /markets/{conditionID} (also the shape
// GET /markets and GET /sampling-markets serve per row), trimmed to a short
// description.
const marketBody = `{
  "enable_order_book": true,
  "active": true,
  "closed": false,
  "archived": false,
  "accepting_orders": true,
  "accepting_order_timestamp": "2025-07-03T20:36:33Z",
  "minimum_order_size": 5,
  "minimum_tick_size": 0.001,
  "condition_id": "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7",
  "question_id": "0x1d925c6933062c2e38031293612d8680ffa097c5d3ba2f87a8ecc565bd47183e",
  "question": "Xi Jinping out before 2027?",
  "description": "Resolves Yes if Xi Jinping leaves power before 2027.",
  "market_slug": "xi-jinping-out-before-2027",
  "end_date_iso": "2026-12-31T00:00:00Z",
  "game_start_time": null,
  "seconds_delay": 0,
  "fpmm": "",
  "maker_base_fee": 1000,
  "taker_base_fee": 1000,
  "notifications_enabled": true,
  "neg_risk": false,
  "neg_risk_market_id": "",
  "neg_risk_request_id": "",
  "icon": "https://example.com/icon.png",
  "image": "https://example.com/image.png",
  "rewards": {
    "rates": [
      {"asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", "rewards_daily_rate": 10}
    ],
    "min_size": 200,
    "max_spread": 3.5
  },
  "is_50_50_outcome": false,
  "tokens": [
    {"token_id": "32338220190071351435772801779725302244575775216413325951443816017994629993401", "outcome": "Yes", "price": 0.0445, "winner": false},
    {"token_id": "25659310674993675562345759665114759892400026242514633218387667107987341231962", "outcome": "No", "price": 0.9555, "winner": false}
  ],
  "tags": ["world affairs", "Geopolitics"]
}`

func checkMarketBody(t *testing.T, m Market) {
	t.Helper()
	if m.ConditionID != "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7" {
		t.Errorf("ConditionID = %s", m.ConditionID)
	}
	if string(m.MinimumTickSize) != "0.001" {
		t.Errorf("MinimumTickSize = %s, want 0.001", m.MinimumTickSize)
	}
	if string(m.MinimumOrderSize) != "5" {
		t.Errorf("MinimumOrderSize = %s, want 5", m.MinimumOrderSize)
	}
	if m.GameStartTime != "" {
		t.Errorf("GameStartTime = %q, want empty (JSON null)", m.GameStartTime)
	}
	if len(m.Tokens) != 2 || m.Tokens[0].TokenID != "32338220190071351435772801779725302244575775216413325951443816017994629993401" {
		t.Fatalf("Tokens = %+v", m.Tokens)
	}
	if string(m.Tokens[0].Price) != "0.0445" {
		t.Errorf("Tokens[0].Price = %s, want 0.0445", m.Tokens[0].Price)
	}
	if len(m.Rewards.Rates) != 1 || string(m.Rewards.Rates[0].RewardsDailyRate) != "10" {
		t.Errorf("Rewards.Rates = %+v", m.Rewards.Rates)
	}
	if string(m.Rewards.MaxSpread) != "3.5" {
		t.Errorf("Rewards.MaxSpread = %s, want 3.5", m.Rewards.MaxSpread)
	}
}

func TestMarket(t *testing.T) {
	const conditionID = "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7"
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, marketBody)
	c := &Client{Host: srv.URL}

	m, err := c.Market(context.Background(), conditionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path != epMarket+conditionID {
		t.Errorf("path = %s, want %s", rec.Path, epMarket+conditionID)
	}
	assertNoAuthHeaders(t, rec.Header)
	checkMarketBody(t, m)
}

// cursorCase is one table-driven case exercising a paginated method's
// cursor handling: which cursor the caller passes and which next_cursor
// value the request must carry for it.
type cursorCase struct {
	name       string
	cursor     string
	wantCursor string
}

var cursorCases = []cursorCase{
	{name: "empty cursor defaults to CursorStart", cursor: "", wantCursor: CursorStart},
	{name: "caller's cursor passes through", cursor: "abc123", wantCursor: "abc123"},
}

func TestMarkets(t *testing.T) {
	page := `{"data":[` + marketBody + `],"next_cursor":"LTE=","limit":1000,"count":1}`
	for _, tc := range cursorCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, http.StatusOK, page)
			c := &Client{Host: srv.URL}

			markets, p, err := c.Markets(context.Background(), tc.cursor)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Path != epMarkets {
				t.Errorf("path = %s, want %s", rec.Path, epMarkets)
			}
			if got := rec.Query.Get("next_cursor"); got != tc.wantCursor {
				t.Errorf("next_cursor = %q, want %q", got, tc.wantCursor)
			}
			assertNoAuthHeaders(t, rec.Header)
			if len(markets) != 1 {
				t.Fatalf("got %d markets, want 1", len(markets))
			}
			checkMarketBody(t, markets[0])
			if p.NextCursor != CursorEnd {
				t.Errorf("NextCursor = %s, want %s", p.NextCursor, CursorEnd)
			}
			if p.Limit != 1000 || p.Count != 1 {
				t.Errorf("Pagination = %+v", p)
			}
		})
	}
}

func TestMarketsError(t *testing.T) {
	srv := recordingServer(t, new(recordedRequest), http.StatusInternalServerError, `{"error":"boom"}`)
	c := &Client{Host: srv.URL}

	if _, _, err := c.Markets(context.Background(), ""); err == nil {
		t.Fatal("got nil error, want one")
	}
}

// simplifiedMarketBody is a live capture of one row of GET
// /simplified-markets and GET /sampling-simplified-markets.
const simplifiedMarketBody = `{
  "condition_id": "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7",
  "rewards": {"rates": null, "min_size": 0, "max_spread": 0},
  "tokens": [
    {"token_id": "32338220190071351435772801779725302244575775216413325951443816017994629993401", "outcome": "Yes", "price": 0.0455, "winner": false},
    {"token_id": "25659310674993675562345759665114759892400026242514633218387667107987341231962", "outcome": "No", "price": 0.9545, "winner": false}
  ],
  "active": true,
  "closed": false,
  "archived": false,
  "accepting_orders": true
}`

// paginatedMarketsCase is one table-driven case covering a market-list
// endpoint's path and cursor handling. call adapts each method's specific
// row type into a common (row count, error) shape so Markets,
// SimplifiedMarkets, SamplingMarkets and SamplingSimplifiedMarkets can share
// one table despite differing return types.
type paginatedMarketsCase struct {
	name     string
	wantPath string
	body     string
	call     func(c *Client, ctx context.Context, cursor string) (n int, err error)
}

var paginatedMarketsCases = []paginatedMarketsCase{
	{
		name:     "SimplifiedMarkets",
		wantPath: epSimplifiedMarkets,
		body:     `{"data":[` + simplifiedMarketBody + `],"next_cursor":"LTE=","limit":500,"count":1}`,
		call: func(c *Client, ctx context.Context, cursor string) (int, error) {
			rows, _, err := c.SimplifiedMarkets(ctx, cursor)
			return len(rows), err
		},
	},
	{
		name:     "SamplingMarkets",
		wantPath: epSamplingMarkets,
		body:     `{"data":[` + marketBody + `],"next_cursor":"LTE=","limit":500,"count":1}`,
		call: func(c *Client, ctx context.Context, cursor string) (int, error) {
			rows, _, err := c.SamplingMarkets(ctx, cursor)
			return len(rows), err
		},
	},
	{
		name:     "SamplingSimplifiedMarkets",
		wantPath: epSamplingSimplifiedMarkets,
		body:     `{"data":[` + simplifiedMarketBody + `],"next_cursor":"LTE=","limit":500,"count":1}`,
		call: func(c *Client, ctx context.Context, cursor string) (int, error) {
			rows, _, err := c.SamplingSimplifiedMarkets(ctx, cursor)
			return len(rows), err
		},
	},
}

func TestPaginatedMarketEndpoints(t *testing.T) {
	for _, tc := range paginatedMarketsCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, http.StatusOK, tc.body)
			c := &Client{Host: srv.URL}

			n, err := tc.call(c, context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if rec.Path != tc.wantPath {
				t.Errorf("path = %s, want %s", rec.Path, tc.wantPath)
			}
			if got := rec.Query.Get("next_cursor"); got != CursorStart {
				t.Errorf("next_cursor = %q, want %q", got, CursorStart)
			}
			if n != 1 {
				t.Errorf("got %d rows, want 1", n)
			}
		})
	}
}

func TestSimplifiedMarketDecode(t *testing.T) {
	page := `{"data":[` + simplifiedMarketBody + `],"next_cursor":"LTE=","limit":500,"count":1}`
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, page)
	c := &Client{Host: srv.URL}

	rows, _, err := c.SimplifiedMarkets(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	m := rows[0]
	if m.Rewards.Rates != nil {
		t.Errorf("Rewards.Rates = %+v, want nil (JSON null)", m.Rewards.Rates)
	}
	if !m.AcceptingOrders {
		t.Error("AcceptingOrders = false, want true")
	}
}

func TestMarketByToken(t *testing.T) {
	const tokenID = "32338220190071351435772801779725302244575775216413325951443816017994629993401"
	body := `{"condition_id":"0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7","primary_token_id":"25659310674993675562345759665114759892400026242514633218387667107987341231962","secondary_token_id":"32338220190071351435772801779725302244575775216413325951443816017994629993401"}`
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, body)
	c := &Client{Host: srv.URL}

	got, err := c.MarketByToken(context.Background(), tokenID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path != epMarketByToken+tokenID {
		t.Errorf("path = %s, want %s", rec.Path, epMarketByToken+tokenID)
	}
	want := MarketByToken{
		ConditionID:      "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7",
		PrimaryTokenID:   "25659310674993675562345759665114759892400026242514633218387667107987341231962",
		SecondaryTokenID: "32338220190071351435772801779725302244575775216413325951443816017994629993401",
	}
	if got != want {
		t.Errorf("MarketByToken() = %+v, want %+v", got, want)
	}
}

// clobMarketBody is a live capture of GET /clob-markets/{conditionID}.
const clobMarketBody = `{"r":{"mi":200,"ma":3.5,"e":true,"moas":4},"t":[{"t":"32338220190071351435772801779725302244575775216413325951443816017994629993401","o":"Yes"},{"t":"25659310674993675562345759665114759892400026242514633218387667107987341231962","o":"No"}],"c":"0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7","mos":5,"mts":0.001,"mbf":1000,"tbf":1000,"ao":true,"cbos":true,"aot":"2025-07-03T20:36:33Z","ibce":true,"fd":{"r":0.04,"e":1,"to":true}}`

func TestClobMarket(t *testing.T) {
	const conditionID = "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7"
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, clobMarketBody)
	c := &Client{Host: srv.URL}

	got, err := c.ClobMarket(context.Background(), conditionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path != epClobMarket+conditionID {
		t.Errorf("path = %s, want %s", rec.Path, epClobMarket+conditionID)
	}
	if got.ConditionID != conditionID {
		t.Errorf("ConditionID = %s", got.ConditionID)
	}
	if len(got.Tokens) != 2 || got.Tokens[0].TokenID != "32338220190071351435772801779725302244575775216413325951443816017994629993401" {
		t.Fatalf("Tokens = %+v", got.Tokens)
	}
	if string(got.TickSize) != "0.001" {
		t.Errorf("TickSize = %s, want 0.001", got.TickSize)
	}
	if string(got.MinOrderSize) != "5" {
		t.Errorf("MinOrderSize = %s, want 5", got.MinOrderSize)
	}
	if got.Fee == nil || got.Fee.Rate != 0.04 || got.Fee.Exponent != 1 || !got.Fee.TakerOnly {
		t.Errorf("Fee = %+v", got.Fee)
	}
	if got.Rewards == nil || string(got.Rewards.MinSize) != "200" || string(got.Rewards.MaxSpread) != "3.5" || !got.Rewards.Enabled {
		t.Errorf("Rewards = %+v", got.Rewards)
	}
	if string(got.Rewards.MOAS) != "4" {
		t.Errorf("Rewards.MOAS = %s, want 4", got.Rewards.MOAS)
	}
	if !got.AcceptingOrders || got.MakerBaseFee != 1000 || got.TakerBaseFee != 1000 {
		t.Errorf("got = %+v", got)
	}
}

// marketLiveActivityBody is a live capture of GET
// /markets/live-activity/{conditionID}. See MarketLiveActivity's doc comment
// for why this does not match the SDK's declared MarketTradeEvent[].
const marketLiveActivityBody = `{"condition_id":"0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7","id":559651,"question":"Xi Jinping out before 2027?","market_slug":"xi-jinping-out-before-2027","event_slug":"xi-jinping-out-before-2027","series_slug":"xi-jinping-out","icon":"https://example.com/icon.png","image":"https://example.com/image.png","tags":["world affairs","Geopolitics"]}`

func TestMarketTradesEvents(t *testing.T) {
	const conditionID = "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7"
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, marketLiveActivityBody)
	c := &Client{Host: srv.URL}

	got, err := c.MarketTradesEvents(context.Background(), conditionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path != epMarketTradesEvents+conditionID {
		t.Errorf("path = %s, want %s", rec.Path, epMarketTradesEvents+conditionID)
	}
	if got.ID != 559651 || got.Question != "Xi Jinping out before 2027?" || len(got.Tags) != 2 {
		t.Errorf("got = %+v", got)
	}
}

// orderBookBody is a live capture of GET /book, trimmed to a few levels.
// Both sides are ordered worst price first, matching what OrderBook's doc
// comment and MarketPrice rely on.
const orderBookBody = `{
  "market": "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7",
  "asset_id": "32338220190071351435772801779725302244575775216413325951443816017994629993401",
  "timestamp": "1786863545542",
  "hash": "ce3dda14fc6bf5365063365b319e1ba8a477fc2e",
  "bids": [
    {"price": "0.001", "size": "10607956.49"},
    {"price": "0.002", "size": "2301172.5"}
  ],
  "asks": [
    {"price": "0.999", "size": "1502655.75"},
    {"price": "0.046", "size": "2906.87"}
  ],
  "min_order_size": "5",
  "tick_size": "0.001",
  "neg_risk": false,
  "last_trade_price": "0.045"
}`

func TestOrderBook(t *testing.T) {
	const tokenID = "32338220190071351435772801779725302244575775216413325951443816017994629993401"
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, orderBookBody)
	c := &Client{Host: srv.URL}

	got, err := c.OrderBook(context.Background(), tokenID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path != epBook {
		t.Errorf("path = %s, want %s", rec.Path, epBook)
	}
	if got := rec.Query.Get("token_id"); got != tokenID {
		t.Errorf("token_id = %q, want %q", got, tokenID)
	}
	assertNoAuthHeaders(t, rec.Header)
	if len(got.Bids) != 2 || len(got.Asks) != 2 {
		t.Fatalf("Bids/Asks = %d/%d, want 2/2", len(got.Bids), len(got.Asks))
	}
	if got.Bids[1].Price != "0.002" || got.Asks[1].Price != "0.046" {
		t.Errorf("Bids/Asks = %+v / %+v", got.Bids, got.Asks)
	}
	if got.MinOrderSize != "5" || got.TickSize != "0.001" {
		t.Errorf("MinOrderSize/TickSize = %s/%s", got.MinOrderSize, got.TickSize)
	}
}

func TestBooks(t *testing.T) {
	params := []BookParams{{TokenID: "111"}, {TokenID: "222"}}
	respBody := `[` + orderBookBody + `]`
	var rec bodyRequest
	srv := bodyRecordingServer(t, &rec, http.StatusOK, respBody)
	c := &Client{Host: srv.URL}

	got, err := c.Books(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.Path != epBooks {
		t.Errorf("path = %s, want %s", rec.Path, epBooks)
	}
	if got := decodeBookParams(t, rec.Body); !reflect.DeepEqual(got, params) {
		t.Errorf("request body = %+v, want %+v", got, params)
	}
	for _, h := range authHeaderKeys {
		if rec.Header.Get(h) != "" {
			t.Errorf("Books carries auth header %s, want none: it is unauthenticated", h)
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1", len(got))
	}
}

func TestMidpointAndMidpoints(t *testing.T) {
	const tokenID = "111"
	t.Run("Midpoint", func(t *testing.T) {
		var rec recordedRequest
		srv := recordingServer(t, &rec, http.StatusOK, `{"mid":"0.0455"}`)
		c := &Client{Host: srv.URL}

		got, err := c.Midpoint(context.Background(), tokenID)
		if err != nil {
			t.Fatal(err)
		}
		if got != "0.0455" {
			t.Errorf("Midpoint() = %q, want 0.0455", got)
		}
		if rec.Path != epMidpoint || rec.Query.Get("token_id") != tokenID {
			t.Errorf("path/query = %s %v", rec.Path, rec.Query)
		}
	})
	t.Run("Midpoints", func(t *testing.T) {
		params := []BookParams{{TokenID: "111"}, {TokenID: "222"}}
		body := `{"111":"0.0455","222":"0.9545"}`
		var rec bodyRequest
		srv := bodyRecordingServer(t, &rec, http.StatusOK, body)
		c := &Client{Host: srv.URL}

		got, err := c.Midpoints(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Method != http.MethodPost || rec.Path != epMidpoints {
			t.Errorf("method/path = %s %s", rec.Method, rec.Path)
		}
		want := map[string]string{"111": "0.0455", "222": "0.9545"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Midpoints() = %+v, want %+v", got, want)
		}
	})
}

func TestPriceAndPrices(t *testing.T) {
	const tokenID = "111"
	t.Run("Price", func(t *testing.T) {
		var rec recordedRequest
		srv := recordingServer(t, &rec, http.StatusOK, `{"price":"0.045"}`)
		c := &Client{Host: srv.URL}

		got, err := c.Price(context.Background(), tokenID, Buy)
		if err != nil {
			t.Fatal(err)
		}
		if got != "0.045" {
			t.Errorf("Price() = %q, want 0.045", got)
		}
		if rec.Query.Get("side") != "BUY" || rec.Query.Get("token_id") != tokenID {
			t.Errorf("query = %v", rec.Query)
		}
	})
	t.Run("Prices", func(t *testing.T) {
		params := []BookParams{{TokenID: "111", Side: Buy}, {TokenID: "111", Side: Sell}}
		body := `{"111":{"BUY":"0.045","SELL":"0.046"}}`
		var rec bodyRequest
		srv := bodyRecordingServer(t, &rec, http.StatusOK, body)
		c := &Client{Host: srv.URL}

		got, err := c.Prices(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Path != epPrices {
			t.Errorf("path = %s, want %s", rec.Path, epPrices)
		}
		gotParams := decodeBookParams(t, rec.Body)
		if !reflect.DeepEqual(gotParams, params) {
			t.Errorf("request body = %+v, want %+v", gotParams, params)
		}
		want := map[string]map[Side]string{"111": {Buy: "0.045", Sell: "0.046"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Prices() = %+v, want %+v", got, want)
		}
	})
}

func TestSpreadAndSpreads(t *testing.T) {
	const tokenID = "111"
	t.Run("Spread", func(t *testing.T) {
		var rec recordedRequest
		srv := recordingServer(t, &rec, http.StatusOK, `{"spread":"0.001"}`)
		c := &Client{Host: srv.URL}

		got, err := c.Spread(context.Background(), tokenID)
		if err != nil {
			t.Fatal(err)
		}
		if got != "0.001" {
			t.Errorf("Spread() = %q, want 0.001", got)
		}
		if rec.Path != epSpread {
			t.Errorf("path = %s, want %s", rec.Path, epSpread)
		}
	})
	t.Run("Spreads", func(t *testing.T) {
		params := []BookParams{{TokenID: "111"}, {TokenID: "222"}}
		body := `{"111":"0.001","222":"0.001"}`
		var rec bodyRequest
		srv := bodyRecordingServer(t, &rec, http.StatusOK, body)
		c := &Client{Host: srv.URL}

		got, err := c.Spreads(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Path != epSpreads {
			t.Errorf("path = %s, want %s", rec.Path, epSpreads)
		}
		want := map[string]string{"111": "0.001", "222": "0.001"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Spreads() = %+v, want %+v", got, want)
		}
	})
}

func TestLastTradePriceAndLastTradesPrices(t *testing.T) {
	const tokenID = "111"
	t.Run("LastTradePrice", func(t *testing.T) {
		var rec recordedRequest
		srv := recordingServer(t, &rec, http.StatusOK, `{"price":"0.045","side":"BUY"}`)
		c := &Client{Host: srv.URL}

		got, err := c.LastTradePrice(context.Background(), tokenID)
		if err != nil {
			t.Fatal(err)
		}
		want := LastTradePrice{Price: "0.045", Side: Buy}
		if got != want {
			t.Errorf("LastTradePrice() = %+v, want %+v", got, want)
		}
		if rec.Path != epLastTradePrice {
			t.Errorf("path = %s, want %s", rec.Path, epLastTradePrice)
		}
	})
	t.Run("LastTradesPrices", func(t *testing.T) {
		params := []BookParams{{TokenID: "111"}, {TokenID: "222"}}
		body := `[{"price":"0.954","side":"SELL","token_id":"222"},{"price":"0.045","side":"BUY","token_id":"111"}]`
		var rec bodyRequest
		srv := bodyRecordingServer(t, &rec, http.StatusOK, body)
		c := &Client{Host: srv.URL}

		got, err := c.LastTradesPrices(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Path != epLastTradesPrices {
			t.Errorf("path = %s, want %s", rec.Path, epLastTradesPrices)
		}
		want := []LastTradePrice{
			{TokenID: "222", Price: "0.954", Side: Sell},
			{TokenID: "111", Price: "0.045", Side: Buy},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LastTradesPrices() = %+v, want %+v", got, want)
		}
	})
}

// priceHistoryCase is one table-driven case for TestPricesHistory: the
// params passed in, and the query values the request must carry for them.
type priceHistoryCase struct {
	name    string
	params  PriceHistoryParams
	want    url.Values
	wantErr bool
}

var priceHistoryCases = []priceHistoryCase{
	{
		name:   "interval",
		params: PriceHistoryParams{Interval: "1d"},
		want:   url.Values{"market": {"111"}, "interval": {"1d"}},
	},
	{
		name:   "startTs and endTs",
		params: PriceHistoryParams{StartTs: 1000, EndTs: 2000},
		want:   url.Values{"market": {"111"}, "startTs": {"1000"}, "endTs": {"2000"}},
	},
	{
		name:   "interval with fidelity",
		params: PriceHistoryParams{Interval: "1h", Fidelity: 10},
		want:   url.Values{"market": {"111"}, "interval": {"1h"}, "fidelity": {"10"}},
	},
	{
		name:    "neither interval nor a full startTs/endTs pair is an error",
		params:  PriceHistoryParams{StartTs: 1000},
		wantErr: true,
	},
}

func TestPricesHistory(t *testing.T) {
	respBody := `{"history":[{"t":1786860613,"p":0.0445},{"t":1786861208,"p":0.0455}]}`
	for _, tc := range priceHistoryCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, http.StatusOK, respBody)
			c := &Client{Host: srv.URL}

			got, err := c.PricesHistory(context.Background(), "111", tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rec.Path != epPricesHistory {
				t.Errorf("path = %s, want %s", rec.Path, epPricesHistory)
			}
			if !reflect.DeepEqual(rec.Query, tc.want) {
				t.Errorf("query = %v, want %v", rec.Query, tc.want)
			}
			if len(got) != 2 || got[0].Time != 1786860613 || string(got[0].Price) != "0.0445" {
				t.Errorf("PricesHistory() = %+v", got)
			}
		})
	}
}

// tickSizeCase is one table-driven case for TestTickSize, covering both wire
// shapes GET /tick-size has been observed to use.
type tickSizeCase struct {
	name string
	body string
	want string
}

var tickSizeCases = []tickSizeCase{
	{name: "bare number", body: `{"minimum_tick_size":0.001}`, want: "0.001"},
	{name: "bare number with trailing zero", body: `{"minimum_tick_size":0.010}`, want: "0.01"},
	{name: "JSON string", body: `{"minimum_tick_size":"0.01"}`, want: "0.01"},
	{name: "JSON string with trailing zero", body: `{"minimum_tick_size":"0.0100"}`, want: "0.01"},
}

func TestTickSize(t *testing.T) {
	for _, tc := range tickSizeCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, http.StatusOK, tc.body)
			c := &Client{Host: srv.URL}

			got, err := c.TickSize(context.Background(), "111")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("TickSize() = %q, want %q", got, tc.want)
			}
			if rec.Path != epTickSize {
				t.Errorf("path = %s, want %s", rec.Path, epTickSize)
			}
			if _, ok := amount.ByTickSize[got]; !ok {
				t.Errorf("TickSize() = %q does not key into amount.ByTickSize", got)
			}
		})
	}
}

// canonicalTickCase is one table-driven case for TestCanonicalTick.
type canonicalTickCase struct {
	name  string
	input string
	want  string
}

var canonicalTickCases = []canonicalTickCase{
	{name: "already canonical", input: "0.001", want: "0.001"},
	{name: "trailing zero", input: "0.010", want: "0.01"},
	{name: "several trailing zeros", input: "0.1000", want: "0.1"},
	{name: "integer, no decimal point", input: "5", want: "5"},
	{name: "trailing zero leaves the point stripped too", input: "1.0", want: "1"},
}

func TestCanonicalTick(t *testing.T) {
	for _, tc := range canonicalTickCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalTick(tc.input); got != tc.want {
				t.Errorf("canonicalTick(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// looseNumberCase is one table-driven case for TestLooseNumberUnmarshalJSON.
type looseNumberCase struct {
	name    string
	input   string
	want    looseNumber
	wantErr bool
}

var looseNumberCases = []looseNumberCase{
	{name: "bare number", input: `0.001`, want: "0.001"},
	{name: "JSON string", input: `"0.001"`, want: "0.001"},
	{name: "null", input: `null`, want: ""},
	{name: "malformed string", input: `"unterminated`, wantErr: true},
}

func TestLooseNumberUnmarshalJSON(t *testing.T) {
	for _, tc := range looseNumberCases {
		t.Run(tc.name, func(t *testing.T) {
			var got looseNumber
			err := got.UnmarshalJSON([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNegRisk(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, `{"neg_risk":true}`)
	c := &Client{Host: srv.URL}

	got, err := c.NegRisk(context.Background(), "111")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("NegRisk() = false, want true")
	}
	if rec.Path != epNegRisk {
		t.Errorf("path = %s, want %s", rec.Path, epNegRisk)
	}
}

func TestFeeRate(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, `{"base_fee":1000}`)
	c := &Client{Host: srv.URL}

	got, err := c.FeeRate(context.Background(), "111")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1000 {
		t.Errorf("FeeRate() = %d, want 1000", got)
	}
	if rec.Path != epFeeRate {
		t.Errorf("path = %s, want %s", rec.Path, epFeeRate)
	}
}

// testBook is a small order book for marketPrice's table-driven tests. Both
// sides are ordered worst price first, as OrderBook's doc comment describes
// and as GET /book was live-verified to send: bids ascend, asks descend, so
// the last element of each is the top of book.
var testBook = OrderBook{
	Bids: []OrderSummary{
		{Price: "0.40", Size: "8"},
		{Price: "0.42", Size: "12"},
		{Price: "0.44", Size: "6"},
	},
	Asks: []OrderSummary{
		{Price: "0.50", Size: "10"},
		{Price: "0.48", Size: "20"},
		{Price: "0.46", Size: "5"},
	},
}

// marketPriceCase is one table-driven case for TestMarketPrice.
type marketPriceCase struct {
	name      string
	book      OrderBook
	side      Side
	size      string
	orderType OrderType
	want      string
	wantErr   bool
}

var marketPriceCases = []marketPriceCase{
	{
		name: "buy stops at the best ask on an exact threshold",
		book: testBook, side: Buy, size: "2.3", orderType: FOK,
		want: "0.46", // top of book: 5 shares * 0.46 = 2.3
	},
	{
		name: "buy walks past the best ask when it is not enough",
		book: testBook, side: Buy, size: "5", orderType: FOK,
		want: "0.48", // 0.46 level contributes 2.3 < 5; add 20*0.48=9.6, total 11.9 >= 5
	},
	{
		name: "buy FOK with insufficient book liquidity is an error",
		book: testBook, side: Buy, size: "100", orderType: FOK,
		wantErr: true,
	},
	{
		name: "buy FAK with insufficient liquidity falls back to the worst ask",
		book: testBook, side: Buy, size: "100", orderType: FAK,
		want: "0.50", // positions[0], the worst (first) element of Asks
	},
	{
		name: "sell stops at the best bid on an exact threshold",
		book: testBook, side: Sell, size: "6", orderType: FOK,
		want: "0.44", // top of book: 6 shares
	},
	{
		name: "sell walks past the best bid when it is not enough",
		book: testBook, side: Sell, size: "10", orderType: FOK,
		want: "0.42", // 0.44 level contributes 6 < 10; add 12, total 18 >= 10
	},
	{
		name: "sell FOK with insufficient book liquidity is an error",
		book: testBook, side: Sell, size: "1000", orderType: FOK,
		wantErr: true,
	},
	{
		name: "sell GTC with insufficient liquidity falls back to the worst bid",
		book: testBook, side: Sell, size: "1000", orderType: GTC,
		want: "0.40", // positions[0], the worst (first) element of Bids
	},
	{
		name: "empty side is an error regardless of order type",
		book: OrderBook{}, side: Buy, size: "1", orderType: GTC,
		wantErr: true,
	},
}

func TestMarketPrice(t *testing.T) {
	for _, tc := range marketPriceCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := marketPrice(tc.book, tc.side, tc.size, tc.orderType)
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("marketPrice() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientMarketPrice(t *testing.T) {
	const tokenID = "111"
	body, err := json.Marshal(testBook)
	if err != nil {
		t.Fatal(err)
	}
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, string(body))
	c := &Client{Host: srv.URL}

	got, err := c.MarketPrice(context.Background(), tokenID, Buy, "2.3", FOK)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.46" {
		t.Errorf("MarketPrice() = %q, want 0.46", got)
	}
	if rec.Path != epBook || rec.Query.Get("token_id") != tokenID {
		t.Errorf("path/query = %s %v", rec.Path, rec.Query)
	}
}

// intPage is a tiny stub of a cursor-paginated endpoint's row source, used
// to test Pages without any network round trip. It hands out items across
// two pages and records every cursor it was called with.
type intPage struct {
	calls []string // cursors get was called with, in order
}

func (p *intPage) get(_ context.Context, cursor string) ([]int, Pagination, error) {
	p.calls = append(p.calls, cursor)
	switch cursor {
	case "":
		return []int{1, 2}, Pagination{NextCursor: "cur2"}, nil
	case "cur2":
		return []int{3}, Pagination{NextCursor: CursorEnd}, nil
	default:
		return nil, Pagination{}, fmt.Errorf("intPage: unexpected cursor %q", cursor)
	}
}

func TestPagesWalksEveryPage(t *testing.T) {
	src := &intPage{}
	var got []int
	for v, err := range Pages(context.Background(), src.get) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Pages walked %v, want %v", got, want)
	}
	wantCalls := []string{"", "cur2"}
	if !reflect.DeepEqual(src.calls, wantCalls) {
		t.Errorf("get called with %v, want %v", src.calls, wantCalls)
	}
}

func TestPagesStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	get := func(_ context.Context, cursor string) ([]int, Pagination, error) {
		return nil, Pagination{}, boom
	}
	n := 0
	var gotErr error
	for v, err := range Pages(context.Background(), get) {
		n++
		gotErr = err
		if v != 0 {
			t.Errorf("v = %d, want the zero value", v)
		}
	}
	if n != 1 {
		t.Errorf("iterated %d times, want 1", n)
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("err = %v, want %v", gotErr, boom)
	}
}

func TestPagesStopsWhenConsumerBreaks(t *testing.T) {
	src := &intPage{}
	n := 0
	for range Pages(context.Background(), src.get) {
		n++
		break
	}
	if n != 1 {
		t.Errorf("iterated %d times, want 1", n)
	}
	if len(src.calls) != 1 {
		t.Errorf("get called %d times, want 1: Pages should not fetch a page it never yields from", len(src.calls))
	}
}

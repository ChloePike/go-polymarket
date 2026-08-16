// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// recordedRequest captures the HTTP request a test server received, so a
// test can assert on the verb, path, query and headers a Client method sent
// without inspecting the transport directly.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
}

// recordingServer starts an httptest.Server that fills rec in from the
// request it receives and replies with status and body. It closes itself
// when the test ends.
func recordingServer(t *testing.T, rec *recordedRequest, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Method = r.Method
		rec.Path = r.URL.Path
		rec.Query = r.URL.Query()
		rec.Header = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// clobGoldenAccount is a signing key and the address derived from it, one
// entry of testdata/vectors.json's "accounts" array — the golden vectors the
// root package's signing tests are pinned against. testL2Client borrows the
// first account as a working key; it does not need the file's order and
// signature vectors, only its accounts.
type clobGoldenAccount struct {
	PrivateKey string `json:"privateKey"`
	Address    string `json:"address"`
}

// clobGoldenFile is the slice of testdata/vectors.json this package's tests
// read.
type clobGoldenFile struct {
	Accounts []clobGoldenAccount `json:"accounts"`
}

// loadGolden reads the golden vectors testL2Client needs a signing key from.
func loadGolden(t *testing.T) clobGoldenFile {
	t.Helper()
	b, err := os.ReadFile("../testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var g clobGoldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return g
}

// testL2Client builds a Client pointed at host with a working Signer and
// APICreds, so it can send level-2 requests. The key comes from the golden
// vectors already pinned for signing tests.
func testL2Client(t *testing.T, host string) *Client {
	t.Helper()
	g := loadGolden(t)
	if len(g.Accounts) == 0 {
		t.Fatal("golden accounts are empty")
	}
	key, err := polymarket.NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	creds := polymarket.APICreds{Key: "key-1", Secret: "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM=", Passphrase: "pass-1"}
	return New(WithHost(host), WithSigner(key), WithCredentials(creds))
}

// authHeaderKeys are the headers both authentication levels can set. Level 1
// is never used by the rewards endpoints, so only its presence or absence as
// a group distinguishes level-2 from no authentication.
var authHeaderKeys = []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE", "POLY_TIMESTAMP"}

// assertNoAuthHeaders fails the test if h carries any authentication header.
func assertNoAuthHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for _, k := range authHeaderKeys {
		if h.Get(k) != "" {
			t.Errorf("unauthenticated request carries %s header", k)
		}
	}
}

// assertL2Headers fails the test if h is missing a level-2 header, or if the
// API key it carries does not match testL2Client's credentials.
func assertL2Headers(t *testing.T, h http.Header) {
	t.Helper()
	for _, k := range authHeaderKeys {
		if h.Get(k) == "" {
			t.Errorf("level-2 request missing %s header", k)
		}
	}
	if got := h.Get("POLY_API_KEY"); got != "key-1" {
		t.Errorf("POLY_API_KEY = %q, want key-1", got)
	}
}

// Live capture of one page of GET /rewards/markets/current.
const currentRewardsMarketsBody = `{
  "data": [
    {
      "condition_id": "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda",
      "rewards_config": [
        {
          "asset_address": "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB",
          "start_date": "2026-08-15",
          "end_date": "2500-12-31",
          "rate_per_day": 2,
          "total_rewards": 0,
          "id": 0
        }
      ],
      "rewards_max_spread": 4.5,
      "rewards_min_size": 20,
      "native_daily_rate": 2,
      "total_daily_rate": 2
    }
  ],
  "next_cursor": "LTE=",
  "limit": 100,
  "count": 1
}`

// currentRewardsMarketsCase is one table-driven case for
// TestCurrentRewardsMarkets.
type currentRewardsMarketsCase struct {
	name       string
	cursor     string // argument passed to CurrentRewardsMarkets
	wantCursor string // next_cursor query value the request should carry
	status     int
	body       string
	wantErr    bool
}

var currentRewardsMarketsCases = []currentRewardsMarketsCase{
	{
		name:       "first page defaults the cursor",
		cursor:     "",
		wantCursor: polymarket.CursorStart,
		status:     http.StatusOK,
		body:       currentRewardsMarketsBody,
	},
	{
		name:       "later page carries the caller's cursor",
		cursor:     "MTA=",
		wantCursor: "MTA=",
		status:     http.StatusOK,
		body:       currentRewardsMarketsBody,
	},
	{
		name:    "API error surfaces as *Error",
		cursor:  "",
		status:  http.StatusInternalServerError,
		body:    `{"error":"boom"}`,
		wantErr: true,
	},
}

func TestCurrentRewardsMarkets(t *testing.T) {
	for _, tc := range currentRewardsMarketsCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, tc.status, tc.body)
			c := New(WithHost(srv.URL))

			markets, page, err := c.CurrentRewardsMarkets(context.Background(), tc.cursor)
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				apiErr, ok := err.(*polymarket.Error)
				if !ok {
					t.Fatalf("error type = %T, want *polymarket.Error", err)
				}
				if apiErr.StatusCode != tc.status {
					t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
				}
				if apiErr.Message != "boom" {
					t.Errorf("Message = %q, want %q", apiErr.Message, "boom")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if rec.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", rec.Method)
			}
			if rec.Path != epRewardsMarketsCurrent {
				t.Errorf("path = %s, want %s", rec.Path, epRewardsMarketsCurrent)
			}
			if got := rec.Query.Get("next_cursor"); got != tc.wantCursor {
				t.Errorf("next_cursor = %q, want %q", got, tc.wantCursor)
			}
			assertNoAuthHeaders(t, rec.Header)

			if len(markets) != 1 {
				t.Fatalf("got %d markets, want 1", len(markets))
			}
			m := markets[0]
			if m.ConditionID != "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda" {
				t.Errorf("ConditionID = %s", m.ConditionID)
			}
			if string(m.RewardsMaxSpread) != "4.5" {
				t.Errorf("RewardsMaxSpread = %s, want 4.5", m.RewardsMaxSpread)
			}
			if string(m.NativeDailyRate) != "2" {
				t.Errorf("NativeDailyRate = %s, want 2", m.NativeDailyRate)
			}
			if len(m.RewardsConfig) != 1 || m.RewardsConfig[0].StartDate != "2026-08-15" {
				t.Errorf("RewardsConfig = %+v", m.RewardsConfig)
			}
			if page.NextCursor != polymarket.CursorEnd {
				t.Errorf("NextCursor = %s, want %s", page.NextCursor, polymarket.CursorEnd)
			}
		})
	}
}

// Live capture of one page of GET /rewards/markets/{condition_id}. This
// shape shares nothing distinguishing with currentRewardsMarketsBody's: see
// RewardsMarketDetail's doc comment.
const rewardsMarketDetailBody = `{
  "data": [
    {
      "condition_id": "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda",
      "question": "Will the Republican Party win the NY-11 House seat?",
      "market_slug": "will-the-republican-party-win-the-ny-11-house-seat",
      "event_slug": "ny-11-house-election-winner",
      "image": "",
      "tokens": [
        {"token_id": "50868012450412588231700991321379235183301872220529434142919756787462014093776", "outcome": "Yes", "price": 0.935},
        {"token_id": "37843096702983984154813593339817451110105539743235209768242171001456417281055", "outcome": "No", "price": 0.065}
      ],
      "rewards_config": [
        {
          "asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
          "start_date": "2026-08-15",
          "end_date": "2500-12-31",
          "id": 71399,
          "rate_per_day": 2,
          "total_rewards": 0,
          "total_days": 173264
        }
      ],
      "rewards_max_spread": 4.5,
      "rewards_min_size": 20,
      "market_competitiveness": 461.474463
    }
  ],
  "next_cursor": "LTE=",
  "limit": 100,
  "count": 1
}`

func TestRewardsMarkets(t *testing.T) {
	const conditionID = "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda"

	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, rewardsMarketDetailBody)
	c := New(WithHost(srv.URL))

	details, page, err := c.RewardsMarkets(context.Background(), conditionID, "")
	if err != nil {
		t.Fatal(err)
	}

	wantPath := epRewardsMarkets + conditionID
	if rec.Path != wantPath {
		t.Errorf("path = %s, want %s", rec.Path, wantPath)
	}
	if got := rec.Query.Get("next_cursor"); got != polymarket.CursorStart {
		t.Errorf("next_cursor = %q, want %q", got, polymarket.CursorStart)
	}
	assertNoAuthHeaders(t, rec.Header)

	if len(details) != 1 {
		t.Fatalf("got %d rows, want 1", len(details))
	}
	d := details[0]
	if d.Question != "Will the Republican Party win the NY-11 House seat?" {
		t.Errorf("Question = %s", d.Question)
	}
	if len(d.Tokens) != 2 || d.Tokens[0].Outcome != "Yes" || string(d.Tokens[0].Price) != "0.935" {
		t.Errorf("Tokens = %+v", d.Tokens)
	}
	if len(d.RewardsConfig) != 1 || d.RewardsConfig[0].TotalDays != 173264 {
		t.Errorf("RewardsConfig = %+v", d.RewardsConfig)
	}
	if string(d.MarketCompetitiveness) != "461.474463" {
		t.Errorf("MarketCompetitiveness = %s, want 461.474463", d.MarketCompetitiveness)
	}
	if page.Count != 1 {
		t.Errorf("Count = %d, want 1", page.Count)
	}
}

// SDK-shaped example: exercising this endpoint needs level-2 credentials
// this client has not run against the live API, so the field names are
// transcribed rather than live-observed. See RewardsUserEarning's doc
// comment.
const userRewardsBody = `{
  "data": [
    {
      "date": "2026-08-15",
      "condition_id": "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda",
      "asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
      "maker_address": "0xAbC0000000000000000000000000000000dEf1",
      "earnings": 12.5,
      "asset_rate": 1
    }
  ],
  "next_cursor": "LTE=",
  "limit": 100,
  "count": 1
}`

func TestUserRewards(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, userRewardsBody)
	c := testL2Client(t, srv.URL)

	earnings, page, err := c.UserRewards(context.Background(), "2026-08-15", polymarket.SigEOA, "")
	if err != nil {
		t.Fatal(err)
	}

	if rec.Path != epRewardsUser {
		t.Errorf("path = %s, want %s", rec.Path, epRewardsUser)
	}
	if got := rec.Query.Get("date"); got != "2026-08-15" {
		t.Errorf("date = %q, want 2026-08-15", got)
	}
	if got := rec.Query.Get("signature_type"); got != "0" {
		t.Errorf("signature_type = %q, want 0", got)
	}
	if got := rec.Query.Get("next_cursor"); got != polymarket.CursorStart {
		t.Errorf("next_cursor = %q, want %q", got, polymarket.CursorStart)
	}
	assertL2Headers(t, rec.Header)

	if len(earnings) != 1 {
		t.Fatalf("got %d rows, want 1", len(earnings))
	}
	if string(earnings[0].Earnings) != "12.5" {
		t.Errorf("Earnings = %s, want 12.5", earnings[0].Earnings)
	}
	if page.Limit != 100 {
		t.Errorf("Limit = %d, want 100", page.Limit)
	}
}

const userRewardsTotalBody = `[
  {
    "date": "2026-08-15",
    "asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
    "maker_address": "0xAbC0000000000000000000000000000000dEf1",
    "earnings": 12.5,
    "asset_rate": 1
  }
]`

func TestUserRewardsTotal(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, userRewardsTotalBody)
	c := testL2Client(t, srv.URL)

	earnings, err := c.UserRewardsTotal(context.Background(), "2026-08-15", polymarket.SigPolyProxy)
	if err != nil {
		t.Fatal(err)
	}

	if rec.Path != epRewardsUserTotal {
		t.Errorf("path = %s, want %s", rec.Path, epRewardsUserTotal)
	}
	if got := rec.Query.Get("signature_type"); got != "1" {
		t.Errorf("signature_type = %q, want 1", got)
	}
	if _, ok := rec.Query["next_cursor"]; ok {
		t.Error("unpaginated endpoint carries a next_cursor parameter")
	}
	assertL2Headers(t, rec.Header)

	if len(earnings) != 1 || earnings[0].MakerAddress != "0xAbC0000000000000000000000000000000dEf1" {
		t.Errorf("earnings = %+v", earnings)
	}
}

const userRewardsPercentagesBody = `{"0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda": 0.42}`

func TestUserRewardsPercentages(t *testing.T) {
	var rec recordedRequest
	srv := recordingServer(t, &rec, http.StatusOK, userRewardsPercentagesBody)
	c := testL2Client(t, srv.URL)

	pct, err := c.UserRewardsPercentages(context.Background(), polymarket.SigEIP1271)
	if err != nil {
		t.Fatal(err)
	}

	if rec.Path != epRewardsUserPercentages {
		t.Errorf("path = %s, want %s", rec.Path, epRewardsUserPercentages)
	}
	if got := rec.Query.Get("signature_type"); got != "3" {
		t.Errorf("signature_type = %q, want 3", got)
	}
	assertL2Headers(t, rec.Header)

	const cid = "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda"
	if got, ok := pct[cid]; !ok || string(got) != "0.42" {
		t.Errorf("percentages[%s] = %v, ok=%v, want 0.42", cid, got, ok)
	}
}

const userRewardsMarketsBody = `{
  "data": [
    {
      "condition_id": "0x0001cb8c0b39aeb614ab9a43867595317f06ede9c011661513065c638fbbefda",
      "question": "Will the Republican Party win the NY-11 House seat?",
      "market_slug": "will-the-republican-party-win-the-ny-11-house-seat",
      "event_slug": "ny-11-house-election-winner",
      "image": "",
      "rewards_max_spread": 4.5,
      "rewards_min_size": 20,
      "market_competitiveness": 461.474463,
      "tokens": [
        {"token_id": "50868012450412588231700991321379235183301872220529434142919756787462014093776", "outcome": "Yes", "price": 0.935}
      ],
      "rewards_config": [
        {
          "asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
          "start_date": "2026-08-15",
          "end_date": "2500-12-31",
          "id": 71399,
          "rate_per_day": 2,
          "total_rewards": 0
        }
      ],
      "maker_address": "0xAbC0000000000000000000000000000000dEf1",
      "earning_percentage": 0.42,
      "earnings": [
        {"asset_address": "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", "earnings": 12.5, "asset_rate": 1}
      ]
    }
  ],
  "next_cursor": "LTE=",
  "limit": 100,
  "count": 1
}`

// userRewardsMarketsCase is one table-driven case for
// TestUserRewardsMarkets, checking which optional filters end up on the
// wire.
type userRewardsMarketsCase struct {
	name       string
	params     RewardsMarketsParams
	wantQuery  url.Values // exact optional keys expected: order_by, position, no_competition
	absentKeys []string   // optional keys that must be absent
}

var userRewardsMarketsCases = []userRewardsMarketsCase{
	{
		name: "no optional filters",
		params: RewardsMarketsParams{
			Date:          "2026-08-15",
			SignatureType: polymarket.SigEOA,
		},
		absentKeys: []string{"order_by", "position", "no_competition"},
	},
	{
		name: "every optional filter set",
		params: RewardsMarketsParams{
			Date:          "2026-08-15",
			SignatureType: polymarket.SigEOA,
			OrderBy:       "earnings",
			Position:      "maker",
			NoCompetition: true,
		},
		wantQuery: url.Values{
			"order_by":       {"earnings"},
			"position":       {"maker"},
			"no_competition": {"true"},
		},
	},
}

func TestUserRewardsMarkets(t *testing.T) {
	for _, tc := range userRewardsMarketsCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec recordedRequest
			srv := recordingServer(t, &rec, http.StatusOK, userRewardsMarketsBody)
			c := testL2Client(t, srv.URL)

			rows, page, err := c.UserRewardsMarkets(context.Background(), tc.params)
			if err != nil {
				t.Fatal(err)
			}

			if rec.Path != epRewardsUserMarkets {
				t.Errorf("path = %s, want %s", rec.Path, epRewardsUserMarkets)
			}
			assertL2Headers(t, rec.Header)

			for k, want := range tc.wantQuery {
				if got := rec.Query.Get(k); got != want[0] {
					t.Errorf("%s = %q, want %q", k, got, want[0])
				}
			}
			for _, k := range tc.absentKeys {
				if _, ok := rec.Query[k]; ok {
					t.Errorf("unexpected %s parameter: %q", k, rec.Query.Get(k))
				}
			}

			if len(rows) != 1 {
				t.Fatalf("got %d rows, want 1", len(rows))
			}
			r := rows[0]
			if string(r.EarningPercentage) != "0.42" {
				t.Errorf("EarningPercentage = %s, want 0.42", r.EarningPercentage)
			}
			if len(r.Earnings) != 1 || string(r.Earnings[0].Earnings) != "12.5" {
				t.Errorf("Earnings = %+v", r.Earnings)
			}
			if page.NextCursor != polymarket.CursorEnd {
				t.Errorf("NextCursor = %s, want %s", page.NextCursor, polymarket.CursorEnd)
			}
		})
	}
}

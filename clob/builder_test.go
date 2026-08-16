// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

const testBuilderCode = "0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417"

// builderTradesQueryCase is one BuilderTradeParams and the query values
// BuilderTrades must send for it.
type builderTradesQueryCase struct {
	name   string
	params BuilderTradeParams
	want   url.Values
}

// noAuthHeaders lists the level-2 headers a request must NOT carry. Both
// builder endpoints are public: no signature, no key, no wallet address.
var noAuthHeaders = []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE"}

// checkNoAuth fails the test if the request carries any level-2 header.
func checkNoAuth(t *testing.T, r *http.Request) {
	t.Helper()
	for _, h := range noAuthHeaders {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("request carries auth header %s = %q, want none", h, v)
		}
	}
}

func TestBuilderFees(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		want := "/fees/builder-fees/" + testBuilderCode
		if r.URL.Path != want {
			t.Errorf("path = %s, want %s", r.URL.Path, want)
		}
		w.Write([]byte(`{"code":"` + testBuilderCode + `","builder_maker_fee_rate_bps":50,"builder_taker_fee_rate_bps":100,"enabled":true}`))
	}))
	defer srv.Close()

	c := New(WithHost(srv.URL))
	got, err := c.BuilderFees(context.Background(), testBuilderCode)
	if err != nil {
		t.Fatal(err)
	}
	want := BuilderFeeRates{
		Code:            testBuilderCode,
		MakerFeeRateBps: 50,
		TakerFeeRateBps: 100,
		Enabled:         true,
	}
	if got != want {
		t.Errorf("BuilderFees() = %+v, want %+v", got, want)
	}
}

func TestBuilderFeesRejectsEmptyOrZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()
	c := New(WithHost(srv.URL))

	for _, code := range []string{"", polymarket.ZeroBytes32} {
		if _, err := c.BuilderFees(context.Background(), code); err == nil {
			t.Errorf("BuilderFees(%q): got nil error", code)
		}
	}
}

func TestBuilderTradesQuery(t *testing.T) {
	cases := []builderTradesQueryCase{
		{
			name:   "defaults",
			params: BuilderTradeParams{},
			want: url.Values{
				"builder_code": {testBuilderCode},
				"next_cursor":  {polymarket.CursorStart},
			},
		},
		{
			name:   "cursor passthrough",
			params: BuilderTradeParams{NextCursor: "abc123"},
			want: url.Values{
				"builder_code": {testBuilderCode},
				"next_cursor":  {"abc123"},
			},
		},
		{
			name: "every filter set",
			params: BuilderTradeParams{
				ID:           "trade-1",
				MakerAddress: "0xmaker",
				Market:       "0xmarket",
				AssetID:      "12345",
				Before:       "1700000000",
				After:        "1600000000",
				NextCursor:   "cursor-2",
			},
			want: url.Values{
				"builder_code":  {testBuilderCode},
				"id":            {"trade-1"},
				"maker_address": {"0xmaker"},
				"market":        {"0xmarket"},
				"asset_id":      {"12345"},
				"before":        {"1700000000"},
				"after":         {"1600000000"},
				"next_cursor":   {"cursor-2"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checkNoAuth(t, r)
				if r.URL.Path != epBuilderTrades {
					t.Errorf("path = %s, want %s", r.URL.Path, epBuilderTrades)
				}
				gotQuery = r.URL.Query()
				w.Write([]byte(`{"data":[],"next_cursor":"LTE=","limit":300,"count":0}`))
			}))
			defer srv.Close()

			c := New(WithHost(srv.URL))
			if _, _, err := c.BuilderTrades(context.Background(), testBuilderCode, tc.params); err != nil {
				t.Fatal(err)
			}
			for k, want := range tc.want {
				if got := gotQuery[k]; len(got) != 1 || got[0] != want[0] {
					t.Errorf("query[%s] = %v, want %v", k, got, want)
				}
			}
		})
	}
}

func TestBuilderTradesDecode(t *testing.T) {
	const body = `{
		"data": [{
			"id": "t1",
			"tradeType": "MATCH",
			"takerOrderHash": "0xhash",
			"builder": "0xbuilder",
			"market": "0xcondition",
			"assetId": "999",
			"side": "BUY",
			"size": "10",
			"sizeUsdc": "5.2",
			"price": "0.52",
			"status": "CONFIRMED",
			"outcome": "Yes",
			"outcomeIndex": 0,
			"owner": "0xowner",
			"maker": "0xmakeraddr",
			"transactionHash": "0xtx",
			"matchTime": "1700000000",
			"bucketIndex": 3,
			"fee": "0.01",
			"feeUsdc": "0.01",
			"builderFee": "0.005",
			"builderCode": "` + testBuilderCode + `",
			"createdAt": "2026-01-01T00:00:00Z",
			"updatedAt": "2026-01-01T00:00:01Z"
		}],
		"next_cursor": "LTE=",
		"limit": 300,
		"count": 1
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New(WithHost(srv.URL))
	trades, page, err := c.BuilderTrades(context.Background(), testBuilderCode, BuilderTradeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1", len(trades))
	}
	tr := trades[0]
	if tr.ID != "t1" || tr.AssetID != "999" || tr.OutcomeIndex != 0 || tr.BucketIndex != 3 {
		t.Errorf("decoded trade = %+v", tr)
	}
	if tr.BuilderCode != testBuilderCode {
		t.Errorf("BuilderCode = %s, want %s", tr.BuilderCode, testBuilderCode)
	}
	if page.NextCursor != polymarket.CursorEnd {
		t.Errorf("NextCursor = %s, want %s", page.NextCursor, polymarket.CursorEnd)
	}
	if page.Limit != 300 || page.Count != 1 {
		t.Errorf("Limit=%d Count=%d, want 300, 1", page.Limit, page.Count)
	}
}

func TestBuilderTradesRejectsEmptyOrZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()
	c := New(WithHost(srv.URL))

	for _, code := range []string{"", polymarket.ZeroBytes32} {
		if _, _, err := c.BuilderTrades(context.Background(), code, BuilderTradeParams{}); err == nil {
			t.Errorf("BuilderTrades(%q): got nil error", code)
		}
	}
}

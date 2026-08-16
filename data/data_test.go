// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package data

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// noAuthHeaders lists the level-2 headers a request must NOT carry. Every
// data-API endpoint is public: no signature, no key, no wallet address.
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

// checkQuery fails the test if got does not carry exactly the keys and
// values in want, no more and no fewer. An unexpected extra key usually means
// a query-building helper fired when a zero-value field should have made it
// omit the parameter.
func checkQuery(t *testing.T, got, want url.Values) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("query has %d keys %v, want %d keys %v", len(got), got, len(want), want)
	}
	for k, w := range want {
		g := got[k]
		if len(g) != len(w) {
			t.Errorf("query[%s] = %v, want %v", k, g, w)
			continue
		}
		for i := range w {
			if g[i] != w[i] {
				t.Errorf("query[%s] = %v, want %v", k, g, w)
				break
			}
		}
	}
}

// dataServer starts an httptest server and returns a Client pointed at it.
func dataServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(polymarket.WithHost(srv.URL))
}

func TestDataHealth(t *testing.T) {
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != epDataHealth {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataHealth)
		}
		w.Write([]byte(`{"data":"OK"}`))
	})
	got, err := c.DataHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "OK" {
		t.Errorf("DataHealth() = %q, want OK", got)
	}
}

// positionsQueryCase is one PositionsParams and the query GET /positions must
// carry for it.
type positionsQueryCase struct {
	name   string
	params PositionsParams
	want   url.Values
}

func TestPositionsQuery(t *testing.T) {
	cases := []positionsQueryCase{
		{
			name:   "user only",
			params: PositionsParams{User: "0xabc"},
			want:   url.Values{"user": {"0xabc"}},
		},
		{
			name:   "event ids join as a comma-separated list of integers",
			params: PositionsParams{User: "0xabc", Event: []int64{795581, 12345}},
			want:   url.Values{"user": {"0xabc"}, "eventId": {"795581,12345"}},
		},
		{
			name: "every filter set",
			params: PositionsParams{
				User:          "0xabc",
				Market:        []string{"0xm1", "0xm2"},
				SizeThreshold: "0",
				Redeemable:    true,
				Mergeable:     true,
				Limit:         50,
				Offset:        10,
				SortBy:        "CASHPNL",
				SortDirection: "ASC",
				Title:         "Tottenham",
			},
			want: url.Values{
				"user":          {"0xabc"},
				"market":        {"0xm1,0xm2"},
				"sizeThreshold": {"0"},
				"redeemable":    {"true"},
				"mergeable":     {"true"},
				"limit":         {"50"},
				"offset":        {"10"},
				"sortBy":        {"CASHPNL"},
				"sortDirection": {"ASC"},
				"title":         {"Tottenham"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
				checkNoAuth(t, r)
				if r.URL.Path != epDataPositions {
					t.Errorf("path = %s, want %s", r.URL.Path, epDataPositions)
				}
				gotQuery = r.URL.Query()
				w.Write([]byte(`[]`))
			})
			if _, err := c.Positions(context.Background(), tc.params); err != nil {
				t.Fatal(err)
			}
			checkQuery(t, gotQuery, tc.want)
		})
	}
}

func TestPositionsDecode(t *testing.T) {
	const body = `[{
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"asset": "99140635426353397249350183889599877135748112658097546731509034870452896140722",
		"conditionId": "0x408a4dcb6a03314bb13a1889a90aeca5fbf31bc20ca165056c514ae575924bd2",
		"size": 162963.4451,
		"avgPrice": 0.9979,
		"initialValue": 162627.5391,
		"grossInitialValue": 162644.036747,
		"entryFeesUsdc": 16.49763,
		"currentValue": 155630.09,
		"cashPnl": -6997.4491,
		"percentPnl": -4.3027,
		"totalBought": 162963.4451,
		"realizedPnl": 0,
		"percentRealizedPnl": -4.3027,
		"curPrice": 0.955,
		"redeemable": false,
		"mergeable": false,
		"title": "Tottenham Hotspur vs. TSG Hoffenheim: Tottenham Hotspur O/U 0.5",
		"slug": "clf-tot-tsg-2026-08-16-team-total-home-0pt5",
		"icon": "https://polymarket-upload.s3.us-east-2.amazonaws.com/soccer ball-bba4025f77.png",
		"eventId": "795581",
		"eventSlug": "clf-tot-tsg-2026-08-16-more-markets",
		"outcome": "Over",
		"outcomeIndex": 0,
		"oppositeOutcome": "Under",
		"oppositeAsset": "78365671558825289212558012953544030771644755581988339251527788113755000354802",
		"endDate": "2026-08-16",
		"negativeRisk": false
	}]`
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	got, err := c.Positions(context.Background(), PositionsParams{User: "0x204f..."})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	p := got[0]
	want := Position{
		ProxyWallet:        "0x204f72f35326db932158cba6adff0b9a1da95e14",
		Asset:              "99140635426353397249350183889599877135748112658097546731509034870452896140722",
		ConditionID:        "0x408a4dcb6a03314bb13a1889a90aeca5fbf31bc20ca165056c514ae575924bd2",
		Size:               "162963.4451",
		AvgPrice:           "0.9979",
		InitialValue:       "162627.5391",
		GrossInitialValue:  "162644.036747",
		EntryFeesUSDC:      "16.49763",
		CurrentValue:       "155630.09",
		CashPnl:            "-6997.4491",
		PercentPnl:         "-4.3027",
		TotalBought:        "162963.4451",
		RealizedPnl:        "0",
		PercentRealizedPnl: "-4.3027",
		CurPrice:           "0.955",
		Title:              "Tottenham Hotspur vs. TSG Hoffenheim: Tottenham Hotspur O/U 0.5",
		Slug:               "clf-tot-tsg-2026-08-16-team-total-home-0pt5",
		Icon:               "https://polymarket-upload.s3.us-east-2.amazonaws.com/soccer ball-bba4025f77.png",
		EventID:            "795581",
		EventSlug:          "clf-tot-tsg-2026-08-16-more-markets",
		Outcome:            "Over",
		OppositeOutcome:    "Under",
		OppositeAsset:      "78365671558825289212558012953544030771644755581988339251527788113755000354802",
		EndDate:            "2026-08-16",
	}
	if p != want {
		t.Errorf("Positions()[0] = %+v, want %+v", p, want)
	}
}

// exactCase is one decoded json.Number and the wire text it must still carry,
// character for character.
type exactCase struct {
	field string
	got   json.Number
	want  string
}

// TestPositionsDecodeExact feeds the decoder values a float64 mishandles and
// checks the text survives untouched. Each one breaks a different way:
//
//   - 0.29 has no exact binary form, so while it reprints as "0.29", any
//     arithmetic on it drifts — 0.29*100 is 28.999999999999996 at runtime.
//   - 1e-8 reprints as "1e-08" and 123456789.12345678 as
//     1.2345678912345678e+08: the value survives, the text does not.
//   - 0.070000000000000007 reprints as "0.07", silently losing digits the
//     server sent.
//
// A size read here becomes a size signed on an order, so a value that changes
// on the way through is a value that moves the wrong amount of money.
func TestPositionsDecodeExact(t *testing.T) {
	const body = `[{
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"asset": "99140635426353397249350183889599877135748112658097546731509034870452896140722",
		"size": 123456789.12345678,
		"avgPrice": 0.29,
		"initialValue": 1e-8,
		"currentValue": 0.1000000000000000055511151231257827,
		"cashPnl": -0.29,
		"curPrice": 0.070000000000000007
	}]`
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	got, err := c.Positions(context.Background(), PositionsParams{User: "0x204f..."})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	p := got[0]
	cases := []exactCase{
		{"Size", p.Size, "123456789.12345678"},
		{"AvgPrice", p.AvgPrice, "0.29"},
		{"InitialValue", p.InitialValue, "1e-8"},
		{"CurrentValue", p.CurrentValue, "0.1000000000000000055511151231257827"},
		{"CashPnl", p.CashPnl, "-0.29"},
		{"CurPrice", p.CurPrice, "0.070000000000000007"},
	}
	for _, tc := range cases {
		if tc.got.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}

	// The exact text is also exactly parseable: big.Rat takes it whole, where
	// a float64 would already have rounded it.
	avg, ok := new(big.Rat).SetString(p.AvgPrice.String())
	if !ok {
		t.Fatalf("big.Rat.SetString(%q) failed", p.AvgPrice)
	}
	if want := big.NewRat(29, 100); avg.Cmp(want) != 0 {
		t.Errorf("AvgPrice as a Rat = %s, want %s", avg, want)
	}

	// A key the server never sent leaves the empty string, which is not a
	// number: callers must check ok rather than read a silent zero.
	if p.TotalBought != "" {
		t.Errorf("TotalBought = %q, want the empty string for an absent key", p.TotalBought)
	}
	if _, ok := new(big.Rat).SetString(p.TotalBought.String()); ok {
		t.Error("big.Rat.SetString accepted the empty string, want it rejected")
	}
}

func TestClosedPositions(t *testing.T) {
	const body = `[{
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"asset": "12895713634933367223676558147527749758143822673340639855731583908529004951892",
		"conditionId": "0xe690620297dfea974d20df84b4cf90460e46a26ec864353482717b90509a3c0b",
		"avgPrice": 0.4499,
		"totalBought": 2367206.9474,
		"realizedPnl": 1156202.6574,
		"curPrice": 1,
		"title": "Will Germany win on 2026-06-25?",
		"slug": "fifwc-ecu-ger-2026-06-25-ger",
		"icon": "https://polymarket-upload.s3.us-east-2.amazonaws.com/soccer ball-bba4025f77.png",
		"eventSlug": "fifwc-ecu-ger-2026-06-25",
		"outcome": "No",
		"outcomeIndex": 1,
		"oppositeOutcome": "Yes",
		"oppositeAsset": "101279272797775203956659007084828831261331275405585994446661991301016814257223",
		"endDate": "2026-06-25",
		"timestamp": 1782425635
	}]`
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataClosedPositions {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataClosedPositions)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(body))
	})
	got, err := c.ClosedPositions(context.Background(), ClosedPositionsParams{
		User: "0x204f...", Limit: 2, SortBy: "TIMESTAMP", SortDirection: "DESC",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{
		"user": {"0x204f..."}, "limit": {"2"}, "sortBy": {"TIMESTAMP"}, "sortDirection": {"DESC"},
	})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	cp := got[0]
	// The wire sends curPrice as the bare integer 1, so the exact text is "1",
	// not "1.0" — json.Number reports what arrived, not a normalized form.
	if cp.RealizedPnl != "1156202.6574" || cp.OutcomeIndex != 1 || cp.Timestamp != 1782425635 || cp.CurPrice != "1" {
		t.Errorf("decoded ClosedPosition = %+v", cp)
	}
}

// fillsQueryCase is one FillsParams and the query GET /trades must carry for
// it.
type fillsQueryCase struct {
	name   string
	params FillsParams
	want   url.Values
}

func TestFillsQuery(t *testing.T) {
	cases := []fillsQueryCase{
		{
			name:   "platform-wide feed: every field left empty",
			params: FillsParams{},
			want:   url.Values{},
		},
		{
			name:   "IncludeMakerFills sends takerOnly=false",
			params: FillsParams{IncludeMakerFills: true},
			want:   url.Values{"takerOnly": {"false"}},
		},
		{
			name: "every filter set",
			params: FillsParams{
				Limit: 500, Offset: 20, IncludeMakerFills: true,
				FilterType: "CASH", FilterAmount: "100",
				Market: []string{"0xm1"}, Event: []int64{795581},
				User: "0xabc", Side: polymarket.Buy,
			},
			want: url.Values{
				"limit": {"500"}, "offset": {"20"}, "takerOnly": {"false"},
				"filterType": {"CASH"}, "filterAmount": {"100"},
				"market": {"0xm1"}, "eventId": {"795581"},
				"user": {"0xabc"}, "side": {"BUY"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != epDataTrades {
					t.Errorf("path = %s, want %s", r.URL.Path, epDataTrades)
				}
				gotQuery = r.URL.Query()
				w.Write([]byte(`[]`))
			})
			if _, err := c.Fills(context.Background(), tc.params); err != nil {
				t.Fatal(err)
			}
			checkQuery(t, gotQuery, tc.want)
		})
	}
}

func TestFillsDecode(t *testing.T) {
	const body = `[{
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"side": "BUY",
		"asset": "26553519739569408724710754771025892539347464160962015583021583647211378793594",
		"conditionId": "0x93ea2f1276cc65cc94d4f987a8d6ff409436f79005466c3a7e00fb791011d7ad",
		"size": 21,
		"price": 0.16,
		"timestamp": 1786861350,
		"title": "Bucheon FC 1995 vs. Jeonbuk Hyundai Motors FC: Jeonbuk Hyundai Motors FC 1st Half O/U 1.5",
		"slug": "kor-bch-jeo-2026-08-16-first-half-team-total-away-1pt5",
		"icon": "https://polymarket-upload.s3.us-east-2.amazonaws.com/k-league-87b53492f4.png",
		"eventSlug": "kor-bch-jeo-2026-08-16-more-markets",
		"outcome": "Over",
		"outcomeIndex": 999,
		"name": "swisstony",
		"pseudonym": "Frail-Possible",
		"bio": "So long, and thanks for all the fish",
		"profileImage": "",
		"profileImageOptimized": "",
		"transactionHash": "0xdb79d27479869fbbea544110dc4120a945774825369b1a321ec507c4b15a034d"
	}]`
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	got, err := c.Fills(context.Background(), FillsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	f := got[0]
	if f.Side != polymarket.Buy {
		t.Errorf("Side = %q, want BUY", f.Side)
	}
	// 999 is the platform's own sentinel for "not applicable" on some rows
	// of the global feed, not a real outcome index; it must decode as-is.
	if f.OutcomeIndex != 999 {
		t.Errorf("OutcomeIndex = %d, want 999", f.OutcomeIndex)
	}
	if f.Size != "21" || f.Price != "0.16" || f.TransactionHash != "0xdb79d27479869fbbea544110dc4120a945774825369b1a321ec507c4b15a034d" {
		t.Errorf("decoded Fill = %+v", f)
	}
}

// activityQueryCase is one ActivityParams and the query GET /activity must
// carry for it.
type activityQueryCase struct {
	name   string
	params ActivityParams
	want   url.Values
}

func TestActivityQuery(t *testing.T) {
	cases := []activityQueryCase{
		{
			name:   "required only",
			params: ActivityParams{User: "0xabc"},
			want:   url.Values{"user": {"0xabc"}},
		},
		{
			name:   "type list joins as a comma-separated list",
			params: ActivityParams{User: "0xabc", Type: []ActivityType{ActivityRedeem, ActivityReward}},
			want:   url.Values{"user": {"0xabc"}, "type": {"REDEEM,REWARD"}},
		},
		{
			name: "every filter set",
			params: ActivityParams{
				User: "0xabc", Limit: 3, Offset: 1,
				Market: []string{"0xm1"}, Event: []int64{795581},
				Start: 1700000000, End: 1800000000,
				SortBy: "CASH", SortDirection: "ASC", Side: polymarket.Sell,
			},
			want: url.Values{
				"user": {"0xabc"}, "limit": {"3"}, "offset": {"1"},
				"market": {"0xm1"}, "eventId": {"795581"},
				"start": {"1700000000"}, "end": {"1800000000"},
				"sortBy": {"CASH"}, "sortDirection": {"ASC"}, "side": {"SELL"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != epDataActivity {
					t.Errorf("path = %s, want %s", r.URL.Path, epDataActivity)
				}
				gotQuery = r.URL.Query()
				w.Write([]byte(`[]`))
			})
			if _, err := c.Activity(context.Background(), tc.params); err != nil {
				t.Fatal(err)
			}
			checkQuery(t, gotQuery, tc.want)
		})
	}
}

func TestActivityDecode(t *testing.T) {
	// A REDEEM row: side is the empty string and price is 0, which are only
	// meaningful for TRADE rows.
	const body = `[{
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"timestamp": 1786861677,
		"conditionId": "0x2db661f1c7901e525b843cd94bccfb514e7a1570e456065f4eb0268311ca5d9f",
		"type": "REDEEM",
		"size": 20,
		"usdcSize": 0,
		"transactionHash": "0x5de796414ba81fa80d141d23a710b90c187c2cd667417e16a0340e36ad9c5363",
		"price": 0,
		"asset": "78336263860524650031155400600701777442313407862548216108095196338559464173193",
		"side": "",
		"outcomeIndex": 1,
		"title": "Deportivo Saprissa vs. CS Cartaginés: 1st Half O/U 2.5",
		"slug": "fpd-sap-car-2026-08-15-first-half-total-2pt5",
		"icon": "https://polymarket-upload.s3.us-east-2.amazonaws.com/soccer-leagues/fpd.png",
		"eventSlug": "fpd-sap-car-2026-08-15-more-markets",
		"outcome": "Under",
		"name": "swisstony",
		"pseudonym": "Frail-Possible",
		"bio": "So long, and thanks for all the fish",
		"profileImage": "",
		"profileImageOptimized": ""
	}]`
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	got, err := c.Activity(context.Background(), ActivityParams{User: "0x204f..."})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	a := got[0]
	if a.Type != ActivityRedeem {
		t.Errorf("Type = %q, want REDEEM", a.Type)
	}
	if a.Side != "" {
		t.Errorf("Side = %q, want empty on a non-TRADE row", a.Side)
	}
	// A non-TRADE row carries the number zero, not a missing field, so Price
	// and USDCSize read "0" — distinct from the empty string a decoder leaves
	// behind when the key is absent.
	if a.Price != "0" || a.USDCSize != "0" || a.Size != "20" || a.Timestamp != 1786861677 {
		t.Errorf("decoded Activity = %+v", a)
	}
}

func TestHolders(t *testing.T) {
	const body = `[{
		"token": "99140635426353397249350183889599877135748112658097546731509034870452896140722",
		"holders": [{
			"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
			"bio": "So long, and thanks for all the fish",
			"asset": "99140635426353397249350183889599877135748112658097546731509034870452896140722",
			"pseudonym": "Frail-Possible",
			"amount": 162963.4451,
			"displayUsernamePublic": true,
			"outcomeIndex": 0,
			"name": "swisstony",
			"profileImage": "",
			"profileImageOptimized": "",
			"verified": false
		}]
	}]`
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataHolders {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataHolders)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(body))
	})
	got, err := c.Holders(context.Background(), HoldersParams{
		Market: []string{"0x408a...", "0xother"}, Limit: 3, MinBalance: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{
		"market": {"0x408a...,0xother"}, "limit": {"3"}, "minBalance": {"1"},
	})
	if len(got) != 1 || len(got[0].Holders) != 1 {
		t.Fatalf("decoded TokenHolders = %+v", got)
	}
	h := got[0].Holders[0]
	if h.Verified {
		t.Errorf("Verified = %v, want false", h.Verified)
	}
	if h.Amount != "162963.4451" || !h.DisplayUsernamePublic {
		t.Errorf("decoded Holder = %+v", h)
	}
}

func TestMarketPositions(t *testing.T) {
	// currPrice (no "e") is this endpoint's own spelling, unlike Position's
	// curPrice — the asymmetry is the live API's, not a transcription slip.
	const body = `[{
		"token": "78365671558825289212558012953544030771644755581988339251527788113755000354802",
		"positions": [{
			"proxyWallet": "0xc73bedf5a0b44e29728a204f6dc633f1a235f046",
			"name": "DwBh1",
			"profileImage": "",
			"verified": false,
			"asset": "78365671558825289212558012953544030771644755581988339251527788113755000354802",
			"conditionId": "0x408a4dcb6a03314bb13a1889a90aeca5fbf31bc20ca165056c514ae575924bd2",
			"avgPrice": 0.0015,
			"size": 160067.0976,
			"currPrice": 0.045,
			"currentValue": 7203.0193,
			"cashPnl": 6960.1975,
			"totalBought": 160067.0976,
			"realizedPnl": 0,
			"totalPnl": 6960.1975,
			"outcome": "Under",
			"outcomeIndex": 1
		}]
	}]`
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataMarketPositions {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataMarketPositions)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(body))
	})
	got, err := c.MarketPositions(context.Background(), MarketPositionsParams{
		Market: "0x408a...", Status: "ALL", SortBy: "TOTAL_PNL", SortDirection: "DESC", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{
		"market": {"0x408a..."}, "status": {"ALL"}, "sortBy": {"TOTAL_PNL"},
		"sortDirection": {"DESC"}, "limit": {"2"},
	})
	if len(got) != 1 || len(got[0].Positions) != 1 {
		t.Fatalf("decoded TokenMarketPositions = %+v", got)
	}
	mp := got[0].Positions[0]
	if mp.CurrPrice != "0.045" {
		t.Errorf("CurrPrice = %v, want 0.045", mp.CurrPrice)
	}
	if mp.TotalPnl != "6960.1975" || mp.Name != "DwBh1" {
		t.Errorf("decoded MarketPosition = %+v", mp)
	}
}

func TestValue(t *testing.T) {
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataValue {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataValue)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(`[{"user":"0x204f72f35326db932158cba6adff0b9a1da95e14","value":451161.4413}]`))
	})
	got, err := c.Value(context.Background(), "0x204f...", []string{"0xm1", "0xm2"})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"user": {"0x204f..."}, "market": {"0xm1,0xm2"}})
	want := []PortfolioValue{{User: "0x204f72f35326db932158cba6adff0b9a1da95e14", Value: "451161.4413"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("Value() = %+v, want %+v", got, want)
	}
}

func TestTradedCount(t *testing.T) {
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataTraded {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataTraded)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"user":"0x204f72f35326db932158cba6adff0b9a1da95e14","traded":184244}`))
	})
	got, err := c.TradedCount(context.Background(), "0x204f...")
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"user": {"0x204f..."}})
	want := Traded{User: "0x204f72f35326db932158cba6adff0b9a1da95e14", Traded: 184244}
	if got != want {
		t.Errorf("TradedCount() = %+v, want %+v", got, want)
	}
}

func TestOpenInterest(t *testing.T) {
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataOpenInterest {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataOpenInterest)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(`[{"market":"0x408a4dcb6a03314bb13a1889a90aeca5fbf31bc20ca165056c514ae575924bd2","value":167381.403083}]`))
	})
	got, err := c.OpenInterest(context.Background(), []string{"0x408a..."})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"market": {"0x408a..."}})
	if len(got) != 1 || got[0].Value != "167381.403083" {
		t.Errorf("OpenInterest() = %+v", got)
	}
}

func TestLiveVolume(t *testing.T) {
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataLiveVolume {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataLiveVolume)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(`[{"total":281682.5565499999,"markets":[
			{"market":"0x408a4dcb6a03314bb13a1889a90aeca5fbf31bc20ca165056c514ae575924bd2","value":167526.646935},
			{"market":"0xfcc2f915357c9c90ec8c53988e826fc97217e8584c2e7a223d5688c5e6d2df53","value":39600.354381}
		]}]`))
	})
	got, err := c.LiveVolume(context.Background(), 795581)
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"id": {"795581"}})
	if got.Total != "281682.5565499999" || len(got.Markets) != 2 {
		t.Fatalf("LiveVolume() = %+v", got)
	}
	if got.Markets[0].Value != "167526.646935" {
		t.Errorf("Markets[0] = %+v", got.Markets[0])
	}
}

// TestLiveVolumeEmptyResponse checks the defensive path: the live endpoint
// always answers with exactly one element even for an unknown event id
// (verified live 2026-08-16, id=999999999 returned
// [{"total":0,"markets":[]}], not []), but LiveVolume must still degrade to
// the zero value rather than panic if the server ever answers with none.
func TestLiveVolumeEmptyResponse(t *testing.T) {
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	got, err := c.LiveVolume(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing decoded, so Total is the zero json.Number — the empty string,
	// not "0". A server that really reports zero volume sends "0", and the two
	// cases stay distinguishable.
	if got.Total != "" || len(got.Markets) != 0 {
		t.Errorf("LiveVolume() = %+v, want the zero value", got)
	}
}

func TestOtherSizes(t *testing.T) {
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataOther {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataOther)
		}
		gotQuery = r.URL.Query()
		// Production never returned a populated row for this report (every
		// id+user combination tried gave 200 []); this row is synthesized
		// from the OpenAPI spec's documented OtherSize shape to check the
		// decode still lines up field-for-field.
		w.Write([]byte(`[{"id":795581,"user":"0x204f72f35326db932158cba6adff0b9a1da95e14","size":12.5}]`))
	})
	got, err := c.OtherSizes(context.Background(), 795581, "0x204f...")
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"id": {"795581"}, "user": {"0x204f..."}})
	want := OtherSize{ID: 795581, User: "0x204f72f35326db932158cba6adff0b9a1da95e14", Size: "12.5"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("OtherSizes() = %+v, want [%+v]", got, want)
	}
}

// leaderboardQueryCase is one LeaderboardParams and the query
// GET /v1/leaderboard must carry for it, checking that the enum types render
// their wire strings correctly.
type leaderboardQueryCase struct {
	name   string
	params LeaderboardParams
	want   url.Values
}

func TestLeaderboardQuery(t *testing.T) {
	cases := []leaderboardQueryCase{
		{
			name:   "no filters",
			params: LeaderboardParams{},
			want:   url.Values{},
		},
		{
			name: "every filter set",
			params: LeaderboardParams{
				Category: LeaderboardCategorySports, TimePeriod: LeaderboardPeriodAll,
				OrderBy: LeaderboardOrderByVol, Limit: 5, Offset: 0,
				User: "0x204f...", UserName: "swisstony",
			},
			want: url.Values{
				"category": {"SPORTS"}, "timePeriod": {"ALL"}, "orderBy": {"VOL"},
				"limit": {"5"}, "user": {"0x204f..."}, "userName": {"swisstony"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != epDataLeaderboard {
					t.Errorf("path = %s, want %s", r.URL.Path, epDataLeaderboard)
				}
				gotQuery = r.URL.Query()
				w.Write([]byte(`[]`))
			})
			if _, err := c.Leaderboard(context.Background(), tc.params); err != nil {
				t.Fatal(err)
			}
			checkQuery(t, gotQuery, tc.want)
		})
	}
}

func TestLeaderboardDecode(t *testing.T) {
	const body = `[{
		"rank": "1",
		"proxyWallet": "0x204f72f35326db932158cba6adff0b9a1da95e14",
		"userName": "swisstony",
		"xUsername": "",
		"verifiedBadge": false,
		"vol": 1803373946.6578584,
		"pnl": 23373702.173558038,
		"profileImage": ""
	}]`
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	got, err := c.Leaderboard(context.Background(), LeaderboardParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	e := got[0]
	// rank is a decimal string on the wire, not a JSON number.
	if e.Rank != "1" || e.UserName != "swisstony" || e.Vol != "1803373946.6578584" {
		t.Errorf("decoded TraderLeaderboardEntry = %+v", e)
	}
}

func TestBuilderLeaderboard(t *testing.T) {
	const body = `[{
		"rank": "1",
		"builder": "traderline",
		"builderCode": "0x6b0e773fada0a2ec67c956b25a737d353a534ea33db56c717ba7854346c67984",
		"volume": 1637556.2775469997,
		"activeUsers": 86,
		"verified": true,
		"builderLogo": "https://polymarket-upload.s3.us-east-2.amazonaws.com/logo.png"
	}]`
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataBuildersLeaderboard {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataBuildersLeaderboard)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(body))
	})
	got, err := c.BuilderLeaderboard(context.Background(), BuilderLeaderboardParams{
		TimePeriod: LeaderboardPeriodWeek, Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"timePeriod": {"WEEK"}, "limit": {"3"}})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	e := got[0]
	if e.BuilderCode != "0x6b0e773fada0a2ec67c956b25a737d353a534ea33db56c717ba7854346c67984" {
		t.Errorf("BuilderCode = %q", e.BuilderCode)
	}
	// 1637556.2775469997 needs 17 significant digits; a float64 round trip
	// reprints it as 1.6375562775469997e+06, so asserting the text also
	// asserts that nothing reformatted it.
	if e.Volume != "1637556.2775469997" {
		t.Errorf("Volume = %q, want 1637556.2775469997", e.Volume)
	}
	if e.Builder != "traderline" || e.ActiveUsers != 86 || !e.Verified {
		t.Errorf("decoded BuilderLeaderboardEntry = %+v", e)
	}
}

func TestBuilderVolume(t *testing.T) {
	const body = `[
		{"dt":"2026-08-16T00:00:00Z","builder":"traderline","builderCode":"0xabc","builderLogo":"","verified":true,"volume":1637031.3575469996,"activeUsers":86,"rank":"1"},
		{"dt":"2026-08-16T00:00:00Z","builder":"betmoar","builderCode":"0xdef","builderLogo":"","verified":true,"volume":1218045.791132,"activeUsers":96,"rank":"2"}
	]`
	var gotQuery url.Values
	c := dataServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epDataBuildersVolume {
			t.Errorf("path = %s, want %s", r.URL.Path, epDataBuildersVolume)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(body))
	})
	got, err := c.BuilderVolume(context.Background(), LeaderboardPeriodWeek)
	if err != nil {
		t.Fatal(err)
	}
	checkQuery(t, gotQuery, url.Values{"timePeriod": {"WEEK"}})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Date != "2026-08-16T00:00:00Z" || got[0].Rank != "1" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Builder != "betmoar" || got[1].Volume != "1218045.791132" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

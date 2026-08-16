// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// ---------------------------------------------------------------------------
// Test fixtures: one real Market and one real Event, captured live from
// https://gamma-api.polymarket.com and compacted, so the transcription tests
// below decode the actual wire shape rather than a hand-written guess.

// marketJSON is a live GET /markets/559651?include_tag=true response
// ("Xi Jinping out before 2027?"), captured 2026-08-16. It carries
// ClobRewards, FeeSchedule, PositionIDs and Tags — the object-typed and
// native-array fields — alongside the stringified Outcomes/OutcomePrices/
// ClobTokenIDs/UMAResolutionStatuses.
const marketJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/Market.json","id":"559651","question":"Xi Jinping out before 2027?","conditionId":"0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7","slug":"xi-jinping-out-before-2027","resolutionSource":"","endDate":"2026-12-31T00:00:00Z","liquidity":"237253.02375","startDate":"2025-07-03T20:37:00.228Z","image":"https://polymarket-upload.s3.us-east-2.amazonaws.com/xi-jinping-out-in-2025-EjF4SM20eaa3.jpg","icon":"https://polymarket-upload.s3.us-east-2.amazonaws.com/xi-jinping-out-in-2025-EjF4SM20eaa3.jpg","description":"This market will resolve to \"Yes\" if China's General Secretary of the Communist Party, Xi Jinping, is removed from power for any length of time between July 3, 2025, and December 31, 2026, 11:59 PM ET. Otherwise, this market will resolve to \"No\".","outcomes":"[\"Yes\", \"No\"]","outcomePrices":"[\"0.0455\", \"0.9545\"]","volume":"12071015.216477996","active":true,"closed":false,"marketMakerAddress":"","createdAt":"2025-07-03T20:25:56.889606Z","updatedAt":"2026-08-16T08:48:06.699066Z","new":false,"featured":false,"submitted_by":"0x91430CaD2d3975766499717fA0D66A78D814E5c5","archived":false,"resolvedBy":"0x157Ce2d672854c848c9b79C49a8Cc6cc89176a49","restricted":true,"groupItemTitle":"","groupItemThreshold":"0","questionID":"0x1d925c6933062c2e38031293612d8680ffa097c5d3ba2f87a8ecc565bd47183e","enableOrderBook":true,"orderPriceMinTickSize":0.001,"orderMinSize":5,"volumeNum":12071015.216477996,"liquidityNum":237253.02375,"endDateIso":"2026-12-31","startDateIso":"2025-07-03","hasReviewedDates":true,"volume24hr":14542.225487,"volume1wk":119459.04871600003,"volume1mo":719704.5928750002,"volume1yr":11148889.007490002,"clobTokenIds":"[\"32338220190071351435772801779725302244575775216413325951443816017994629993401\", \"25659310674993675562345759665114759892400026242514633218387667107987341231962\"]","positionIds":["895257453734540292493143124621755605615887197499956109032745153687078305792","895257453734540292493143124621755605615887197499956109032745153687078305793"],"comboStatus":"pending","umaBond":"500","umaReward":"5","volume24hrClob":14542.225487,"volume1wkClob":119459.04871600003,"volume1moClob":719704.5928750002,"volume1yrClob":11148889.007490002,"volumeClob":12071015.216477996,"liquidityClob":237253.02375,"makerBaseFee":1000,"takerBaseFee":1000,"customLiveness":0,"acceptingOrders":true,"negRisk":false,"negRiskRequestID":"","ready":false,"funded":false,"acceptingOrdersTimestamp":"2025-07-03T20:36:33Z","tags":[{"id":"366","label":"world affairs","slug":"world-affairs","publishedAt":"2023-11-02 22:05:44.425+00","createdAt":"2023-11-02T22:05:44.48Z","updatedAt":"2026-04-17T20:47:56.524437Z","requiresTranslation":false},{"id":"100265","label":"Geopolitics","slug":"geopolitics","forceShow":true,"createdAt":"2024-06-12T20:13:03.615956Z","updatedAt":"2026-04-17T20:49:04.209055Z","requiresTranslation":false},{"id":"101970","label":"World","slug":"world","forceShow":false,"createdAt":"2025-03-19T23:36:08.498099Z","updatedAt":"2026-04-17T17:18:59.135061Z","requiresTranslation":false},{"id":"102458","label":"Earn 4%","slug":"earn-4","forceShow":false,"createdAt":"2025-08-01T13:31:11.928744Z","updatedAt":"2026-04-17T21:09:22.871226Z","isCarousel":false,"requiresTranslation":false},{"id":"101253","label":"Macro Geopolitics","slug":"macro-geopolitics","forceShow":false,"createdAt":"2024-11-13T01:49:20.436741Z","updatedAt":"2026-04-17T17:19:59.236496Z","requiresTranslation":false},{"id":"103715","label":"HFC","slug":"hfc","forceShow":false,"createdAt":"2026-02-10T00:17:21.422175Z","updatedAt":"2026-04-17T20:55:08.357992Z","isCarousel":false,"requiresTranslation":false}],"cyom":false,"competitive":0.8287955052762158,"pagerDutyNotificationEnabled":false,"approved":true,"clobRewards":[{"id":"303430","conditionId":"0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7","assetAddress":"0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174","rewardsAmount":0,"rewardsDailyRate":10,"startDate":"2026-04-30","endDate":"2500-12-31"}],"rewardsMinSize":200,"rewardsMaxSpread":3.5,"spread":0.001,"oneDayPriceChange":0.001,"oneWeekPriceChange":-0.001,"oneMonthPriceChange":-0.003,"oneYearPriceChange":-0.1795,"lastTradePrice":0.045,"bestBid":0.045,"bestAsk":0.046,"automaticallyActive":true,"clearBookOnStart":true,"manualActivation":false,"negRiskOther":false,"umaResolutionStatuses":"[]","pendingDeployment":false,"deploying":false,"deployingTimestamp":"2025-07-03T20:35:48.270879Z","rfqEnabled":false,"holdingRewardsEnabled":true,"feesEnabled":true,"requiresTranslation":false,"feeType":"politics_fees","feeSchedule":{"exponent":1,"rate":0.04,"takerOnly":true,"rebateRate":0.25},"version":"v1"}`

// eventJSON is a live GET /events/2890 response (an NBA game event with one
// market, one series and one tag), captured 2026-08-16. It cross-checks the
// Market-inside-Event nesting and the Market/Event field-type asymmetries
// documented on Event's doc comment.
const eventJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/Event.json","id":"2890","ticker":"nba-will-the-mavericks-beat-the-grizzlies-by-more-than-5pt5-points-in-their-december-4-matchup","slug":"nba-will-the-mavericks-beat-the-grizzlies-by-more-than-5pt5-points-in-their-december-4-matchup","title":"NBA: Will the Mavericks beat the Grizzlies by more than 5.5 points in their December 4 matchup?","description":"desc","resolutionSource":"https://www.nba.com/games","startDate":"2021-12-04T00:00:00Z","creationDate":"2021-12-04T00:00:00Z","endDate":"2021-12-04T00:00:00Z","image":"https://example.com/i.png","icon":"https://example.com/i.png","active":true,"closed":true,"archived":false,"new":false,"featured":false,"restricted":false,"liquidity":0,"volume":1335.05,"openInterest":0,"sortBy":"ascending","category":"Sports","published_at":"2022-07-27 14:40:02.064+00","createdAt":"2022-07-27T14:40:02.074Z","updatedAt":"2026-07-02T07:14:01.478072Z","competitive":0,"volume24hr":0,"volume1wk":0,"volume1mo":0,"volume1yr":0,"liquidityAmm":0,"liquidityClob":0,"commentCount":8125,"markets":[{"id":"239826","question":"NBA: Will the Mavericks beat the Grizzlies by more than 5.5 points in their December 4 matchup?","conditionId":"0x064d33e3f5703792aafa92bfb0ee10e08f461b1b34c02c1f02671892ede1609a","slug":"nba-will-the-mavericks-beat-the-grizzlies-by-more-than-5pt5-points-in-their-december-4-matchup","resolutionSource":"https://www.nba.com/games","endDate":"2021-12-04T00:00:00Z","category":"Sports","liquidity":"50.000009","startDate":"2021-12-04T19:35:03.796Z","fee":"20000000000000000","image":"https://example.com/m.png","icon":"https://example.com/m.png","description":"desc","outcomes":"[\"Yes\", \"No\"]","outcomePrices":"[\"0.0000004113679809846114013590098187297978\", \"0.9999995886320190153885986409901813\"]","volume":"1335.045385","active":true,"marketType":"normal","closed":true,"marketMakerAddress":"0x9c568Ce9a316e7CF9bCCA352b409dfDdCD9b2C08","updatedBy":15,"createdAt":"2021-12-04T10:33:13.541Z","updatedAt":"2024-04-24T23:35:51.063381Z","closedTime":"2021-12-05 20:37:01+00","wideFormat":false,"new":false,"sentDiscord":false,"featured":false,"submitted_by":"0x790A4485e5198763C0a34272698ed0cd9506949B","twitterCardLocation":"https://example.com/t.png","twitterCardLastRefreshed":"1638736245595","archived":false,"resolvedBy":"0x0dD333859cF16942dd333D7570D839b8946Ac221","restricted":false,"volumeNum":1335.05,"liquidityNum":50,"endDateIso":"2021-12-04","startDateIso":"2021-12-04","hasReviewedDates":true,"readyForCron":true,"volume24hr":0,"volume1wk":0,"volume1mo":0,"volume1yr":0,"clobTokenIds":"[\"28182404005967940652495463228537840901055649726248190462854914416579180110833\", \"47044845753450022047436429968808601130811164131571549682541703866165095016290\"]","comboStatus":"disabled","fpmmLive":true,"volume1wkAmm":0,"volume1moAmm":0,"volume1yrAmm":0,"volume1wkClob":0,"volume1moClob":0,"volume1yrClob":0,"creator":"","ready":false,"funded":false,"cyom":false,"competitive":0,"pagerDutyNotificationEnabled":false,"approved":true,"rewardsMinSize":0,"rewardsMaxSpread":0,"spread":1,"oneDayPriceChange":0,"oneHourPriceChange":0,"oneWeekPriceChange":0,"oneMonthPriceChange":0,"oneYearPriceChange":0,"lastTradePrice":0,"bestBid":0,"bestAsk":1,"clearBookOnStart":true,"manualActivation":false,"negRiskOther":false,"umaResolutionStatuses":"[]","pendingDeployment":false,"deploying":false,"rfqEnabled":false,"holdingRewardsEnabled":false,"feesEnabled":false,"requiresTranslation":false,"feeType":null,"version":"v1"}],"series":[{"id":"2","ticker":"nba","slug":"nba","title":"NBA","seriesType":"single","recurrence":"daily","image":"https://example.com/s.png","icon":"https://example.com/s.png","layout":"default","active":true,"closed":false,"archived":false,"new":false,"featured":false,"restricted":true,"publishedAt":"2023-01-30 17:13:39.006+00","createdBy":"15","updatedBy":"15","createdAt":"2022-10-13T00:36:01.131Z","updatedAt":"2026-08-16T08:48:54.264477Z","commentsEnabled":false,"competitive":"0","volume24hr":11.073004,"startDate":"2021-01-01T17:00:00Z","commentCount":6279,"requiresTranslation":false}],"tags":[{"id":"100215","label":"All","slug":"all","forceShow":false,"updatedAt":"2026-04-17T20:23:54.340488Z","requiresTranslation":false}],"cyom":false,"closedTime":"2022-07-27T14:40:02.074Z","showAllOutcomes":false,"showMarketImages":true,"enableNegRisk":false,"seriesSlug":"nba","negRiskAugmented":false,"pendingDeployment":false,"deploying":false,"requiresTranslation":false,"eventMetadata":{"context_requires_regen":true},"version":"v1"}`

// ---------------------------------------------------------------------------
// Transcription: prove the 171-field Market and 108-field Event structs (and
// the nested Tag/Series/ClobRewards/FeeSchedule types) match the live wire
// shape by decoding a real captured response with DisallowUnknownFields. A
// stray or misspelled json tag anywhere in these structs fails this test.

func TestMarketTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(marketJSON))
	dec.DisallowUnknownFields()
	var m Market
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decoding live Market fixture: %v", err)
	}

	if m.ID != "559651" {
		t.Errorf("ID = %q, want 559651", m.ID)
	}
	if m.ConditionID != "0xa467b14d51f01b957109d9cbb1d6c124fab2a089d52ed8f471d23c2812e743b7" {
		t.Errorf("ConditionID = %q", m.ConditionID)
	}
	// Liquidity is a decimal STRING on Market; LiquidityNum is the same value
	// as a number. Both must decode, into different Go types.
	if m.Liquidity != "237253.02375" {
		t.Errorf("Liquidity = %q, want the decimal string", m.Liquidity)
	}
	if m.LiquidityNum != 237253.02375 {
		t.Errorf("LiquidityNum = %v, want 237253.02375", m.LiquidityNum)
	}
	if m.ComboStatus != ComboStatusPending {
		t.Errorf("ComboStatus = %q, want %q", m.ComboStatus, ComboStatusPending)
	}
	if m.Version != ProtocolVersionV1 {
		t.Errorf("Version = %q, want %q", m.Version, ProtocolVersionV1)
	}
	if len(m.Tags) != 6 {
		t.Fatalf("len(Tags) = %d, want 6", len(m.Tags))
	}
	if m.Tags[0].Label != "world affairs" {
		t.Errorf("Tags[0].Label = %q", m.Tags[0].Label)
	}
	if len(m.PositionIDs) != 2 {
		t.Errorf("len(PositionIDs) = %d, want 2 (native array, not stringified)", len(m.PositionIDs))
	}
	if len(m.ClobRewards) != 1 || m.ClobRewards[0].RewardsDailyRate != 10 {
		t.Errorf("ClobRewards = %+v, want one entry with RewardsDailyRate 10", m.ClobRewards)
	}
	if m.FeeSchedule.Rate != 0.04 || !m.FeeSchedule.TakerOnly {
		t.Errorf("FeeSchedule = %+v", m.FeeSchedule)
	}

	outcomes, err := m.DecodeOutcomes()
	if err != nil {
		t.Fatalf("DecodeOutcomes: %v", err)
	}
	if len(outcomes) != 2 || outcomes[0] != "Yes" || outcomes[1] != "No" {
		t.Errorf("DecodeOutcomes() = %v, want [Yes No]", outcomes)
	}
	prices, err := m.DecodeOutcomePrices()
	if err != nil {
		t.Fatalf("DecodeOutcomePrices: %v", err)
	}
	if len(prices) != 2 || prices[0] != "0.0455" {
		t.Errorf("DecodeOutcomePrices() = %v", prices)
	}
	tokenIDs, err := m.DecodeClobTokenIDs()
	if err != nil {
		t.Fatalf("DecodeClobTokenIDs: %v", err)
	}
	if len(tokenIDs) != 2 {
		t.Errorf("DecodeClobTokenIDs() = %v, want 2 entries", tokenIDs)
	}
	statuses, err := m.DecodeUMAResolutionStatuses()
	if err != nil {
		t.Fatalf("DecodeUMAResolutionStatuses: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("DecodeUMAResolutionStatuses() = %v, want empty", statuses)
	}
}

func TestEventTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(eventJSON))
	dec.DisallowUnknownFields()
	var e Event
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("decoding live Event fixture: %v", err)
	}

	if e.ID != "2890" {
		t.Errorf("ID = %q, want 2890", e.ID)
	}
	// Event.Liquidity and Event.Volume are numbers; Market's same-named
	// fields are decimal strings — see Event's doc comment.
	if e.Liquidity != 0 {
		t.Errorf("Liquidity = %v, want 0", e.Liquidity)
	}
	if e.Volume != 1335.05 {
		t.Errorf("Volume = %v, want 1335.05", e.Volume)
	}
	if e.PublishedAt != "2022-07-27 14:40:02.064+00" {
		t.Errorf("PublishedAt = %q, want the raw non-RFC3339 wire value", e.PublishedAt)
	}
	if got, want := e.EventMetadata["context_requires_regen"], true; got != want {
		t.Errorf("EventMetadata[context_requires_regen] = %v, want %v", got, want)
	}
	if len(e.Markets) != 1 {
		t.Fatalf("len(Markets) = %d, want 1", len(e.Markets))
	}
	mkt := e.Markets[0]
	// Market.UpdatedBy is int64, unlike Event.UpdatedBy (string) — the same
	// asymmetry documented on Event's doc comment, checked from the other
	// side here.
	if mkt.UpdatedBy != 15 {
		t.Errorf("Markets[0].UpdatedBy = %v, want 15", mkt.UpdatedBy)
	}
	if mkt.FeeType != "" {
		t.Errorf("Markets[0].FeeType = %q, want empty (wire value was JSON null)", mkt.FeeType)
	}
	if len(e.Series) != 1 {
		t.Fatalf("len(Series) = %d, want 1", len(e.Series))
	}
	// Series.CreatedBy and Series.Competitive are strings, unlike the
	// same-named fields on Market/Event (int64/string and float64) — see
	// Series' doc comment.
	if e.Series[0].CreatedBy != "15" {
		t.Errorf("Series[0].CreatedBy = %q, want the string \"15\"", e.Series[0].CreatedBy)
	}
	if e.Series[0].Competitive != "0" {
		t.Errorf("Series[0].Competitive = %q, want the string \"0\"", e.Series[0].Competitive)
	}
	if len(e.Tags) != 1 || e.Tags[0].Label != "All" {
		t.Errorf("Tags = %+v", e.Tags)
	}
}

// ---------------------------------------------------------------------------
// Stringified-JSON-array decode helper: happy path, "[]", "" and malformed.

// decodeStringArrayCase is one input to decodeStringArray and the []string
// (or error) it must produce.
type decodeStringArrayCase struct {
	name    string
	in      string
	want    []string
	wantErr bool
}

func TestDecodeStringArray(t *testing.T) {
	cases := []decodeStringArrayCase{
		{name: "typical", in: `["Yes", "No"]`, want: []string{"Yes", "No"}},
		{name: "empty array", in: "[]", want: nil},
		{name: "absent field", in: "", want: nil},
		{name: "malformed", in: "not json", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeStringArray(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatalf("got error %v, want none", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestRelatedMarketDecodeAccessors checks that RelatedMarket's Decode*
// methods delegate to the same stringified-array decoding Market's do.
func TestRelatedMarketDecodeAccessors(t *testing.T) {
	rm := RelatedMarket{
		Outcomes:      `["Yes", "No"]`,
		OutcomePrices: `["0.5", "0.5"]`,
	}
	outcomes, err := rm.DecodeOutcomes()
	if err != nil || len(outcomes) != 2 || outcomes[0] != "Yes" {
		t.Errorf("DecodeOutcomes() = %v, %v", outcomes, err)
	}
	prices, err := rm.DecodeOutcomePrices()
	if err != nil || len(prices) != 2 || prices[1] != "0.5" {
		t.Errorf("DecodeOutcomePrices() = %v, %v", prices, err)
	}
}

// ---------------------------------------------------------------------------
// Query-building: EventFilter, MarketFilter and the *bool tri-state
// convention. These call the private setX helpers directly rather than
// round-tripping through HTTP — this file is inside package gamma.

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

func boolPtr(b bool) *bool { return &b }

func TestSetEventFilterArraysAndBools(t *testing.T) {
	f := EventFilter{
		ID:       []int64{2890, 3591},
		Slug:     []string{"a", "b"},
		Closed:   boolPtr(false),
		Featured: boolPtr(true),
		TagID:    []int64{100215},
		Locale:   "en",
	}
	q := url.Values{}
	setEventFilter(q, f)
	want := url.Values{
		"id":       {"2890", "3591"},
		"slug":     {"a", "b"},
		"closed":   {"false"},
		"featured": {"true"},
		"tag_id":   {"100215"},
		"locale":   {"en"},
	}
	checkQuery(t, q, want)
}

// TestBoolFilterOmitsWhenNil is the *bool-false-reaches-the-wire check:
// Closed=false must appear in the query as the literal string "false" (a
// verified, working filter — GET /events?closed=false and closed=true
// return disjoint id sets), and a nil Closed must omit the key entirely,
// not send an empty string.
func TestBoolFilterOmitsWhenNil(t *testing.T) {
	qFalse := url.Values{}
	setEventFilter(qFalse, EventFilter{Closed: boolPtr(false)})
	if got := qFalse.Get("closed"); got != "false" {
		t.Errorf("closed=false query = %q, want \"false\"", got)
	}

	qNil := url.Values{}
	setEventFilter(qNil, EventFilter{})
	if qNil.Has("closed") {
		t.Errorf("nil Closed set a closed key: %v", qNil)
	}
}

func TestSetMarketFilterArraysAndBools(t *testing.T) {
	f := MarketFilter{
		ID:           []int64{559651},
		ClobTokenIDs: []string{"111", "222"},
		Active:       boolPtr(true),
		Archived:     boolPtr(false),
		TagID:        []int64{1, 2, 3},
	}
	q := url.Values{}
	setMarketFilter(q, f)
	want := url.Values{
		"id":             {"559651"},
		"clob_token_ids": {"111", "222"},
		"active":         {"true"},
		"archived":       {"false"},
		"tag_id":         {"1", "2", "3"},
	}
	checkQuery(t, q, want)
}

func TestSetPageAscendingFalseReachesWire(t *testing.T) {
	q := url.Values{}
	setPage(q, 25, 50, "volume24hr", boolPtr(false))
	want := url.Values{
		"limit":     {"25"},
		"offset":    {"50"},
		"order":     {"volume24hr"},
		"ascending": {"false"},
	}
	checkQuery(t, q, want)
}

func TestSetKeysetPageHasNoOffsetKey(t *testing.T) {
	q := url.Values{}
	setKeysetPage(q, 10, "", nil, "cursor-1")
	if q.Has("offset") {
		t.Errorf("keyset query carries an offset key: %v", q)
	}
	if got := q.Get("after_cursor"); got != "cursor-1" {
		t.Errorf("after_cursor = %q, want cursor-1", got)
	}
}

// ---------------------------------------------------------------------------
// HTTP-level behavior: method/path/query wiring, envelope decoding, POST
// bodies, error mapping. gammaServer starts an httptest.Server and returns a
// Client pointed at it, mirroring data_test.go's dataServer.

func gammaServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(WithHost(srv.URL))
}

var authHeaderKeys = []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE"}

func checkNoAuth(t *testing.T, r *http.Request) {
	t.Helper()
	for _, k := range authHeaderKeys {
		if r.Header.Get(k) != "" {
			t.Errorf("request carries auth header %s, want none: every Gamma endpoint is public", k)
		}
	}
}

func TestEventsNoAuthAndPath(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != epEvents {
			t.Errorf("path = %s, want %s", r.URL.Path, epEvents)
		}
		w.Write([]byte("[]"))
	})
	if _, err := c.Events(context.Background(), EventsParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestEventByID(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/2890" {
			t.Errorf("path = %s, want /events/2890", r.URL.Path)
		}
		w.Write([]byte(eventJSON))
	})
	got, err := c.Event(context.Background(), 2890, EventDetailParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "2890" {
		t.Errorf("ID = %q, want 2890", got.ID)
	}
}

func TestEventNotFound(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"type":"not found error","error":"id not found"}`))
	})
	_, err := c.Event(context.Background(), 999999999, EventDetailParams{})
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	var apiErr *polymarket.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Message != "id not found" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "id not found")
	}
}

func TestEventBySlugPath(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/slug/my-event" {
			t.Errorf("path = %s, want /events/slug/my-event", r.URL.Path)
		}
		w.Write([]byte(eventJSON))
	})
	if _, err := c.EventBySlug(context.Background(), "my-event", EventDetailParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestMarketByID(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/559651" {
			t.Errorf("path = %s, want /markets/559651", r.URL.Path)
		}
		if r.URL.Query().Get("include_tag") != "true" {
			t.Errorf("include_tag query = %q, want true", r.URL.Query().Get("include_tag"))
		}
		w.Write([]byte(marketJSON))
	})
	got, err := c.Market(context.Background(), 559651, MarketDetailParams{IncludeTag: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Question != "Xi Jinping out before 2027?" {
		t.Errorf("Question = %q", got.Question)
	}
}

func TestMarketBySlugPath(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/slug/xi-jinping-out-before-2027" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(marketJSON))
	})
	if _, err := c.MarketBySlug(context.Background(), "xi-jinping-out-before-2027", MarketDetailParams{}); err != nil {
		t.Fatal(err)
	}
}

// TestMarketDescriptionDecodesFullMarket confirms GET /markets/{id}/description
// resolves to the full Market schema (live-confirmed), not the narrower
// {description} shape the older docs spec once suggested.
func TestMarketDescriptionDecodesFullMarket(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/559651/description" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(marketJSON))
	})
	got, err := c.MarketDescription(context.Background(), 559651)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "559651" || got.ConditionID == "" {
		t.Errorf("MarketDescription() decoded a partial object: %+v", got)
	}
}

func TestEventTagsAndMarketTagsPaths(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/2890/tags", "/markets/559651/tags":
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"id":"1","label":"All"}]`))
	})
	tags, err := c.EventTags(context.Background(), 2890)
	if err != nil || len(tags) != 1 {
		t.Errorf("EventTags = %v, %v", tags, err)
	}
	tags, err = c.MarketTags(context.Background(), 559651)
	if err != nil || len(tags) != 1 {
		t.Errorf("MarketTags = %v, %v", tags, err)
	}
}

func TestEventTweetCountAndCommentsCount(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/2890/tweet-count":
			w.Write([]byte(`{"tweetCount":42}`))
		case "/events/2890/comments/count":
			w.Write([]byte(`{"count":142828}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	tc, err := c.EventTweetCount(context.Background(), 2890)
	if err != nil || tc != 42 {
		t.Errorf("EventTweetCount() = %d, %v, want 42", tc, err)
	}
	cc, err := c.EventCommentsCount(context.Background(), 2890)
	if err != nil || cc != 142828 {
		t.Errorf("EventCommentsCount() = %d, %v, want 142828", cc, err)
	}
}

// TestEventsPaginationEnvelope decodes GET /events/pagination's {data,
// pagination} envelope, the offset-pagination style this package's scope
// covers alongside keyset.
func TestEventsPaginationEnvelope(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epEventsPagination {
			t.Errorf("path = %s, want %s", r.URL.Path, epEventsPagination)
		}
		w.Write([]byte(`{"data":[` + eventJSON + `],"pagination":{"hasMore":true,"totalResults":3833}}`))
	})
	got, err := c.EventsPagination(context.Background(), EventsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != "2890" {
		t.Errorf("Data = %+v", got.Data)
	}
	if !got.Pagination.HasMore || got.Pagination.TotalResults != 3833 {
		t.Errorf("Pagination = %+v, want {true 3833}", got.Pagination)
	}
}

// TestEventsKeysetEnvelope covers both the full-page (NextCursor present)
// and last-page (NextCursor omitted) shapes GET /events/keyset returns.
func TestEventsKeysetEnvelope(t *testing.T) {
	t.Run("full page", func(t *testing.T) {
		c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != epEventsKeyset {
				t.Errorf("path = %s, want %s", r.URL.Path, epEventsKeyset)
			}
			if r.URL.Query().Has("offset") {
				t.Errorf("keyset request carries an offset param: %v", r.URL.Query())
			}
			w.Write([]byte(`{"events":[` + eventJSON + `],"next_cursor":"baQCMe2w"}`))
		})
		got, err := c.EventsKeyset(context.Background(), EventsKeysetParams{Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Events) != 1 || got.NextCursor != "baQCMe2w" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("last page", func(t *testing.T) {
		c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"events":[` + eventJSON + `]}`))
		})
		got, err := c.EventsKeyset(context.Background(), EventsKeysetParams{AfterCursor: "baQCMe2w"})
		if err != nil {
			t.Fatal(err)
		}
		if got.NextCursor != "" {
			t.Errorf("NextCursor = %q, want empty on the last page", got.NextCursor)
		}
	})
}

func TestMarketsKeysetEnvelope(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epMarketsKeyset {
			t.Errorf("path = %s, want %s", r.URL.Path, epMarketsKeyset)
		}
		if r.URL.Query().Has("offset") {
			t.Errorf("keyset request carries an offset param: %v", r.URL.Query())
		}
		w.Write([]byte(`{"markets":[` + marketJSON + `],"next_cursor":"bjZlbp"}`))
	})
	got, err := c.MarketsKeyset(context.Background(), MarketsKeysetParams{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Markets) != 1 || got.NextCursor != "bjZlbp" {
		t.Errorf("got %+v", got)
	}
}

// bodyRequest captures a request's method, path, query and raw body, for the
// two POST endpoints in this package's scope.
type bodyRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

func recordBody(t *testing.T, rec *bodyRequest, w http.ResponseWriter, r *http.Request, respBody string) {
	t.Helper()
	rec.Method = r.Method
	rec.Path = r.URL.Path
	rec.Query = r.URL.Query()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	rec.Body = b
	w.Write([]byte(respBody))
}

func TestMarketsInformationPostsBodyAndQuery(t *testing.T) {
	var rec bodyRequest
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		recordBody(t, &rec, w, r, "["+marketJSON+"]")
	})
	filter := MarketsFilterBody{
		ID:     []int64{559651},
		Closed: boolPtr(false),
	}
	got, err := c.MarketsInformation(context.Background(), filter, ListControl{Limit: 10, Ascending: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "559651" {
		t.Errorf("got %+v", got)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.Path != epMarketsInfo {
		t.Errorf("path = %s, want %s", rec.Path, epMarketsInfo)
	}
	if rec.Query.Get("limit") != "10" || rec.Query.Get("ascending") != "false" {
		t.Errorf("query = %v", rec.Query)
	}
	var gotBody MarketsFilterBody
	if err := json.Unmarshal(rec.Body, &gotBody); err != nil {
		t.Fatalf("decoding request body: %v (body %s)", err, rec.Body)
	}
	if len(gotBody.ID) != 1 || gotBody.ID[0] != 559651 {
		t.Errorf("body.ID = %v, want [559651]", gotBody.ID)
	}
	if gotBody.Closed == nil || *gotBody.Closed != false {
		t.Errorf("body.Closed = %v, want pointer to false", gotBody.Closed)
	}
}

func TestMarketsAbridgedPath(t *testing.T) {
	var rec bodyRequest
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		recordBody(t, &rec, w, r, "[]")
	})
	if _, err := c.MarketsAbridged(context.Background(), MarketsFilterBody{}, ListControl{}); err != nil {
		t.Fatal(err)
	}
	if rec.Path != epMarketsAbridged {
		t.Errorf("path = %s, want %s", rec.Path, epMarketsAbridged)
	}
}

func TestRelatedMarketsQuery(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/559651/related-markets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("closed") != "false" {
			t.Errorf("closed query = %q, want false", r.URL.Query().Get("closed"))
		}
		w.Write([]byte(`[{"id":"1","conditionId":"0xabc","outcomes":"[\"Yes\", \"No\"]","outcomePrices":"[\"0.5\", \"0.5\"]"}]`))
	})
	got, err := c.RelatedMarkets(context.Background(), 559651, RelatedMarketsParams{Closed: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	outcomes, err := got[0].DecodeOutcomes()
	if err != nil || len(outcomes) != 2 {
		t.Errorf("DecodeOutcomes() = %v, %v", outcomes, err)
	}
}

func TestSimilarEventsQuery(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epEventsSimilar {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("id") != "2890" || q.Get("limit") != "5" || q.Get("closed") != "false" {
			t.Errorf("query = %v", q)
		}
		w.Write([]byte("[]"))
	})
	if _, err := c.SimilarEvents(context.Background(), SimilarEventsParams{ID: 2890, Limit: 5, Closed: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
}

func TestEventByPartnerQuery(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epEventsByPartner {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("partner") != "some-partner" || q.Get("external_id") != "abc123" {
			t.Errorf("query = %v", q)
		}
		w.Write([]byte(eventJSON))
	})
	got, err := c.EventByPartner(context.Background(), EventByPartnerParams{Partner: "some-partner", ExternalID: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "2890" {
		t.Errorf("ID = %q", got.ID)
	}
}

func TestEventExternalPartnersPaths(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/2890/external-partners":
			w.Write([]byte(`[{"id":1,"eventId":2890,"partnerId":1,"externalId":"x"}]`))
		case "/events/2890/external-partners/acme":
			w.Write([]byte(`{"id":1,"eventId":2890,"partnerId":1,"externalId":"x"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	list, err := c.EventExternalPartners(context.Background(), 2890)
	if err != nil || len(list) != 1 {
		t.Errorf("EventExternalPartners() = %v, %v", list, err)
	}
	one, err := c.EventExternalPartner(context.Background(), 2890, "acme")
	if err != nil || one.ExternalID != "x" {
		t.Errorf("EventExternalPartner() = %+v, %v", one, err)
	}
}

func TestEventCreatorsAndByID(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epEventCreators:
			if r.URL.Query().Get("creator_handle") != "alice" {
				t.Errorf("creator_handle query = %q", r.URL.Query().Get("creator_handle"))
			}
			w.Write([]byte(`[{"id":"1","creatorName":"Alice"}]`))
		case "/events/creators/1":
			w.Write([]byte(`{"id":"1","creatorName":"Alice"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	list, err := c.EventCreators(context.Background(), EventCreatorsParams{CreatorHandle: "alice"})
	if err != nil || len(list) != 1 {
		t.Errorf("EventCreators() = %v, %v", list, err)
	}
	one, err := c.EventCreator(context.Background(), 1)
	if err != nil || one.CreatorName != "Alice" {
		t.Errorf("EventCreator() = %+v, %v", one, err)
	}
}

func TestEventsResultsQuery(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epEventsResults {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("event_week") != "10" {
			t.Errorf("event_week query = %q, want 10", q.Get("event_week"))
		}
		w.Write([]byte("[]"))
	})
	if _, err := c.EventsResults(context.Background(), EventsResultsParams{EventWeek: 10}); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Client construction smoke tests.

func TestNewAndNewWithSession(t *testing.T) {
	c := New(WithHost("https://example.invalid"))
	if c == nil || c.session == nil {
		t.Fatal("New() returned a client with no session")
	}
	s := polymarket.NewSession(polymarket.GammaHost)
	c2 := NewWithSession(s)
	if c2.session != s {
		t.Error("NewWithSession() did not adopt the given session")
	}
}

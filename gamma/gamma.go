// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

// This file covers Gamma's two core object families — Event and Market — and
// everything they share: the two pagination styles Gamma uses, and the
// decode helpers for Gamma's stringified-JSON-array fields.
//
// THE STRINGIFIED-JSON-ARRAY TRAP: Market.Outcomes, Market.OutcomePrices,
// Market.ClobTokenIDs, Market.UMAResolutionStatuses (and RelatedMarket's
// Outcomes/OutcomePrices) all arrive on the wire as a JSON array encoded
// *inside a JSON string* — e.g. `"outcomes":"[\"Yes\", \"No\"]"` — not as a
// native JSON array. They are declared `string` below, exactly as the wire
// sends them, so a plain json.Unmarshal never silently fails on them. Use
// the Decode* accessor next to each one to get a real []string.
//
// PAGINATION: Gamma has three unrelated styles. Most list endpoints (GET
// /events, GET /markets) return a bare JSON array with no paging metadata at
// all — request Limit+1 and see if you got it back to know if more rows
// exist, or use one of the two styles below. GET /events/pagination wraps
// the same list in the {data, pagination} envelope (EventsPagination, using
// Pagination). GET /events/keyset and GET /markets/keyset use cursor
// pagination instead (EventsKeysetPage, MarketsKeysetPage): pass the
// previous response's NextCursor as AfterCursor to get the next page:
// NextCursor is empty exactly on the last page. Keyset pagination rejects an
// Offset outright (verified live: GET /markets/keyset?offset=5 -> 422
// "offset is not allowed on keyset endpoints"), which is why the Keyset
// parameter types below carry no Offset field at all.
//
// FLOAT64: Gamma's numeric fields (prices, liquidity, volume, spreads) are
// display and analytics readings — the figures a Polymarket page renders —
// never amounts that flow through order construction or get signed, so the
// no-float64 rule in CLAUDE.md does not apply to them, the same reasoning
// data.go's Position type documents. BestBid, BestAsk and LastTradePrice in
// particular are Gamma's own cached, advisory view of a market's price, not
// a live order-book read — use the clob package's Price/Book methods for
// anything that feeds an order.
//
// NULLABILITY: Gamma's schema marks most fields "not required" rather than
// nullable, and several fields documented non-nullable are observed live as
// "" or 0 rather than omitted. This package follows the rest of this
// codebase's convention of never using pointers for scalar fields: an absent
// or null field simply decodes to its Go zero value. Nested single-object
// fields (FeeSchedule, ImageOptimization, CryptoMarketConfig, ...) behave
// the same way — a zero-valued struct when Gamma omits the relation.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	polymarket "github.com/ChloePike/go-polymarket"
)

// Gamma host paths that take no path parameter. Paths with a path parameter
// (an id, a slug) are built inline with fmt.Sprintf next to the method that
// uses them.
const (
	epEvents           = "/events"
	epEventsPagination = "/events/pagination"
	epEventsKeyset     = "/events/keyset"
	epEventsResults    = "/events/results"
	epEventCreators    = "/events/creators"
	epEventsSimilar    = "/events/similar"
	epEventsByPartner  = "/events/by-partner"
	epMarkets          = "/markets"
	epMarketsKeyset    = "/markets/keyset"
	epMarketsInfo      = "/markets/information"
	epMarketsAbridged  = "/markets/abridged"
)

// ---------------------------------------------------------------------------
// Query-building helpers. A zero value (empty string, 0, nil slice, nil
// pointer) means "omit the parameter and let the server apply its default"
// throughout this file. Bool-shaped filters use *bool rather than bool: many
// of them are meaningfully queried both ways (GET /events?closed=false and
// GET /events?closed=true return disjoint id sets, confirmed live), which a
// plain bool zero-value convention cannot express — nil omits the
// parameter, and a non-nil *bool sends its value explicitly either way.

func setStr(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func setInt(q url.Values, key string, val int) {
	if val != 0 {
		q.Set(key, strconv.Itoa(val))
	}
}

func setInt64(q url.Values, key string, val int64) {
	if val != 0 {
		q.Set(key, strconv.FormatInt(val, 10))
	}
}

func setFloat64(q url.Values, key string, val float64) {
	if val != 0 {
		q.Set(key, strconv.FormatFloat(val, 'f', -1, 64))
	}
}

func setBool(q url.Values, key string, val *bool) {
	if val != nil {
		q.Set(key, strconv.FormatBool(*val))
	}
}

// setStrs sends one repeated query parameter per element (id=1&id=2), which
// is the encoding Gamma actually accepts for its array-typed filters —
// verified live: a comma-joined id=1,2 does not filter, repeated id= does.
func setStrs(q url.Values, key string, vals []string) {
	for _, v := range vals {
		q.Add(key, v)
	}
}

func setInt64s(q url.Values, key string, vals []int64) {
	for _, v := range vals {
		q.Add(key, strconv.FormatInt(v, 10))
	}
}

// setPage appends the classic offset-pagination controls every non-keyset
// Gamma list endpoint accepts.
func setPage(q url.Values, limit, offset int, order string, ascending *bool) {
	setInt(q, "limit", limit)
	setInt(q, "offset", offset)
	setStr(q, "order", order)
	setBool(q, "ascending", ascending)
}

// setKeysetPage appends the cursor-pagination controls a GET .../keyset
// endpoint accepts. There is deliberately no offset parameter here: the
// server 422s if one is sent.
func setKeysetPage(q url.Values, limit int, order string, ascending *bool, afterCursor string) {
	setInt(q, "limit", limit)
	setStr(q, "order", order)
	setBool(q, "ascending", ascending)
	setStr(q, "after_cursor", afterCursor)
}

// ---------------------------------------------------------------------------
// Stringified-JSON-array decode helpers.

// decodeStringArray decodes one of Gamma's stringified-JSON-array fields — a
// JSON array of strings that Gamma encodes as a JSON string, not as a native
// array; see this file's top-level doc comment. An empty wire value decodes
// to a nil slice with no error, covering both an absent field and the
// literal "[]" Gamma sends for e.g. an unset UMAResolutionStatuses.
func decodeStringArray(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("gamma: decoding stringified array %q: %w", s, err)
	}
	return out, nil
}

// DecodeOutcomes decodes Outcomes, Gamma's JSON-array-in-a-string encoding
// of a market's outcome names (e.g. "[\"Yes\", \"No\"]").
func (m Market) DecodeOutcomes() ([]string, error) { return decodeStringArray(m.Outcomes) }

// DecodeOutcomePrices decodes OutcomePrices. Each element is a decimal price
// as a string, not a JSON number — kept as []string, never []float64, so no
// precision is lost and Market prices stay compatible with CLAUDE.md's
// no-float64-for-money rule.
func (m Market) DecodeOutcomePrices() ([]string, error) { return decodeStringArray(m.OutcomePrices) }

// DecodeClobTokenIDs decodes ClobTokenIDs into the ERC-1155 token id strings
// (big integers, kept as strings) that identify each outcome on the CLOB.
func (m Market) DecodeClobTokenIDs() ([]string, error) { return decodeStringArray(m.ClobTokenIDs) }

// DecodeUMAResolutionStatuses decodes UMAResolutionStatuses. Commonly an
// empty array ("[]") on an unresolved market.
func (m Market) DecodeUMAResolutionStatuses() ([]string, error) {
	return decodeStringArray(m.UMAResolutionStatuses)
}

// DecodeOutcomes decodes Outcomes on a RelatedMarket. RelatedMarket carries
// the same stringified-array encoding as Market — see this file's top-level
// doc comment.
func (r RelatedMarket) DecodeOutcomes() ([]string, error) { return decodeStringArray(r.Outcomes) }

// DecodeOutcomePrices decodes OutcomePrices on a RelatedMarket. See
// Market.DecodeOutcomePrices for why this stays []string.
func (r RelatedMarket) DecodeOutcomePrices() ([]string, error) {
	return decodeStringArray(r.OutcomePrices)
}

// ---------------------------------------------------------------------------
// Small closed-set fields Gamma documents as string enums.

// A ComboStatus is a Market's combo-bet eligibility state.
type ComboStatus string

const (
	ComboStatusPending  ComboStatus = "pending"
	ComboStatusEnabled  ComboStatus = "enabled"
	ComboStatusDisabled ComboStatus = "disabled"
)

// A ResolutionStatus is a Market's high-level resolution state.
type ResolutionStatus string

const (
	ResolutionStatusActive   ResolutionStatus = "active"
	ResolutionStatusResolved ResolutionStatus = "resolved"
)

// A ProtocolVersion is the on-chain protocol version a Market or Event
// trades on: v1 (legacy CTF/CLOB) or v2 (polymarket-v2 modules). Treat an
// unrecognized value as unsupported rather than assuming v1 semantics.
type ProtocolVersion string

const (
	ProtocolVersionV1 ProtocolVersion = "v1"
	ProtocolVersionV2 ProtocolVersion = "v2"
)

// ---------------------------------------------------------------------------
// Supporting types nested inside Market and Event.

// ImageOptimization is CDN-optimized-image metadata, embedded wherever a
// Market or Event carries an *Optimized field (IconOptimized,
// ImageOptimized, FeaturedImageOptimized).
type ImageOptimization struct {
	Field                     string  `json:"field"`
	ID                        string  `json:"id"`
	ImageOptimizedComplete    bool    `json:"imageOptimizedComplete"`
	ImageOptimizedLastUpdated string  `json:"imageOptimizedLastUpdated"`
	ImageSizeKbOptimized      float64 `json:"imageSizeKbOptimized"`
	ImageSizeKbSource         float64 `json:"imageSizeKbSource"`
	ImageURLOptimized         string  `json:"imageUrlOptimized"`
	ImageURLSource            string  `json:"imageUrlSource"`
	RelID                     int64   `json:"relID"`
	RelName                   string  `json:"relname"`
}

// FeeSchedule is a Market's fee configuration, embedded as Market.FeeSchedule.
type FeeSchedule struct {
	Exponent   float64 `json:"exponent"`
	Rate       float64 `json:"rate"`
	RebateRate float64 `json:"rebateRate"`
	TakerOnly  bool    `json:"takerOnly"`
}

// ClobRewards is one asset's liquidity-reward configuration for a market,
// embedded as Market.ClobRewards[]. Unlike Outcomes/OutcomePrices/
// ClobTokenIDs, this field is a genuine native JSON array, not a
// stringified one.
type ClobRewards struct {
	AssetAddress     string  `json:"assetAddress"`
	ConditionID      string  `json:"conditionId"`
	EndDate          string  `json:"endDate"`
	ID               string  `json:"id"`
	RewardsAmount    float64 `json:"rewardsAmount"`
	RewardsDailyRate float64 `json:"rewardsDailyRate"`
	StartDate        string  `json:"startDate"`
}

// CryptoMarketConfig is a Market's crypto-price-feed configuration,
// embedded as Market.CryptoMarketConfig, present only on markets that
// resolve against a crypto price feed.
type CryptoMarketConfig struct {
	Schema              string `json:"$schema"`
	Asset               string `json:"asset"`
	Duration            string `json:"duration"`
	ID                  string `json:"id"`
	TWAPEnabled         bool   `json:"twapEnabled"`
	TWAPLookbackSeconds int64  `json:"twapLookbackSeconds"`
}

// InternalUser is a minimal internal-staff reference, embedded as
// Market.InternalUsers[] / Event.InternalUsers[] / Series.InternalUsers[].
type InternalUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// BestLine is one sports betting line attached to an Event, embedded as
// Event.BestLines[]. Fields beyond these three are UNVERIFIED: the live
// schema documents only id, line and lineType.
type BestLine struct {
	ID       string  `json:"id"`
	Line     float64 `json:"line"`
	LineType string  `json:"lineType"`
}

// A Tag labels an Event or a Market for browsing and filtering. It is
// embedded as Event.Tags[] / Market.Tags[] (the latter populated only when
// a request sets IncludeTag), and stands alone via GET /tags — a family
// this package does not otherwise wrap.
type Tag struct {
	Schema              string     `json:"$schema"`
	ActiveEventsCount   int64      `json:"activeEventsCount"`
	CreatedAt           string     `json:"createdAt"`
	CreatedBy           int64      `json:"createdBy"`
	ForceHide           bool       `json:"forceHide"`
	ForceShow           bool       `json:"forceShow"`
	ID                  string     `json:"id"`
	IsCarousel          bool       `json:"isCarousel"`
	Label               string     `json:"label"`
	PublishedAt         string     `json:"publishedAt"`
	RequiresTranslation bool       `json:"requiresTranslation"`
	Slug                string     `json:"slug"`
	Templates           []Template `json:"templates"`
	UpdatedAt           string     `json:"updatedAt"`
	UpdatedBy           int64      `json:"updatedBy"`
}

// A Series groups a recurring family of Events (e.g. "NBA", "NFL"). It is
// embedded as Event.Series[], and nests Event.Markets[] indirectly through
// Events.
//
// Series.Competitive and Series.CreatedBy/UpdatedBy are strings, unlike the
// same-named fields on Market (float64, int64) — an asymmetry in Gamma's own
// schema, not a transcription slip; see this file's top-level doc comment.
// Series.TemplateVariables is a bool here, where Market's and Event's
// same-named field is a string — likewise a genuine schema asymmetry.
type Series struct {
	Schema              string         `json:"$schema"`
	Active              bool           `json:"active"`
	Archived            bool           `json:"archived"`
	CgAssetName         string         `json:"cgAssetName"`
	Closed              bool           `json:"closed"`
	CommentCount        int64          `json:"commentCount"`
	CommentsEnabled     bool           `json:"commentsEnabled"`
	Competitive         string         `json:"competitive"`
	CreatedAt           string         `json:"createdAt"`
	CreatedBy           string         `json:"createdBy"`
	Description         string         `json:"description"`
	Events              []Event        `json:"events"`
	Featured            bool           `json:"featured"`
	Icon                string         `json:"icon"`
	ID                  string         `json:"id"`
	Image               string         `json:"image"`
	InternalUsers       []InternalUser `json:"internalUsers"`
	IsTemplate          bool           `json:"isTemplate"`
	Layout              string         `json:"layout"`
	Liquidity           float64        `json:"liquidity"`
	New                 bool           `json:"new"`
	PublishedAt         string         `json:"publishedAt"`
	PythTokenID         string         `json:"pythTokenID"`
	Recurrence          string         `json:"recurrence"`
	RequiresTranslation bool           `json:"requiresTranslation"`
	Restricted          bool           `json:"restricted"`
	Score               int64          `json:"score"`
	SeriesType          string         `json:"seriesType"`
	Slug                string         `json:"slug"`
	StartDate           string         `json:"startDate"`
	Subtitle            string         `json:"subtitle"`
	Tags                []Tag          `json:"tags"`
	TemplateVariables   bool           `json:"templateVariables"`
	Ticker              string         `json:"ticker"`
	Title               string         `json:"title"`
	UpdatedAt           string         `json:"updatedAt"`
	UpdatedBy           string         `json:"updatedBy"`
	Volume              float64        `json:"volume"`
	Volume24hr          float64        `json:"volume24hr"`
}

// A Template is a market/event creation template, embedded as
// Tag.Templates[] and Event.Templates[].
type Template struct {
	Schema                  string   `json:"$schema"`
	CreatedAt               string   `json:"createdAt"`
	CreatorUserID           string   `json:"creatorUserId"`
	Description             string   `json:"description"`
	DisplayName             string   `json:"displayName"`
	EventSlug               string   `json:"eventSlug"`
	EventTitle              string   `json:"eventTitle"`
	Events                  []Event  `json:"events"`
	ID                      string   `json:"id"`
	Markets                 string   `json:"markets"`
	MarketsAugmentedNegRisk bool     `json:"marketsAugmentedNegRisk"`
	MarketsNegRisk          bool     `json:"marketsNegRisk"`
	MarketsOrder            string   `json:"marketsOrder"`
	MarketsShowImages       bool     `json:"marketsShowImages"`
	ResolutionSource        string   `json:"resolutionSource"`
	Series                  []Series `json:"series"`
	Tags                    []Tag    `json:"tags"`
	UpdatedAt               string   `json:"updatedAt"`
	UserVariables           string   `json:"userVariables"`
}

// An EventCreator is one entry in the event-creator directory, embedded as
// Event.EventCreators[] and returned by GET /events/creators(/{id}).
type EventCreator struct {
	Schema        string `json:"$schema"`
	CreatedAt     string `json:"createdAt"`
	CreatorHandle string `json:"creatorHandle"`
	CreatorImage  string `json:"creatorImage"`
	CreatorName   string `json:"creatorName"`
	CreatorURL    string `json:"creatorUrl"`
	ID            string `json:"id"`
	UpdatedAt     string `json:"updatedAt"`
}

// A Partner is an external distribution partner Gamma tracks event/team/
// sport mappings against — embedded as EventExternalPartnerMapping.Partner.
type Partner struct {
	Schema    string `json:"$schema"`
	CreatedAt string `json:"createdAt"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	UpdatedAt string `json:"updatedAt"`
}

// EventExternalPartnerMapping links one Event to one external Partner's own
// identifier for it, embedded as Event.ExternalPartners[] and returned by
// GET /events/{id}/external-partners(/{partnerSlug}).
type EventExternalPartnerMapping struct {
	Schema     string  `json:"$schema"`
	CreatedAt  string  `json:"createdAt"`
	EventID    int64   `json:"eventId"`
	ExternalID string  `json:"externalId"`
	ID         int64   `json:"id"`
	Partner    Partner `json:"partner"`
	PartnerID  int64   `json:"partnerId"`
	UpdatedAt  string  `json:"updatedAt"`
}

// SportExternalPartnerMapping links one sport to one external partner's own
// identifier for it, embedded as SportsMetadata.ExternalPartners[]. Not
// independently curled this session — shape taken from the live OpenAPI
// schema, by analogy with the curled EventExternalPartnerMapping.
type SportExternalPartnerMapping struct {
	Schema     string  `json:"$schema"`
	CreatedAt  string  `json:"createdAt"`
	ExternalID string  `json:"externalId"`
	ID         int64   `json:"id"`
	Partner    Partner `json:"partner"`
	PartnerID  int64   `json:"partnerId"`
	SportID    int64   `json:"sportId"`
	UpdatedAt  string  `json:"updatedAt"`
}

// SportsMetadata describes one sport or league, embedded as Event.Sport.
// UNVERIFIED against the live shape beyond the fields curled in the report
// this package is built from ($schema, createdAt, externalPartners, id,
// image, name, ordering, primaryTagId, resolution, series, sport, tags,
// updatedAt) — Tags and Series arrive as comma-separated ID strings here,
// not JSON, a third encoding style distinct from the stringified-JSON-array
// trap: split on "," and parse each element as an integer.
type SportsMetadata struct {
	Schema           string                        `json:"$schema"`
	CreatedAt        string                        `json:"createdAt"`
	ExternalPartners []SportExternalPartnerMapping `json:"externalPartners"`
	ID               int64                         `json:"id"`
	Image            string                        `json:"image"`
	Name             string                        `json:"name"`
	Ordering         string                        `json:"ordering"`
	PrimaryTagID     int64                         `json:"primaryTagId"`
	Resolution       string                        `json:"resolution"`
	// Series is a comma-separated list of series ids, not a JSON array.
	Series string `json:"series"`
	Sport  string `json:"sport"`
	// Tags is a comma-separated list of tag ids, not a JSON array.
	Tags      string `json:"tags"`
	UpdatedAt string `json:"updatedAt"`
}

// TeamExternalPartnerMapping links one Team to one external partner's own
// identifier for it, embedded as Team.ExternalPartners[]. UNVERIFIED live
// (shape taken from the OpenAPI schema, by analogy with
// EventExternalPartnerMapping).
type TeamExternalPartnerMapping struct {
	Schema     string  `json:"$schema"`
	CreatedAt  string  `json:"createdAt"`
	ExternalID string  `json:"externalId"`
	ID         int64   `json:"id"`
	Partner    Partner `json:"partner"`
	PartnerID  int64   `json:"partnerId"`
	TeamID     int64   `json:"teamId"`
	UpdatedAt  string  `json:"updatedAt"`
}

// A Team is a sports team directory entry, embedded as Event.Teams[].
type Team struct {
	Schema           string                       `json:"$schema"`
	Abbreviation     string                       `json:"abbreviation"`
	Alias            string                       `json:"alias"`
	Color            string                       `json:"color"`
	CreatedAt        string                       `json:"createdAt"`
	ExternalPartners []TeamExternalPartnerMapping `json:"externalPartners"`
	ID               int64                        `json:"id"`
	League           string                       `json:"league"`
	Logo             string                       `json:"logo"`
	Name             string                       `json:"name"`
	Ordering         string                       `json:"ordering"`
	ProviderID       int64                        `json:"providerId"`
	Record           string                       `json:"record"`
	UpdatedAt        string                       `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Market.

// A Market is one binary or categorical question on Polymarket: its
// question text, its outcomes and their current prices, its trading and
// resolution state, and the fee, reward and liquidity configuration behind
// it. It is the response shape of every Markets-family method in this file,
// and nests under Event.Markets[] too — same schema, same field names, just
// commonly with fewer relations expanded there.
//
// Outcomes, OutcomePrices, ClobTokenIDs and UMAResolutionStatuses are the
// stringified-JSON-array trap fields — decode them with the Decode* methods
// below, never json.Unmarshal directly into a slice. See this file's
// top-level doc comment for the full explanation.
type Market struct {
	Schema                   string  `json:"$schema"`
	AcceptingOrders          bool    `json:"acceptingOrders"`
	AcceptingOrdersTimestamp string  `json:"acceptingOrdersTimestamp"`
	AcceptingOrdersUntil     string  `json:"acceptingOrdersUntil"`
	Active                   bool    `json:"active"`
	AMMType                  string  `json:"ammType"`
	Approved                 bool    `json:"approved"`
	Archived                 bool    `json:"archived"`
	AutomaticallyActive      bool    `json:"automaticallyActive"`
	AutomaticallyResolved    bool    `json:"automaticallyResolved"`
	BestAsk                  float64 `json:"bestAsk"`
	BestBid                  float64 `json:"bestBid"`
	Category                 string  `json:"category"`
	CategoryMailchimpTag     string  `json:"categoryMailchimpTag"`
	ChartColor               string  `json:"chartColor"`
	ClearBookOnStart         bool    `json:"clearBookOnStart"`
	// ClobRewards is a genuine native array (not stringified).
	ClobRewards []ClobRewards `json:"clobRewards"`
	// ClobTokenIDs is a stringified JSON array — decode with DecodeClobTokenIDs.
	ClobTokenIDs string `json:"clobTokenIds"`
	Closed       bool   `json:"closed"`
	ClosedTime   string `json:"closedTime"`
	// ComboStatus can only be set at creation (enabled requires a ConditionID
	// and two position ids); after creation it changes only through the
	// dedicated combo-status endpoint, not a plain market update.
	ComboStatus          ComboStatus        `json:"comboStatus"`
	CommentsEnabled      bool               `json:"commentsEnabled"`
	Competitive          float64            `json:"competitive"`
	ConditionID          string             `json:"conditionId"`
	CreatedAt            string             `json:"createdAt"`
	CreatedBy            int64              `json:"createdBy"`
	Creator              string             `json:"creator"`
	CryptoMarketConfig   CryptoMarketConfig `json:"cryptoMarketConfig"`
	CryptoMarketConfigID string             `json:"cryptoMarketConfigId"`
	CurationOrder        int64              `json:"curationOrder"`
	CustomLiveness       int64              `json:"customLiveness"`
	CYOM                 bool               `json:"cyom"`
	DenominationToken    string             `json:"denominationToken"`
	Deploying            bool               `json:"deploying"`
	DeployingTimestamp   string             `json:"deployingTimestamp"`
	Description          string             `json:"description"`
	DisqusThread         string             `json:"disqusThread"`
	EnableOrderBook      bool               `json:"enableOrderBook"`
	EndDate              string             `json:"endDate"`
	EndDateISO           string             `json:"endDateIso"`
	EventStartTime       string             `json:"eventStartTime"`
	// Events is populated on the top-level GET /markets response.
	Events                []Event           `json:"events"`
	Featured              bool              `json:"featured"`
	Fee                   string            `json:"fee"`
	FeeExponent           float64           `json:"feeExponent"`
	FeeRate               float64           `json:"feeRate"`
	FeeSchedule           FeeSchedule       `json:"feeSchedule"`
	FeeType               string            `json:"feeType"`
	FeesEnabled           bool              `json:"feesEnabled"`
	FormatType            string            `json:"formatType"`
	FPMMLive              bool              `json:"fpmmLive"`
	Funded                bool              `json:"funded"`
	FundedTimestamp       string            `json:"fundedTimestamp"`
	GameID                string            `json:"gameId"`
	GameStartTime         string            `json:"gameStartTime"`
	GroupItemRange        string            `json:"groupItemRange"`
	GroupItemThreshold    string            `json:"groupItemThreshold"`
	GroupItemTitle        string            `json:"groupItemTitle"`
	HasReviewedDates      bool              `json:"hasReviewedDates"`
	HoldingRewardsEnabled bool              `json:"holdingRewardsEnabled"`
	Icon                  string            `json:"icon"`
	IconOptimized         ImageOptimization `json:"iconOptimized"`
	ID                    string            `json:"id"`
	Image                 string            `json:"image"`
	ImageOptimized        ImageOptimization `json:"imageOptimized"`
	InternalUsers         []InternalUser    `json:"internalUsers"`
	LastTradePrice        float64           `json:"lastTradePrice"`
	Line                  float64           `json:"line"`
	// Liquidity is a decimal string on this endpoint; prefer LiquidityNum.
	Liquidity               string         `json:"liquidity"`
	LiquidityAMM            float64        `json:"liquidityAmm"`
	LiquidityCLOB           float64        `json:"liquidityClob"`
	LiquidityNum            float64        `json:"liquidityNum"`
	LowerBound              string         `json:"lowerBound"`
	LowerBoundDate          string         `json:"lowerBoundDate"`
	MailchimpTag            string         `json:"mailchimpTag"`
	MakerBaseFee            int64          `json:"makerBaseFee"`
	MakerRebatesFeeShareBps int64          `json:"makerRebatesFeeShareBps"`
	ManualActivation        bool           `json:"manualActivation"`
	MarketGroup             int64          `json:"marketGroup"`
	MarketMakerAddress      string         `json:"marketMakerAddress"`
	MarketMetadata          map[string]any `json:"marketMetadata"`
	MarketType              string         `json:"marketType"`
	// Markets is a self-nesting relation observed in the live schema; expect
	// it empty on almost every response.
	Markets              []Market `json:"markets"`
	NegRisk              bool     `json:"negRisk"`
	NegRiskMarketID      string   `json:"negRiskMarketID"`
	NegRiskOther         bool     `json:"negRiskOther"`
	NegRiskRequestID     string   `json:"negRiskRequestID"`
	New                  bool     `json:"new"`
	NotificationsEnabled bool     `json:"notificationsEnabled"`
	// OnchainEventID is the parent v2 on-chain event id: a bytes32 hex value
	// with the bottom 3 bytes zero, equal to the v2 ConditionID with its
	// condition index cleared. Shared by every market of a neg-risk event.
	// Present only on v2 markets; read-only, derived from the v2 ConditionID.
	OnchainEventID        string  `json:"onchainEventId"`
	OneDayPriceChange     float64 `json:"oneDayPriceChange"`
	OneHourPriceChange    float64 `json:"oneHourPriceChange"`
	OneMonthPriceChange   float64 `json:"oneMonthPriceChange"`
	OneWeekPriceChange    float64 `json:"oneWeekPriceChange"`
	OneYearPriceChange    float64 `json:"oneYearPriceChange"`
	OrderMinSize          float64 `json:"orderMinSize"`
	OrderPriceMinTickSize float64 `json:"orderPriceMinTickSize"`
	// OutcomePrices is a stringified JSON array — decode with DecodeOutcomePrices.
	OutcomePrices string `json:"outcomePrices"`
	// Outcomes is a stringified JSON array — decode with DecodeOutcomes.
	Outcomes                     string `json:"outcomes"`
	PagerDutyNotificationEnabled bool   `json:"pagerDutyNotificationEnabled"`
	// PastSlugs' encoding is UNVERIFIED — the report this package is built
	// from does not confirm whether it is a stringified array like Outcomes
	// or a plain scalar; kept as a raw string pending confirmation.
	PastSlugs         string `json:"pastSlugs"`
	PendingDeployment bool   `json:"pendingDeployment"`
	// PositionIDs is a genuine native array (not stringified).
	PositionIDs                  []string         `json:"positionIds"`
	Question                     string           `json:"question"`
	QuestionID                   string           `json:"questionID"`
	Ready                        bool             `json:"ready"`
	ReadyForCron                 bool             `json:"readyForCron"`
	ReadyTimestamp               string           `json:"readyTimestamp"`
	RequiresTranslation          bool             `json:"requiresTranslation"`
	ResolutionSource             string           `json:"resolutionSource"`
	ResolutionStatus             ResolutionStatus `json:"resolutionStatus"`
	ResolvedBy                   string           `json:"resolvedBy"`
	Restricted                   bool             `json:"restricted"`
	RewardsMaxSpread             float64          `json:"rewardsMaxSpread"`
	RewardsMinSize               float64          `json:"rewardsMinSize"`
	RFQEnabled                   bool             `json:"rfqEnabled"`
	ScheduledDeploymentTimestamp string           `json:"scheduledDeploymentTimestamp"`
	Score                        int64            `json:"score"`
	SecondsDelay                 int64            `json:"secondsDelay"`
	SentDiscord                  bool             `json:"sentDiscord"`
	SeriesColor                  string           `json:"seriesColor"`
	// ShortOutcomes' encoding is UNVERIFIED — same caveat as PastSlugs.
	ShortOutcomes    string  `json:"shortOutcomes"`
	ShowGMPOutcome   bool    `json:"showGmpOutcome"`
	ShowGMPSeries    bool    `json:"showGmpSeries"`
	Slug             string  `json:"slug"`
	SponsorImage     string  `json:"sponsorImage"`
	SponsorName      string  `json:"sponsorName"`
	SportsMarketType string  `json:"sportsMarketType"`
	Spread           float64 `json:"spread"`
	StartDate        string  `json:"startDate"`
	StartDateISO     string  `json:"startDateIso"`
	Subcategory      string  `json:"subcategory"`
	// SubmittedBy's wire key is the literal snake_case "submitted_by" — the
	// one field on Market that breaks from the otherwise-consistent
	// camelCase convention.
	SubmittedBy              string `json:"submitted_by"`
	Tags                     []Tag  `json:"tags"`
	TakerBaseFee             int64  `json:"takerBaseFee"`
	TeamAID                  string `json:"teamAID"`
	TeamBID                  string `json:"teamBID"`
	TwitterCardImage         string `json:"twitterCardImage"`
	TwitterCardLastRefreshed string `json:"twitterCardLastRefreshed"`
	TwitterCardLastValidated string `json:"twitterCardLastValidated"`
	TwitterCardLocation      string `json:"twitterCardLocation"`
	UMABond                  string `json:"umaBond"`
	UMAEndDate               string `json:"umaEndDate"`
	UMAEndDateISO            string `json:"umaEndDateIso"`
	UMAResolutionStatus      string `json:"umaResolutionStatus"`
	// UMAResolutionStatuses is a stringified JSON array (often "[]") —
	// decode with DecodeUMAResolutionStatuses.
	UMAResolutionStatuses string          `json:"umaResolutionStatuses"`
	UMAReward             string          `json:"umaReward"`
	UpdatedAt             string          `json:"updatedAt"`
	UpdatedBy             int64           `json:"updatedBy"`
	UpperBound            string          `json:"upperBound"`
	UpperBoundDate        string          `json:"upperBoundDate"`
	Version               ProtocolVersion `json:"version"`
	// Volume is a decimal string on this endpoint; prefer VolumeNum.
	Volume         string  `json:"volume"`
	Volume1mo      float64 `json:"volume1mo"`
	Volume1moAMM   float64 `json:"volume1moAmm"`
	Volume1moCLOB  float64 `json:"volume1moClob"`
	Volume1wk      float64 `json:"volume1wk"`
	Volume1wkAMM   float64 `json:"volume1wkAmm"`
	Volume1wkCLOB  float64 `json:"volume1wkClob"`
	Volume1yr      float64 `json:"volume1yr"`
	Volume1yrAMM   float64 `json:"volume1yrAmm"`
	Volume1yrCLOB  float64 `json:"volume1yrClob"`
	Volume24hr     float64 `json:"volume24hr"`
	Volume24hrAMM  float64 `json:"volume24hrAmm"`
	Volume24hrCLOB float64 `json:"volume24hrClob"`
	VolumeAMM      float64 `json:"volumeAmm"`
	VolumeCLOB     float64 `json:"volumeClob"`
	VolumeNum      float64 `json:"volumeNum"`
	WideFormat     bool    `json:"wideFormat"`
	XAxisValue     string  `json:"xAxisValue"`
	YAxisValue     string  `json:"yAxisValue"`
}

// A RelatedMarket is one entry in GET /markets/{id}/related-markets: a
// lightweight cross-reference to another market, not a full Market.
// Outcomes and OutcomePrices carry the same stringified-JSON-array
// encoding as Market — decode with the Decode* methods above.
type RelatedMarket struct {
	ConditionID string `json:"conditionId"`
	EventSlug   string `json:"eventSlug"`
	ID          string `json:"id"`
	Image       string `json:"image"`
	// OutcomePrices is a stringified JSON array — decode with DecodeOutcomePrices.
	OutcomePrices string `json:"outcomePrices"`
	// Outcomes is a stringified JSON array — decode with DecodeOutcomes.
	Outcomes  string `json:"outcomes"`
	Question  string `json:"question"`
	Slug      string `json:"slug"`
	StartDate string `json:"startDate"`
	Volume    string `json:"volume"`
}

// ---------------------------------------------------------------------------
// Event.

// An Event is a Polymarket page: a title and slug, the Markets it groups
// (a single-market event has exactly one), its parent Series, its Tags, and
// its own resolution and display state.
//
// A handful of fields carry the same JSON key as a Market field but a
// different wire type — a genuine asymmetry in Gamma's own schema, not a
// transcription slip:
//
//	field         Market type   Event type
//	createdBy     int64         string
//	gameId        string        int64
//	liquidity     string        float64 (Market's is a decimal string)
//	score         int64         string
//	updatedBy     int64         string
type Event struct {
	Schema                 string                        `json:"$schema"`
	Active                 bool                          `json:"active"`
	Archived               bool                          `json:"archived"`
	AutomaticallyActive    bool                          `json:"automaticallyActive"`
	AutomaticallyResolved  bool                          `json:"automaticallyResolved"`
	BestLines              []BestLine                    `json:"bestLines"`
	CantEstimate           bool                          `json:"cantEstimate"`
	CarouselMap            string                        `json:"carouselMap"`
	Category               string                        `json:"category"`
	Closed                 bool                          `json:"closed"`
	ClosedTime             string                        `json:"closedTime"`
	Color                  string                        `json:"color"`
	CommentCount           int64                         `json:"commentCount"`
	CommentsEnabled        bool                          `json:"commentsEnabled"`
	Competitive            float64                       `json:"competitive"`
	CountryName            string                        `json:"countryName"`
	CreatedAt              string                        `json:"createdAt"`
	CreatedBy              string                        `json:"createdBy"`
	CreationDate           string                        `json:"creationDate"`
	CumulativeMarkets      bool                          `json:"cumulativeMarkets"`
	CYOM                   bool                          `json:"cyom"`
	Deploying              bool                          `json:"deploying"`
	DeployingTimestamp     string                        `json:"deployingTimestamp"`
	Description            string                        `json:"description"`
	DisqusThread           string                        `json:"disqusThread"`
	Elapsed                string                        `json:"elapsed"`
	ElectionType           string                        `json:"electionType"`
	EnableNegRisk          bool                          `json:"enableNegRisk"`
	EnableOrderBook        bool                          `json:"enableOrderBook"`
	EndDate                string                        `json:"endDate"`
	Ended                  bool                          `json:"ended"`
	EstimateValue          bool                          `json:"estimateValue"`
	EstimatedValue         string                        `json:"estimatedValue"`
	EventCreators          []EventCreator                `json:"eventCreators"`
	EventDate              string                        `json:"eventDate"`
	EventMetadata          map[string]any                `json:"eventMetadata"`
	EventWeek              int64                         `json:"eventWeek"`
	ExternalPartners       []EventExternalPartnerMapping `json:"externalPartners"`
	Featured               bool                          `json:"featured"`
	FeaturedImage          string                        `json:"featuredImage"`
	FeaturedImageOptimized ImageOptimization             `json:"featuredImageOptimized"`
	FeaturedOrder          int64                         `json:"featuredOrder"`
	FinishedTimestamp      string                        `json:"finishedTimestamp"`
	GameID                 int64                         `json:"gameId"`
	GMPChartMode           string                        `json:"gmpChartMode"`
	Icon                   string                        `json:"icon"`
	IconOptimized          ImageOptimization             `json:"iconOptimized"`
	ID                     string                        `json:"id"`
	Image                  string                        `json:"image"`
	ImageOptimized         ImageOptimization             `json:"imageOptimized"`
	InternalUsers          []InternalUser                `json:"internalUsers"`
	IsTemplate             bool                          `json:"isTemplate"`
	LastHighlight          string                        `json:"lastHighlight"`
	LastHighlightAt        string                        `json:"lastHighlightAt"`
	LastHighlightType      string                        `json:"lastHighlightType"`
	Liquidity              float64                       `json:"liquidity"`
	LiquidityAMM           float64                       `json:"liquidityAmm"`
	LiquidityCLOB          float64                       `json:"liquidityClob"`
	Live                   bool                          `json:"live"`
	Markets                []Market                      `json:"markets"`
	NegRisk                bool                          `json:"negRisk"`
	NegRiskAugmented       bool                          `json:"negRiskAugmented"`
	NegRiskFeeBips         int64                         `json:"negRiskFeeBips"`
	NegRiskMarketID        string                        `json:"negRiskMarketID"`
	New                    bool                          `json:"new"`
	OpenInterest           float64                       `json:"openInterest"`
	ParentEventID          int64                         `json:"parentEventId"`
	PendingDeployment      bool                          `json:"pendingDeployment"`
	Period                 string                        `json:"period"`
	// PublishedAt's wire key is the literal snake_case "published_at", and
	// its value is not RFC3339 (observed live as "2022-07-27 14:40:02.064+00",
	// space-separated with a short UTC offset) — kept as a raw string rather
	// than parsed, like every other timestamp field in this package.
	PublishedAt                  string         `json:"published_at"`
	RequiresTranslation          bool           `json:"requiresTranslation"`
	RescheduledFromGameID        int64          `json:"rescheduledFromGameId"`
	ResolutionSource             string         `json:"resolutionSource"`
	Restricted                   bool           `json:"restricted"`
	ScheduledDeploymentTimestamp string         `json:"scheduledDeploymentTimestamp"`
	Score                        string         `json:"score"`
	Series                       []Series       `json:"series"`
	SeriesSlug                   string         `json:"seriesSlug"`
	ShowAllOutcomes              bool           `json:"showAllOutcomes"`
	ShowMarketImages             bool           `json:"showMarketImages"`
	Slug                         string         `json:"slug"`
	SortBy                       string         `json:"sortBy"`
	Sport                        SportsMetadata `json:"sport"`
	SpreadsMainLine              float64        `json:"spreadsMainLine"`
	StartDate                    string         `json:"startDate"`
	StartTime                    string         `json:"startTime"`
	SubEvents                    []string       `json:"subEvents"`
	Subcategory                  string         `json:"subcategory"`
	Subtitle                     string         `json:"subtitle"`
	// TagLabels' and TagSlugs' wire keys are the literal snake_case
	// "tag_labels" / "tag_slugs" — the same asymmetry Market.SubmittedBy has.
	TagLabels         []string        `json:"tag_labels"`
	TagSlugs          []string        `json:"tag_slugs"`
	Tags              []Tag           `json:"tags"`
	Teams             []Team          `json:"teams"`
	TemplateVariables string          `json:"templateVariables"`
	Templates         []Template      `json:"templates"`
	Ticker            string          `json:"ticker"`
	Title             string          `json:"title"`
	TotalsMainLine    float64         `json:"totalsMainLine"`
	TurnProviderID    string          `json:"turnProviderId"`
	TweetCount        int64           `json:"tweetCount"`
	UpdatedAt         string          `json:"updatedAt"`
	UpdatedBy         string          `json:"updatedBy"`
	Version           ProtocolVersion `json:"version"`
	Volume            float64         `json:"volume"`
	Volume1mo         float64         `json:"volume1mo"`
	Volume1wk         float64         `json:"volume1wk"`
	Volume1yr         float64         `json:"volume1yr"`
	Volume24hr        float64         `json:"volume24hr"`
}

// ---------------------------------------------------------------------------
// Shared pagination envelopes.

// Pagination is the {hasMore, totalResults} metadata block Gamma attaches to
// the wrapped-list endpoints — GET /events/pagination in this package's
// scope.
type Pagination struct {
	HasMore      bool  `json:"hasMore"`
	TotalResults int64 `json:"totalResults"`
}

// EventsPagination is the envelope GET /events/pagination returns: the same
// Event list GET /events returns, plus paging metadata GET /events itself
// never carries.
type EventsPagination struct {
	Schema     string     `json:"$schema"`
	Data       []Event    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// EventsKeysetPage is the envelope GET /events/keyset returns: one page of
// Events plus the cursor for the next page. NextCursor is empty exactly on
// the last page — that omission, not an empty-string sentinel distinct from
// "", is the end-of-results signal.
type EventsKeysetPage struct {
	Schema     string  `json:"$schema"`
	Events     []Event `json:"events"`
	NextCursor string  `json:"next_cursor"`
}

// MarketsKeysetPage is the envelope GET /markets/keyset returns: one page of
// Markets plus the cursor for the next page, with the same NextCursor
// end-of-results convention as EventsKeysetPage.
type MarketsKeysetPage struct {
	Schema     string   `json:"$schema"`
	Markets    []Market `json:"markets"`
	NextCursor string   `json:"next_cursor"`
}

// ---------------------------------------------------------------------------
// Events: query filters and list/detail parameters.

// EventFilter holds the predicate filters GET /events, GET /events/pagination
// and GET /events/keyset all accept: everything that narrows which events
// match. Pagination controls (Limit, Offset, Order, Ascending, AfterCursor)
// are deliberately not part of this type — they differ per endpoint, most
// importantly because keyset pagination rejects Offset outright. See
// EventsParams and EventsKeysetParams, which each embed this and add the
// controls their endpoint actually accepts.
type EventFilter struct {
	ID                 []int64
	Slug               []string
	Closed             *bool
	Live               *bool
	LiquidityMin       float64
	LiquidityMax       float64
	VolumeMin          float64
	VolumeMax          float64
	StartDateMin       string
	StartDateMax       string
	EndDateMin         string
	EndDateMax         string
	StartTimeMin       string
	StartTimeMax       string
	TagID              []int64
	RelatedTags        *bool
	TagMatch           string
	TagSlug            string
	ExcludeTagID       []int64
	Featured           *bool
	FeaturedOrder      *bool
	CYOM               *bool
	SeriesID           []int64
	SeriesSlug         []string
	GameID             []int64
	EventDate          string
	EventWeek          int
	Recurrence         string
	CreatedBy          []string
	ParentEventID      []int64
	IncludeChildren    *bool
	TitleSearch        string
	IncludeTemplate    *bool
	IncludeBestLines   *bool
	EventMetadataKey   string
	EventMetadataValue string
	PartnerSlug        string
	Locale             string
}

func setEventFilter(q url.Values, f EventFilter) {
	setInt64s(q, "id", f.ID)
	setStrs(q, "slug", f.Slug)
	setBool(q, "closed", f.Closed)
	setBool(q, "live", f.Live)
	setFloat64(q, "liquidity_min", f.LiquidityMin)
	setFloat64(q, "liquidity_max", f.LiquidityMax)
	setFloat64(q, "volume_min", f.VolumeMin)
	setFloat64(q, "volume_max", f.VolumeMax)
	setStr(q, "start_date_min", f.StartDateMin)
	setStr(q, "start_date_max", f.StartDateMax)
	setStr(q, "end_date_min", f.EndDateMin)
	setStr(q, "end_date_max", f.EndDateMax)
	setStr(q, "start_time_min", f.StartTimeMin)
	setStr(q, "start_time_max", f.StartTimeMax)
	setInt64s(q, "tag_id", f.TagID)
	setBool(q, "related_tags", f.RelatedTags)
	setStr(q, "tag_match", f.TagMatch)
	setStr(q, "tag_slug", f.TagSlug)
	setInt64s(q, "exclude_tag_id", f.ExcludeTagID)
	setBool(q, "featured", f.Featured)
	setBool(q, "featured_order", f.FeaturedOrder)
	setBool(q, "cyom", f.CYOM)
	setInt64s(q, "series_id", f.SeriesID)
	setStrs(q, "series_slug", f.SeriesSlug)
	setInt64s(q, "game_id", f.GameID)
	setStr(q, "event_date", f.EventDate)
	setInt(q, "event_week", f.EventWeek)
	setStr(q, "recurrence", f.Recurrence)
	setStrs(q, "created_by", f.CreatedBy)
	setInt64s(q, "parent_event_id", f.ParentEventID)
	setBool(q, "include_children", f.IncludeChildren)
	setStr(q, "title_search", f.TitleSearch)
	setBool(q, "include_template", f.IncludeTemplate)
	setBool(q, "include_best_lines", f.IncludeBestLines)
	setStr(q, "event_metadata_key", f.EventMetadataKey)
	setStr(q, "event_metadata_value", f.EventMetadataValue)
	setStr(q, "partner_slug", f.PartnerSlug)
	setStr(q, "locale", f.Locale)
}

// EventsParams filters and paginates GET /events and GET /events/pagination,
// which accept the identical filter set with offset pagination.
type EventsParams struct {
	EventFilter
	Limit     int
	Offset    int
	Order     string
	Ascending *bool
}

// EventsKeysetParams filters and paginates GET /events/keyset. There is no
// Offset field: the server rejects one (verified live, 422 "offset is not
// allowed on keyset endpoints"). Leave AfterCursor empty for the first page,
// then pass the previous EventsKeysetPage.NextCursor for each page after.
type EventsKeysetParams struct {
	EventFilter
	Limit       int
	Order       string
	Ascending   *bool
	AfterCursor string
}

// EventDetailParams are the optional query parameters GET /events/{id} and
// GET /events/slug/{slug} both accept.
type EventDetailParams struct {
	IncludeTemplate  *bool
	IncludeBestLines *bool
	Locale           string
}

// EventsResultsParams filters and paginates GET /events/results.
type EventsResultsParams struct {
	ID           []int64
	Slug         []string
	Closed       *bool
	SeriesID     []int64
	GameID       []int64
	EventDate    string
	EventWeek    int
	StartTimeMin string
	StartTimeMax string
	Limit        int
	Offset       int
	Order        string
	Ascending    *bool
}

// EventCreatorsParams filters and paginates GET /events/creators.
type EventCreatorsParams struct {
	CreatorName   string
	CreatorHandle string
	Limit         int
	Offset        int
	Order         string
	Ascending     *bool
}

// SimilarEventsParams selects a reference event for GET /events/similar,
// either directly (ID or EventSlug) or by free-text search against
// Typesense (EventTitle, MarketTitle, MarketSlug — the parent event of the
// given market becomes the reference).
type SimilarEventsParams struct {
	ID          int64
	EventTitle  string
	EventSlug   string
	MarketTitle string
	MarketSlug  string
	// Limit defaults to 10 server-side and is capped at 50.
	Limit int
	// Closed selects only resolved (true) or only open (false) events when
	// set; nil returns both. Always combined server-side with an
	// unconditional active=true filter.
	Closed *bool
}

// EventByPartnerParams selects the event GET /events/by-partner reports:
// the one event an external partner has mapped to the given identifier.
type EventByPartnerParams struct {
	// Partner is the external partner's slug.
	Partner string
	// ExternalID is the sport identifier the partner uses for this event.
	ExternalID string
}

// ---------------------------------------------------------------------------
// Events: methods.

// Events lists events, filtered and offset-paginated. It needs no
// authentication.
//
// GET /events
func (c *Client) Events(ctx context.Context, p EventsParams) ([]Event, error) {
	q := url.Values{}
	setEventFilter(q, p.EventFilter)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Event
	if err := c.session.Get(ctx, epEvents, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Event reports one event by id. It needs no authentication. A miss returns
// a *polymarket.Error with StatusCode 404.
//
// GET /events/{id}
func (c *Client) Event(ctx context.Context, id int64, p EventDetailParams) (Event, error) {
	q := url.Values{}
	setBool(q, "include_template", p.IncludeTemplate)
	setBool(q, "include_best_lines", p.IncludeBestLines)
	setStr(q, "locale", p.Locale)
	var out Event
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epEvents, id), q, &out)
	return out, err
}

// EventBySlug reports one event by its slug, an exact match — not a fuzzy
// search. It needs no authentication.
//
// GET /events/slug/{slug}
func (c *Client) EventBySlug(ctx context.Context, slug string, p EventDetailParams) (Event, error) {
	q := url.Values{}
	setBool(q, "include_template", p.IncludeTemplate)
	setBool(q, "include_best_lines", p.IncludeBestLines)
	setStr(q, "locale", p.Locale)
	var out Event
	err := c.session.Get(ctx, epEvents+"/slug/"+slug, q, &out)
	return out, err
}

// EventTags reports one event's tags. It needs no authentication.
//
// GET /events/{id}/tags
func (c *Client) EventTags(ctx context.Context, id int64) ([]Tag, error) {
	var out []Tag
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/tags", epEvents, id), nil, &out)
	return out, err
}

// eventTweetCount decodes GET /events/{id}/tweet-count.
type eventTweetCount struct {
	TweetCount int64 `json:"tweetCount"`
}

// EventTweetCount reports one event's Twitter/X share count. It needs no
// authentication.
//
// GET /events/{id}/tweet-count
func (c *Client) EventTweetCount(ctx context.Context, id int64) (int64, error) {
	var out eventTweetCount
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/tweet-count", epEvents, id), nil, &out)
	return out.TweetCount, err
}

// gammaCount decodes the {count} shape shared by the two .../comments/count
// endpoints in this package's scope.
type gammaCount struct {
	Count int64 `json:"count"`
}

// EventCommentsCount reports one event's comment count. It needs no
// authentication.
//
// GET /events/{id}/comments/count
func (c *Client) EventCommentsCount(ctx context.Context, id int64) (int64, error) {
	var out gammaCount
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/comments/count", epEvents, id), nil, &out)
	return out.Count, err
}

// EventsPagination lists events wrapped with paging metadata (HasMore,
// TotalResults) — the same filter set as Events, offset-paginated, with the
// total-count and has-more indicator that GET /events itself never returns.
// It needs no authentication.
//
// GET /events/pagination
func (c *Client) EventsPagination(ctx context.Context, p EventsParams) (EventsPagination, error) {
	q := url.Values{}
	setEventFilter(q, p.EventFilter)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out EventsPagination
	err := c.session.Get(ctx, epEventsPagination, q, &out)
	return out, err
}

// EventsKeyset lists events with cursor pagination: pass "" as
// p.AfterCursor for the first page, then each page's returned
// EventsKeysetPage.NextCursor for the next, until NextCursor comes back
// empty. It needs no authentication.
//
// A 503 from this endpoint means keyset pagination is disabled server-side
// for this deploy — callers should treat that as "fall back to Events",
// not a hard failure.
//
// GET /events/keyset
func (c *Client) EventsKeyset(ctx context.Context, p EventsKeysetParams) (EventsKeysetPage, error) {
	q := url.Values{}
	setEventFilter(q, p.EventFilter)
	setKeysetPage(q, p.Limit, p.Order, p.Ascending, p.AfterCursor)
	var out EventsKeysetPage
	err := c.session.Get(ctx, epEventsKeyset, q, &out)
	return out, err
}

// EventsResults reports sport event results. It needs no authentication.
//
// GET /events/results
func (c *Client) EventsResults(ctx context.Context, p EventsResultsParams) ([]Event, error) {
	q := url.Values{}
	setInt64s(q, "id", p.ID)
	setStrs(q, "slug", p.Slug)
	setBool(q, "closed", p.Closed)
	setInt64s(q, "series_id", p.SeriesID)
	setInt64s(q, "game_id", p.GameID)
	setStr(q, "event_date", p.EventDate)
	setInt(q, "event_week", p.EventWeek)
	setStr(q, "start_time_min", p.StartTimeMin)
	setStr(q, "start_time_max", p.StartTimeMax)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Event
	err := c.session.Get(ctx, epEventsResults, q, &out)
	return out, err
}

// EventCreators lists the event-creator directory. It needs no
// authentication.
//
// GET /events/creators
func (c *Client) EventCreators(ctx context.Context, p EventCreatorsParams) ([]EventCreator, error) {
	q := url.Values{}
	setStr(q, "creator_name", p.CreatorName)
	setStr(q, "creator_handle", p.CreatorHandle)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []EventCreator
	err := c.session.Get(ctx, epEventCreators, q, &out)
	return out, err
}

// EventCreator reports one event creator by id. It needs no authentication.
//
// GET /events/creators/{id}
func (c *Client) EventCreator(ctx context.Context, id int64) (EventCreator, error) {
	var out EventCreator
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epEventCreators, id), nil, &out)
	return out, err
}

// SimilarEvents reports events similar to a reference event, selected by
// p.ID, p.EventSlug, or free-text search. It needs no authentication.
//
// GET /events/similar
func (c *Client) SimilarEvents(ctx context.Context, p SimilarEventsParams) ([]Event, error) {
	q := url.Values{}
	setInt64(q, "id", p.ID)
	setStr(q, "event_title", p.EventTitle)
	setStr(q, "event_slug", p.EventSlug)
	setStr(q, "market_title", p.MarketTitle)
	setStr(q, "market_slug", p.MarketSlug)
	setInt(q, "limit", p.Limit)
	setBool(q, "closed", p.Closed)
	var out []Event
	err := c.session.Get(ctx, epEventsSimilar, q, &out)
	return out, err
}

// EventByPartner reports the event an external partner has mapped to the
// given identifier. It needs no authentication.
//
// GET /events/by-partner
func (c *Client) EventByPartner(ctx context.Context, p EventByPartnerParams) (Event, error) {
	q := url.Values{}
	setStr(q, "partner", p.Partner)
	setStr(q, "external_id", p.ExternalID)
	var out Event
	err := c.session.Get(ctx, epEventsByPartner, q, &out)
	return out, err
}

// EventExternalPartners reports every external partner mapping for one
// event. It needs no authentication.
//
// GET /events/{id}/external-partners
func (c *Client) EventExternalPartners(ctx context.Context, id int64) ([]EventExternalPartnerMapping, error) {
	var out []EventExternalPartnerMapping
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/external-partners", epEvents, id), nil, &out)
	return out, err
}

// EventExternalPartner reports one event's mapping to one named external
// partner. It needs no authentication.
//
// GET /events/{id}/external-partners/{partnerSlug}
func (c *Client) EventExternalPartner(ctx context.Context, id int64, partnerSlug string) (EventExternalPartnerMapping, error) {
	var out EventExternalPartnerMapping
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/external-partners/%s", epEvents, id, partnerSlug), nil, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Markets: query filters and list/detail parameters.

// MarketFilter holds the predicate filters GET /markets and GET
// /markets/keyset both accept. Pagination controls live in MarketsParams and
// MarketsKeysetParams, which each embed this — see EventFilter's doc comment
// for why the split exists.
type MarketFilter struct {
	ID                  []int64
	Slug                []string
	Archived            *bool
	Active              *bool
	Decimalized         *bool
	Closed              *bool
	ClobTokenIDs        []string
	PositionIDs         []string
	ConditionIDs        []string
	MarketMakerAddress  []string
	LiquidityNumMin     float64
	LiquidityNumMax     float64
	VolumeNumMin        float64
	VolumeNumMax        float64
	StartDateMin        string
	StartDateMax        string
	EndDateMin          string
	EndDateMax          string
	TagID               []int64
	RelatedTags         *bool
	TagMatch            string
	CYOM                *bool
	RFQEnabled          *bool
	ComboStatus         string
	UMAResolutionStatus string
	GameID              string
	SportsMarketTypes   []string
	RewardsMinSize      float64
	QuestionIDs         []string
	IncludeTag          *bool
	Locale              string
	MarketMetadataKey   string
	MarketMetadataValue string
}

func setMarketFilter(q url.Values, f MarketFilter) {
	setInt64s(q, "id", f.ID)
	setStrs(q, "slug", f.Slug)
	setBool(q, "archived", f.Archived)
	setBool(q, "active", f.Active)
	setBool(q, "decimalized", f.Decimalized)
	setBool(q, "closed", f.Closed)
	setStrs(q, "clob_token_ids", f.ClobTokenIDs)
	setStrs(q, "position_ids", f.PositionIDs)
	setStrs(q, "condition_ids", f.ConditionIDs)
	setStrs(q, "market_maker_address", f.MarketMakerAddress)
	setFloat64(q, "liquidity_num_min", f.LiquidityNumMin)
	setFloat64(q, "liquidity_num_max", f.LiquidityNumMax)
	setFloat64(q, "volume_num_min", f.VolumeNumMin)
	setFloat64(q, "volume_num_max", f.VolumeNumMax)
	setStr(q, "start_date_min", f.StartDateMin)
	setStr(q, "start_date_max", f.StartDateMax)
	setStr(q, "end_date_min", f.EndDateMin)
	setStr(q, "end_date_max", f.EndDateMax)
	setInt64s(q, "tag_id", f.TagID)
	setBool(q, "related_tags", f.RelatedTags)
	setStr(q, "tag_match", f.TagMatch)
	setBool(q, "cyom", f.CYOM)
	setBool(q, "rfq_enabled", f.RFQEnabled)
	setStr(q, "combo_status", f.ComboStatus)
	setStr(q, "uma_resolution_status", f.UMAResolutionStatus)
	setStr(q, "game_id", f.GameID)
	setStrs(q, "sports_market_types", f.SportsMarketTypes)
	setFloat64(q, "rewards_min_size", f.RewardsMinSize)
	setStrs(q, "question_ids", f.QuestionIDs)
	setBool(q, "include_tag", f.IncludeTag)
	setStr(q, "locale", f.Locale)
	setStr(q, "market_metadata_key", f.MarketMetadataKey)
	setStr(q, "market_metadata_value", f.MarketMetadataValue)
}

// MarketsParams filters and paginates GET /markets.
type MarketsParams struct {
	MarketFilter
	Limit     int
	Offset    int
	Order     string
	Ascending *bool
}

// MarketsKeysetParams filters and paginates GET /markets/keyset. There is no
// Offset field: the server rejects one (verified live, 422 "offset is not
// allowed on keyset endpoints"). Leave AfterCursor empty for the first page,
// then pass the previous MarketsKeysetPage.NextCursor for each page after.
type MarketsKeysetParams struct {
	MarketFilter
	Limit       int
	Order       string
	Ascending   *bool
	AfterCursor string
}

// MarketDetailParams are the optional query parameters GET /markets/{id} and
// GET /markets/slug/{slug} both accept.
type MarketDetailParams struct {
	IncludeTag *bool
	Locale     string
}

// RelatedMarketsParams filters and paginates GET /markets/{id}/related-markets.
type RelatedMarketsParams struct {
	Closed    *bool
	Limit     int
	Offset    int
	Order     string
	Ascending *bool
}

// MarketsFilterBody is the JSON body POST /markets/information and POST
// /markets/abridged both take: mirrors most of MarketFilter, camelCased and
// sent as a request body instead of query parameters. Both endpoints
// additionally accept Limit/Offset/Order/Ascending as query parameters
// alongside this body — see ListControl and MarketsInformation.
type MarketsFilterBody struct {
	ID                  []int64  `json:"id,omitempty"`
	Slug                []string `json:"slug,omitempty"`
	Closed              *bool    `json:"closed,omitempty"`
	ClobTokenIDs        []string `json:"clobTokenIds,omitempty"`
	ConditionIDs        []string `json:"conditionIds,omitempty"`
	QuestionIDs         []string `json:"questionIds,omitempty"`
	PositionIDs         []string `json:"positionIds,omitempty"`
	MarketMakerAddress  []string `json:"marketMakerAddress,omitempty"`
	ComboStatus         string   `json:"comboStatus,omitempty"`
	CYOM                *bool    `json:"cyom,omitempty"`
	RFQEnabled          *bool    `json:"rfqEnabled,omitempty"`
	RelatedTags         *bool    `json:"relatedTags,omitempty"`
	TagID               []int64  `json:"tagId,omitempty"`
	IncludeTags         *bool    `json:"includeTags,omitempty"`
	UMAResolutionStatus string   `json:"umaResolutionStatus,omitempty"`
	GameID              string   `json:"gameId,omitempty"`
	SportsMarketTypes   []string `json:"sportsMarketTypes,omitempty"`
	RewardsMinSize      float64  `json:"rewardsMinSize,omitempty"`
	LiquidityNumMin     float64  `json:"liquidityNumMin,omitempty"`
	LiquidityNumMax     float64  `json:"liquidityNumMax,omitempty"`
	VolumeNumMin        float64  `json:"volumeNumMin,omitempty"`
	VolumeNumMax        float64  `json:"volumeNumMax,omitempty"`
	StartDateMin        string   `json:"startDateMin,omitempty"`
	StartDateMax        string   `json:"startDateMax,omitempty"`
	EndDateMin          string   `json:"endDateMin,omitempty"`
	EndDateMax          string   `json:"endDateMax,omitempty"`
}

// ListControl is the limit/offset/order/ascending controls POST
// /markets/information and POST /markets/abridged accept as query
// parameters, sent alongside the JSON filter body.
type ListControl struct {
	Limit     int
	Offset    int
	Order     string
	Ascending *bool
}

// ---------------------------------------------------------------------------
// Markets: methods.

// Markets lists markets, filtered and offset-paginated. It needs no
// authentication.
//
// GET /markets
func (c *Client) Markets(ctx context.Context, p MarketsParams) ([]Market, error) {
	q := url.Values{}
	setMarketFilter(q, p.MarketFilter)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Market
	if err := c.session.Get(ctx, epMarkets, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Market reports one market by id. It needs no authentication. A miss
// returns a *polymarket.Error with StatusCode 404.
//
// GET /markets/{id}
func (c *Client) Market(ctx context.Context, id int64, p MarketDetailParams) (Market, error) {
	q := url.Values{}
	setBool(q, "include_tag", p.IncludeTag)
	setStr(q, "locale", p.Locale)
	var out Market
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epMarkets, id), q, &out)
	return out, err
}

// MarketBySlug reports one market by its slug, an exact match — not a fuzzy
// search. It needs no authentication.
//
// GET /markets/slug/{slug}
func (c *Client) MarketBySlug(ctx context.Context, slug string, p MarketDetailParams) (Market, error) {
	q := url.Values{}
	setBool(q, "include_tag", p.IncludeTag)
	setStr(q, "locale", p.Locale)
	var out Market
	err := c.session.Get(ctx, epMarkets+"/slug/"+slug, q, &out)
	return out, err
}

// MarketTags reports one market's tags. It needs no authentication.
//
// GET /markets/{id}/tags
func (c *Client) MarketTags(ctx context.Context, id int64) ([]Tag, error) {
	var out []Tag
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/tags", epMarkets, id), nil, &out)
	return out, err
}

// MarketDescription reports one market by id — the same full Market object
// Market itself returns. The name and the narrower shape once documented
// for this route ({description: string}) come from Gamma's older, unofficial
// docs spec; the live OpenAPI schema this package is built from resolves the
// endpoint's response to the full Market schema, confirmed live (curled this
// route directly). Kept as a distinct method, rather than folded into
// Market, because it is Gamma's own separate route.
//
// GET /markets/{id}/description
func (c *Client) MarketDescription(ctx context.Context, id int64) (Market, error) {
	var out Market
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/description", epMarkets, id), nil, &out)
	return out, err
}

// RelatedMarkets reports markets related to one market. It needs no
// authentication.
//
// GET /markets/{id}/related-markets
func (c *Client) RelatedMarkets(ctx context.Context, id int64, p RelatedMarketsParams) ([]RelatedMarket, error) {
	q := url.Values{}
	setBool(q, "closed", p.Closed)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []RelatedMarket
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/related-markets", epMarkets, id), q, &out)
	return out, err
}

// MarketsKeyset lists markets with cursor pagination: pass "" as
// p.AfterCursor for the first page, then each page's returned
// MarketsKeysetPage.NextCursor for the next, until NextCursor comes back
// empty. It needs no authentication.
//
// A 503 from this endpoint means keyset pagination is disabled server-side
// for this deploy — callers should treat that as "fall back to Markets",
// not a hard failure.
//
// GET /markets/keyset
func (c *Client) MarketsKeyset(ctx context.Context, p MarketsKeysetParams) (MarketsKeysetPage, error) {
	q := url.Values{}
	setMarketFilter(q, p.MarketFilter)
	setKeysetPage(q, p.Limit, p.Order, p.Ascending, p.AfterCursor)
	var out MarketsKeysetPage
	err := c.session.Get(ctx, epMarketsKeyset, q, &out)
	return out, err
}

// MarketsInformation looks up markets by a filter sent in the request body
// rather than as query parameters. It needs no authentication.
//
// POST /markets/information
func (c *Client) MarketsInformation(ctx context.Context, filter MarketsFilterBody, p ListControl) ([]Market, error) {
	return c.postMarketsFilter(ctx, epMarketsInfo, filter, p)
}

// MarketsAbridged is the same filtered lookup as MarketsInformation, on a
// separate route. Despite the name, Gamma's own schema does not document a
// reduced field set on the response — the "abridged" distinction, if any, is
// UNVERIFIED beyond the route existing. It needs no authentication.
//
// POST /markets/abridged
func (c *Client) MarketsAbridged(ctx context.Context, filter MarketsFilterBody, p ListControl) ([]Market, error) {
	return c.postMarketsFilter(ctx, epMarketsAbridged, filter, p)
}

// postMarketsFilter implements MarketsInformation and MarketsAbridged, which
// share a request shape. It uses session.Do rather than session.Get because
// both endpoints are POSTs with a body but no authentication — AuthNone,
// polymarket.Request's zero value, is exactly the level a public POST needs.
func (c *Client) postMarketsFilter(ctx context.Context, path string, filter MarketsFilterBody, p ListControl) ([]Market, error) {
	q := url.Values{}
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Market
	req := polymarket.Request{
		Method: http.MethodPost,
		Path:   path,
		Query:  q,
		Body:   filter,
		Out:    &out,
	}
	if err := c.session.Do(ctx, req); err != nil {
		return nil, err
	}
	return out, nil
}

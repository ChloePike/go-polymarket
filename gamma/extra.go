// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

// This file covers the rest of Gamma's metadata surface beyond Event and
// Market (declared in gamma.go, and reused here without redeclaration):
// series, tags, comments, public search, public profiles, sports/teams, and
// the two service-health routes.
//
// PARAMETER ASYMMETRIES: several sibling routes that look identical accept
// different query parameters on the live, authoritative spec
// (https://gamma-api.polymarket.com/openapi.json) — not the older public
// docs spec, which this package follows per gamma.go's own precedent of
// treating openapi:live as ground truth. Notably: GET /tags/{id} accepts
// include_template, but GET /tags/slug/{slug} does not; GET
// /tags/{id}/related-tags accepts omit_empty and status but not locale,
// while GET /tags/{id}/related-tags/tags accepts all three; GET
// /tags/slug/{slug}/related-tags accepts no query parameters at all, while
// its sibling GET /tags/slug/{slug}/related-tags/tags accepts the same three
// as the id form. Each method below sends exactly what its own route
// accepts, not a shared superset.
//
// GET /comments requires ParentEntityType and ParentEntityID: the OpenAPI
// schema marks both optional, but the live server 422s without them
// ("required query parameter is missing"). CommentsParams has no default
// zero-value that succeeds.

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Gamma host paths that take no path parameter, in this file's scope. Paths
// with a path parameter are built inline with fmt.Sprintf next to the method
// that uses them, matching gamma.go's convention.
const (
	epSeries         = "/series"
	epSeriesSummary  = "/series-summary"
	epTags           = "/tags"
	epComments       = "/comments"
	epPublicSearch   = "/public-search"
	epPublicProfile  = "/public-profile"
	epPublicProfiles = "/public-profiles"
	epSports         = "/sports"
	epSportsMktTypes = "/sports/market-types"
	epTeams          = "/teams"
	epStatus         = "/status"
	epReadyz         = "/readyz"
)

// The three values GET /comments' parent_entity_type parameter accepts:
// Event and Series are capitalized, market is lowercase — straight from
// Gamma's own schema enum, not a transcription slip.
const (
	ParentEntityEvent  = "Event"
	ParentEntitySeries = "Series"
	ParentEntityMarket = "market"
)

// ---------------------------------------------------------------------------
// Sports: comma-separated-list decode helper (a third stringified-collection
// encoding, distinct from the JSON-in-a-string trap gamma.go documents:
// SportsMetadata.Tags and SportsMetadata.Series are a literal
// comma-separated list of decimal integers, not JSON at all).

// decodeCommaIDs decodes one of SportsMetadata's comma-separated id lists. An
// empty wire value decodes to a nil slice with no error, mirroring
// decodeStringArray's convention for an absent/empty field.
func decodeCommaIDs(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("gamma: decoding comma-separated id list %q: %w", s, err)
		}
		out[i] = v
	}
	return out, nil
}

// DecodeTagIDs decodes SportsMetadata.Tags, a comma-separated list of tag
// ids, into real int64s.
func (s SportsMetadata) DecodeTagIDs() ([]int64, error) { return decodeCommaIDs(s.Tags) }

// DecodeSeriesIDs decodes SportsMetadata.Series, a comma-separated list of
// series ids, into real int64s.
func (s SportsMetadata) DecodeSeriesIDs() ([]int64, error) { return decodeCommaIDs(s.Series) }

// ---------------------------------------------------------------------------
// Series.

// SeriesParams filters and paginates GET /series. There is no AfterCursor
// field: the live spec lists an after_cursor parameter on this route, but
// keyset cursoring is documented as belonging to GET /series/keyset-shaped
// routes elsewhere in the API, not this one — its presence here is treated
// as vestigial, the same call gamma.go's EventFilter doc comment makes about
// GET /events.
type SeriesParams struct {
	Slug          []string
	Closed        *bool
	Recurrence    string
	ExcludeEvents *bool
	Locale        string
	Limit         int
	Offset        int
	Order         string
	Ascending     *bool
}

// SeriesDetailParams are the optional query parameters GET /series/{id}
// accepts. There is no equivalent lookup by slug: the only slug-based access
// to a Series is the Slug filter on SeriesParams.
type SeriesDetailParams struct {
	Locale string
}

// SeriesSummary is the compact per-series schedule digest GET
// /series-summary/{id} and GET /series-summary/slug/{slug} return: the dates
// and week numbers a series has open events on, not the full Series object.
type SeriesSummary struct {
	Schema           string   `json:"$schema"`
	EarliestOpenDate string   `json:"earliest_open_date"`
	EarliestOpenWeek int64    `json:"earliest_open_week"`
	EventDates       []string `json:"eventDates"`
	EventWeeks       []int64  `json:"eventWeeks"`
	ID               string   `json:"id"`
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	Volume           float64  `json:"volume"`
	Volume24hr       float64  `json:"volume24hr"`
}

// Series lists series, filtered and offset-paginated. It needs no
// authentication.
//
// GET /series
func (c *Client) Series(ctx context.Context, p SeriesParams) ([]Series, error) {
	q := url.Values{}
	setStrs(q, "slug", p.Slug)
	setBool(q, "closed", p.Closed)
	setStr(q, "recurrence", p.Recurrence)
	setBool(q, "exclude_events", p.ExcludeEvents)
	setStr(q, "locale", p.Locale)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Series
	if err := c.session.Get(ctx, epSeries, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SeriesByID reports one series by id. It needs no authentication. A miss
// returns a *polymarket.Error with StatusCode 404.
//
// GET /series/{id}
func (c *Client) SeriesByID(ctx context.Context, id int64, p SeriesDetailParams) (Series, error) {
	q := url.Values{}
	setStr(q, "locale", p.Locale)
	var out Series
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epSeries, id), q, &out)
	return out, err
}

// SeriesCommentsCount reports one series' comment count. It needs no
// authentication.
//
// GET /series/{id}/comments/count
func (c *Client) SeriesCommentsCount(ctx context.Context, id int64) (int64, error) {
	var out gammaCount
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/comments/count", epSeries, id), nil, &out)
	return out.Count, err
}

// SeriesSummaryByID reports one series' schedule summary by id. It needs no
// authentication.
//
// GET /series-summary/{id}
func (c *Client) SeriesSummaryByID(ctx context.Context, id int64) (SeriesSummary, error) {
	var out SeriesSummary
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epSeriesSummary, id), nil, &out)
	return out, err
}

// SeriesSummaryBySlug reports one series' schedule summary by slug. It needs
// no authentication.
//
// GET /series-summary/slug/{slug}
func (c *Client) SeriesSummaryBySlug(ctx context.Context, slug string) (SeriesSummary, error) {
	var out SeriesSummary
	err := c.session.Get(ctx, epSeriesSummary+"/slug/"+slug, nil, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Tags.

// A RelatedTag is one raw tag-relationship row returned by
// GET /tags/{id}/related-tags and its slug sibling — not the resolved Tag
// object itself; see TagRelatedTagsResolved for that.
type RelatedTag struct {
	Schema       string `json:"$schema"`
	ID           string `json:"id"`
	Rank         int64  `json:"rank"`
	RelatedTagID int64  `json:"relatedTagID"`
	TagID        int64  `json:"tagID"`
}

// TagsParams filters and paginates GET /tags.
type TagsParams struct {
	IncludeTemplate *bool
	IsCarousel      *bool
	Locale          string
	Limit           int
	Offset          int
	Order           string
	Ascending       *bool
}

// TagDetailParams are the optional query parameters GET /tags/{id} accepts.
// GET /tags/slug/{slug} does not accept IncludeTemplate — see TagBySlugParams
// and this file's top-level doc comment on parameter asymmetries.
type TagDetailParams struct {
	IncludeTemplate *bool
	Locale          string
}

// TagBySlugParams are the optional query parameters GET /tags/slug/{slug}
// accepts — narrower than TagDetailParams; see this file's top-level doc
// comment.
type TagBySlugParams struct {
	Locale string
}

// RelatedTagsParams are the optional query parameters GET
// /tags/{id}/related-tags accepts. It has no Locale field: that route's live
// schema does not accept one, unlike its .../tags sibling
// (RelatedTagsResolvedParams) — see this file's top-level doc comment.
type RelatedTagsParams struct {
	OmitEmpty *bool
	Status    string
}

// RelatedTagsResolvedParams are the optional query parameters GET
// /tags/{id}/related-tags/tags and GET /tags/slug/{slug}/related-tags/tags
// both accept.
type RelatedTagsResolvedParams struct {
	OmitEmpty *bool
	Status    string
	Locale    string
}

// Tags lists tags, filtered and offset-paginated. It needs no
// authentication.
//
// GET /tags
func (c *Client) Tags(ctx context.Context, p TagsParams) ([]Tag, error) {
	q := url.Values{}
	setBool(q, "include_template", p.IncludeTemplate)
	setBool(q, "is_carousel", p.IsCarousel)
	setStr(q, "locale", p.Locale)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Tag
	if err := c.session.Get(ctx, epTags, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Tag reports one tag by id. It needs no authentication. A miss returns a
// *polymarket.Error with StatusCode 404.
//
// GET /tags/{id}
func (c *Client) Tag(ctx context.Context, id int64, p TagDetailParams) (Tag, error) {
	q := url.Values{}
	setBool(q, "include_template", p.IncludeTemplate)
	setStr(q, "locale", p.Locale)
	var out Tag
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epTags, id), q, &out)
	return out, err
}

// TagBySlug reports one tag by its slug, an exact match — not a fuzzy
// search. It needs no authentication.
//
// GET /tags/slug/{slug}
func (c *Client) TagBySlug(ctx context.Context, slug string, p TagBySlugParams) (Tag, error) {
	q := url.Values{}
	setStr(q, "locale", p.Locale)
	var out Tag
	err := c.session.Get(ctx, epTags+"/slug/"+slug, q, &out)
	return out, err
}

// TagRelatedTags reports the raw relationship rows for tags related to one
// tag, by id — not the resolved Tag objects themselves; see
// TagRelatedTagsResolved for that. It needs no authentication.
//
// GET /tags/{id}/related-tags
func (c *Client) TagRelatedTags(ctx context.Context, id int64, p RelatedTagsParams) ([]RelatedTag, error) {
	q := url.Values{}
	setBool(q, "omit_empty", p.OmitEmpty)
	setStr(q, "status", p.Status)
	var out []RelatedTag
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/related-tags", epTags, id), q, &out)
	return out, err
}

// TagRelatedTagsBySlug is TagRelatedTags looked up by slug instead of id. It
// accepts no query parameters at all — a live-schema asymmetry from every
// other related-tags route; see this file's top-level doc comment. It needs
// no authentication.
//
// GET /tags/slug/{slug}/related-tags
func (c *Client) TagRelatedTagsBySlug(ctx context.Context, slug string) ([]RelatedTag, error) {
	var out []RelatedTag
	err := c.session.Get(ctx, epTags+"/slug/"+slug+"/related-tags", nil, &out)
	return out, err
}

// TagRelatedTagsResolved reports the resolved Tag objects related to one
// tag, by id — the convenience form of TagRelatedTags when tag metadata is
// wanted rather than the raw relationship rows. It needs no authentication.
//
// GET /tags/{id}/related-tags/tags
func (c *Client) TagRelatedTagsResolved(ctx context.Context, id int64, p RelatedTagsResolvedParams) ([]Tag, error) {
	q := url.Values{}
	setBool(q, "omit_empty", p.OmitEmpty)
	setStr(q, "status", p.Status)
	setStr(q, "locale", p.Locale)
	var out []Tag
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d/related-tags/tags", epTags, id), q, &out)
	return out, err
}

// TagRelatedTagsResolvedBySlug is TagRelatedTagsResolved looked up by slug
// instead of id. It needs no authentication.
//
// GET /tags/slug/{slug}/related-tags/tags
func (c *Client) TagRelatedTagsResolvedBySlug(ctx context.Context, slug string, p RelatedTagsResolvedParams) ([]Tag, error) {
	q := url.Values{}
	setBool(q, "omit_empty", p.OmitEmpty)
	setStr(q, "status", p.Status)
	setStr(q, "locale", p.Locale)
	var out []Tag
	err := c.session.Get(ctx, epTags+"/slug/"+slug+"/related-tags/tags", q, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Comments.

// CommentMedia is one media attachment on a Comment, embedded as
// Comment.Media[].
type CommentMedia struct {
	AltText         string `json:"altText"`
	CommentID       int64  `json:"commentID"`
	CreatedAt       string `json:"createdAt"`
	ID              string `json:"id"`
	MediaType       string `json:"mediaType"`
	Provider        string `json:"provider"`
	ProviderMediaID string `json:"providerMediaId"`
	URL             string `json:"url"`
}

// CommentPosition is the commenter's position size in the market a Comment
// was posted on, embedded as CommentProfile.Positions[] — populated only
// when a request sets GetPositions.
type CommentPosition struct {
	PositionSize string `json:"positionSize"`
	TokenID      string `json:"tokenId"`
}

// CommentProfile is the commenter's profile snapshot embedded in each
// Comment.Profile and Reaction.Profile.
type CommentProfile struct {
	BaseAddress           string            `json:"baseAddress"`
	Bio                   string            `json:"bio"`
	DisplayUsernamePublic bool              `json:"displayUsernamePublic"`
	IsCreator             bool              `json:"isCreator"`
	IsMod                 bool              `json:"isMod"`
	Name                  string            `json:"name"`
	Positions             []CommentPosition `json:"positions"`
	ProfileImage          string            `json:"profileImage"`
	ProfileImageOptimized ImageOptimization `json:"profileImageOptimized"`
	ProxyWallet           string            `json:"proxyWallet"`
	Pseudonym             string            `json:"pseudonym"`
}

// A Reaction is one emoji reaction on a Comment, embedded as
// Comment.Reactions[].
type Reaction struct {
	Schema       string         `json:"$schema"`
	CommentID    int64          `json:"commentID"`
	CreatedAt    string         `json:"createdAt"`
	Icon         string         `json:"icon"`
	ID           string         `json:"id"`
	Profile      CommentProfile `json:"profile"`
	ReactionType string         `json:"reactionType"`
	UserAddress  string         `json:"userAddress"`
}

// A Comment is one user comment on an Event, Series or Market.
type Comment struct {
	Schema           string         `json:"$schema"`
	Body             string         `json:"body"`
	CreatedAt        string         `json:"createdAt"`
	ID               string         `json:"id"`
	Media            []CommentMedia `json:"media"`
	ParentCommentID  string         `json:"parentCommentID"`
	ParentEntityID   int64          `json:"parentEntityID"`
	ParentEntityType string         `json:"parentEntityType"`
	Profile          CommentProfile `json:"profile"`
	ReactionCount    int64          `json:"reactionCount"`
	Reactions        []Reaction     `json:"reactions"`
	ReplyAddress     string         `json:"replyAddress"`
	ReportCount      int64          `json:"reportCount"`
	TradeAsset       string         `json:"tradeAsset"`
	UpdatedAt        string         `json:"updatedAt"`
	UserAddress      string         `json:"userAddress"`
}

// CommentsParams filters and paginates GET /comments. ParentEntityType and
// ParentEntityID are effectively required: the live server 422s without
// both, even though the OpenAPI schema marks them optional — set
// ParentEntityType to one of the ParentEntity* constants (note the
// inconsistent casing: Event and Series are capitalized, market is not) and
// ParentEntityID to the numeric id of that event, series or market.
type CommentsParams struct {
	ParentEntityType string
	ParentEntityID   int64
	GetPositions     *bool
	HoldersOnly      *bool
	Limit            int
	Offset           int
	Order            string
	Ascending        *bool
}

// CommentParams are the optional query parameters GET /comments/{id}
// accepts.
type CommentParams struct {
	GetPositions *bool
}

// CommentsByAddressParams paginates GET /comments/user_address/{address}.
type CommentsByAddressParams struct {
	Limit     int
	Offset    int
	Order     string
	Ascending *bool
}

// Comments lists comments on one event, series or market, filtered and
// offset-paginated. It needs no authentication.
//
// GET /comments
func (c *Client) Comments(ctx context.Context, p CommentsParams) ([]Comment, error) {
	q := url.Values{}
	setStr(q, "parent_entity_type", p.ParentEntityType)
	setInt64(q, "parent_entity_id", p.ParentEntityID)
	setBool(q, "get_positions", p.GetPositions)
	setBool(q, "holders_only", p.HoldersOnly)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Comment
	if err := c.session.Get(ctx, epComments, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CommentsByID reports the comment with the given id. Despite the singular
// lookup, Gamma's own schema types the response as an array — decode
// accordingly, and expect at most one element back. It needs no
// authentication.
//
// GET /comments/{id}
func (c *Client) CommentsByID(ctx context.Context, id int64, p CommentParams) ([]Comment, error) {
	q := url.Values{}
	setBool(q, "get_positions", p.GetPositions)
	var out []Comment
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epComments, id), q, &out)
	return out, err
}

// CommentsByUserAddress lists one author's comments, offset-paginated. It
// needs no authentication.
//
// GET /comments/user_address/{user_address}
func (c *Client) CommentsByUserAddress(ctx context.Context, address string, p CommentsByAddressParams) ([]Comment, error) {
	q := url.Values{}
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Comment
	err := c.session.Get(ctx, epComments+"/user_address/"+address, q, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Public search.

// A SearchTag is one tag result in a PublicSearchResponse — a distinct,
// lighter shape from the catalog Tag type, with a live count of matching
// events rather than Tag's administrative fields.
type SearchTag struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Slug       string `json:"slug"`
	EventCount int64  `json:"event_count"`
}

// A SearchProfile is one profile result in a PublicSearchResponse.
type SearchProfile struct {
	Name                  string `json:"name"`
	Pseudonym             string `json:"pseudonym"`
	DisplayUsernamePublic bool   `json:"displayUsernamePublic"`
	ProfileImage          string `json:"profileImage"`
	ProxyWallet           string `json:"proxyWallet"`
}

// PublicSearchResponse is what GET /public-search returns: several result
// categories sharing one Pagination block. Tags and Profiles are populated
// only when the request sets SearchTags / SearchProfiles — the default is
// events-only.
type PublicSearchResponse struct {
	Events     []Event         `json:"events"`
	Tags       []SearchTag     `json:"tags"`
	Profiles   []SearchProfile `json:"profiles"`
	Pagination Pagination      `json:"pagination"`
}

// PublicSearchParams queries GET /public-search. Q is required.
type PublicSearchParams struct {
	Q                 string
	Cache             *bool
	EventsStatus      string
	EventsTag         []string
	KeepClosedMarkets int
	LimitPerType      int
	Page              int
	Sort              string
	Ascending         *bool
	SearchTags        *bool
	SearchProfiles    *bool
	Recurrence        string
	ExcludeTagID      []int64
	Presets           []string
	Optimized         *bool
}

// PublicSearch searches events, and optionally tags and profiles, by free
// text. It needs no authentication.
//
// GET /public-search
func (c *Client) PublicSearch(ctx context.Context, p PublicSearchParams) (PublicSearchResponse, error) {
	q := url.Values{}
	setStr(q, "q", p.Q)
	setBool(q, "cache", p.Cache)
	setStr(q, "events_status", p.EventsStatus)
	setStrs(q, "events_tag", p.EventsTag)
	setInt(q, "keep_closed_markets", p.KeepClosedMarkets)
	setInt(q, "limit_per_type", p.LimitPerType)
	setInt(q, "page", p.Page)
	setStr(q, "sort", p.Sort)
	setBool(q, "ascending", p.Ascending)
	setBool(q, "search_tags", p.SearchTags)
	setBool(q, "search_profiles", p.SearchProfiles)
	setStr(q, "recurrence", p.Recurrence)
	setInt64s(q, "exclude_tag_id", p.ExcludeTagID)
	setStrs(q, "presets", p.Presets)
	setBool(q, "optimized", p.Optimized)
	var out PublicSearchResponse
	err := c.session.Get(ctx, epPublicSearch, q, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Public profiles.

// PublicProfileUser is one platform-role entry in
// PublicProfileResponse.Users[].
type PublicProfileUser struct {
	CommunityMod bool   `json:"communityMod"`
	Creator      bool   `json:"creator"`
	ID           string `json:"id"`
	Mod          bool   `json:"mod"`
}

// PublicProfileResponse is what GET /public-profile returns: the public
// profile for one wallet address.
type PublicProfileResponse struct {
	Schema                string              `json:"$schema"`
	Bio                   string              `json:"bio"`
	CreatedAt             string              `json:"createdAt"`
	DiscordUsername       string              `json:"discordUsername"`
	DisplayUsernamePublic bool                `json:"displayUsernamePublic"`
	Name                  string              `json:"name"`
	ProfileImage          string              `json:"profileImage"`
	ProxyWallet           string              `json:"proxyWallet"`
	Pseudonym             string              `json:"pseudonym"`
	TakerTier             int64               `json:"takerTier"`
	TakerTierName         string              `json:"takerTierName"`
	Users                 []PublicProfileUser `json:"users"`
	VerifiedBadge         bool                `json:"verifiedBadge"`
	WeightedVolume        float64             `json:"weightedVolume"`
	XUsername             string              `json:"xUsername"`
}

// A PublicProfileBatchEntry is one address' profile in a PublicProfiles
// batch lookup. Address identifies which entry answers which query address;
// it and every other field are absent for an address with no profile,
// rather than the whole entry being omitted — UNVERIFIED: not curled with a
// no-profile address.
type PublicProfileBatchEntry struct {
	Address               string `json:"address"`
	Bio                   string `json:"bio"`
	CreatedAt             string `json:"createdAt"`
	DiscordUsername       string `json:"discordUsername"`
	DisplayUsernamePublic bool   `json:"displayUsernamePublic"`
	Name                  string `json:"name"`
	ProfileImage          string `json:"profileImage"`
	ProxyWallet           string `json:"proxyWallet"`
	Pseudonym             string `json:"pseudonym"`
	VerifiedBadge         bool   `json:"verifiedBadge"`
	XUsername             string `json:"xUsername"`
}

// publicProfilesBatch decodes the {$schema, profiles} envelope GET
// /public-profiles returns; PublicProfiles unwraps it to the entry slice
// callers want, matching gammaCount and eventTweetCount's pattern for a
// single-field wire envelope.
type publicProfilesBatch struct {
	Schema   string                    `json:"$schema"`
	Profiles []PublicProfileBatchEntry `json:"profiles"`
}

// PublicProfile reports the public profile for one wallet address. It needs
// no authentication. A miss returns a *polymarket.Error with StatusCode 404;
// a malformed address returns one with StatusCode 400.
//
// GET /public-profile
func (c *Client) PublicProfile(ctx context.Context, address string) (PublicProfileResponse, error) {
	q := url.Values{}
	setStr(q, "address", address)
	var out PublicProfileResponse
	err := c.session.Get(ctx, epPublicProfile, q, &out)
	return out, err
}

// PublicProfiles resolves several wallet addresses to their public profiles
// in one call. It needs no authentication.
//
// GET /public-profiles
func (c *Client) PublicProfiles(ctx context.Context, addresses []string) ([]PublicProfileBatchEntry, error) {
	q := url.Values{}
	setStrs(q, "address", addresses)
	var out publicProfilesBatch
	err := c.session.Get(ctx, epPublicProfiles, q, &out)
	return out.Profiles, err
}

// ---------------------------------------------------------------------------
// Sports and teams.

// sportsMarketTypesResponse decodes the {marketTypes} envelope GET
// /sports/market-types returns; SportsMarketTypes unwraps it.
type sportsMarketTypesResponse struct {
	MarketTypes []string `json:"marketTypes"`
}

// Sports lists every sport and league Gamma tracks metadata for. It takes no
// parameters and needs no authentication.
//
// GET /sports
func (c *Client) Sports(ctx context.Context) ([]SportsMetadata, error) {
	var out []SportsMetadata
	if err := c.session.Get(ctx, epSports, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SportsMarketTypes reports every valid sports market type string — the
// enum domain for Market.SportsMarketType and the sports_market_types filter
// on GET /markets. It needs no authentication.
//
// GET /sports/market-types
func (c *Client) SportsMarketTypes(ctx context.Context) ([]string, error) {
	var out sportsMarketTypesResponse
	err := c.session.Get(ctx, epSportsMktTypes, nil, &out)
	return out.MarketTypes, err
}

// TeamsParams filters and paginates GET /teams.
type TeamsParams struct {
	League       []string
	Name         []string
	Abbreviation []string
	ProviderID   []int64
	Limit        int
	Offset       int
	Order        string
	Ascending    *bool
}

// Teams lists sports teams, filtered and offset-paginated. It needs no
// authentication.
//
// GET /teams
func (c *Client) Teams(ctx context.Context, p TeamsParams) ([]Team, error) {
	q := url.Values{}
	setStrs(q, "league", p.League)
	setStrs(q, "name", p.Name)
	setStrs(q, "abbreviation", p.Abbreviation)
	setInt64s(q, "provider_id", p.ProviderID)
	setPage(q, p.Limit, p.Offset, p.Order, p.Ascending)
	var out []Team
	if err := c.session.Get(ctx, epTeams, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Team reports one sports team by id. It needs no authentication. A miss
// returns a *polymarket.Error with StatusCode 404.
//
// GET /teams/{id}
func (c *Client) Team(ctx context.Context, id int64) (Team, error) {
	var out Team
	err := c.session.Get(ctx, fmt.Sprintf("%s/%d", epTeams, id), nil, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Service health.

// ReadyzStatus is what GET /readyz returns: per-dependency connectivity.
// Only DB gates readiness; Cache and Replica are informational.
type ReadyzStatus struct {
	Schema  string `json:"$schema"`
	DB      string `json:"db"`
	Replica string `json:"replica"`
	Cache   string `json:"cache"`
}

// Status checks that Gamma is reachable. It needs no authentication.
//
// GET /status answers with a text/plain body ("OK"), not JSON, so this
// method passes a nil decode target — Session.Do skips JSON-decoding
// entirely when the target is nil — and reports success or failure purely
// from the HTTP status code, the same shape clob.Client.Ping uses for its
// sibling health check.
//
// GET /status
func (c *Client) Status(ctx context.Context) error {
	return c.session.Get(ctx, epStatus, nil, nil)
}

// Ready checks whether Gamma's dependencies (database, read replica, cache)
// are reachable. Unlike Status, this route answers with real JSON. It needs
// no authentication.
//
// GET /readyz
func (c *Client) Ready(ctx context.Context) (ReadyzStatus, error) {
	var out ReadyzStatus
	err := c.session.Get(ctx, epReadyz, nil, &out)
	return out, err
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test fixtures: live-captured responses, compacted, so the transcription
// tests decode the actual wire shape rather than a hand-written guess.
// gammaServer, checkNoAuth and the marketJSON/eventJSON fixtures come from
// gamma_test.go, same package.

// seriesJSON is a live GET /series?limit=1&exclude_events=true response
// (NFL), captured 2026-08-16.
const seriesJSON = `{"id":"1","ticker":"nfl","slug":"nfl","title":"NFL","seriesType":"single","recurrence":"daily","description":"NFL","layout":"default","active":true,"closed":false,"archived":false,"new":false,"featured":false,"restricted":true,"publishedAt":"2022-10-13 00:34:49.115+00","createdBy":"15","updatedBy":"15","createdAt":"2022-10-13T00:34:06.557Z","updatedAt":"2026-08-16T09:04:53.209867Z","commentsEnabled":false,"competitive":"0","volume24hr":7943.585086,"startDate":"2023-07-01T19:00:00Z","commentCount":3098,"requiresTranslation":false}`

// seriesSummaryJSON is a live GET /series-summary/2 response (NBA),
// captured 2026-08-16, with the long eventDates/eventWeeks arrays trimmed.
const seriesSummaryJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/SeriesSummary.json","id":"2","title":"NBA","slug":"nba","eventDates":["2024-10-22","2024-10-23"],"eventWeeks":[1,2,3],"volume24hr":11.073004}`

// tagJSON is a live GET /tags?limit=1 response, captured 2026-08-16.
const tagJSON = `{"id":"101867","label":"product marekt fit","slug":"product-marekt-fit","createdAt":"2025-02-18T16:58:25.464578Z","updatedAt":"2026-04-17T17:23:11.67487Z","requiresTranslation":false}`

// relatedTagJSON is a live GET /tags/100215/related-tags?omit_empty=true
// entry, captured 2026-08-16.
const relatedTagJSON = `{"id":"42737","tagID":100215,"relatedTagID":126,"rank":1}`

// commentJSON is a live GET /comments?parent_entity_type=Event&parent_entity_id=903193
// entry, captured 2026-08-16.
const commentJSON = `{"id":"3078242","body":"Top holders - guy #1 who lost 1.2m$ will be dead already","parentEntityType":"Event","parentEntityID":903193,"userAddress":"0xd64a35688885e02547d571c3670500bb72e6db08","createdAt":"2026-06-22T22:11:51.597936Z","updatedAt":"2026-06-22T22:11:58.496758Z","profile":{"name":"0xc8510cb479c8305212F519fd990fD160279EF470-1768639087745","pseudonym":"Vibrant-Headache","displayUsernamePublic":true,"proxyWallet":"0xc8510cb479c8305212f519fd990fd160279ef470","baseAddress":"0xd64a35688885e02547d571c3670500bb72e6db08"},"reportCount":0,"reactionCount":0}`

// publicSearchJSON is a live GET
// /public-search?q=trump&limit_per_type=1&search_tags=true&search_profiles=true
// response, captured 2026-08-16, with the (large, full-Event-shaped) events
// array trimmed to an empty one — event decoding is already covered by
// TestEventTranscription in gamma_test.go.
const publicSearchJSON = `{"events":[],"tags":[{"id":"126","label":"Trump","slug":"trump","event_count":178}],"profiles":[{"name":"THE.DONALD.TRUMP","pseudonym":"Unlucky-Lid","displayUsernamePublic":true,"profileImage":"https://example.com/p.png","proxyWallet":"0x455d126afb13bcb59e386a0d59208ac83c35c04b"}],"pagination":{"hasMore":true,"totalResults":3833}}`

// publicProfileJSON is a live GET /public-profile response, captured
// 2026-08-16.
const publicProfileJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/PublicProfileResponse.json","createdAt":"2024-02-09T01:08:31.155094Z","proxyWallet":"0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b","profileImage":"https://example.com/i.png","displayUsernamePublic":true,"bio":"what is your plan","pseudonym":"Peppery-Capital","name":"Car","users":[{"id":"501613","creator":true,"mod":false,"communityMod":true}],"xUsername":"CarOnPolymarket","verifiedBadge":true,"takerTier":2,"takerTierName":"Silver","weightedVolume":77546.069434}`

// publicProfilesBatchJSON is a live GET /public-profiles response, captured
// 2026-08-16.
const publicProfilesBatchJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/PublicProfilesBatchResponse.json","profiles":[{"address":"0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b","createdAt":"2024-02-09T01:08:31.155094Z","proxyWallet":"0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b","profileImage":"https://example.com/i.png","displayUsernamePublic":true,"bio":"what is your plan","pseudonym":"Peppery-Capital","name":"Car","xUsername":"CarOnPolymarket","verifiedBadge":true}]}`

// sportJSON is a live GET /sports entry (UFL), captured 2026-08-16.
const sportJSON = `{"id":630,"sport":"ufl","name":"UFL","image":"https://example.com/ufl.png","resolution":"https://www.theufl.com/","ordering":"away","tags":"1,100639,1186,105925","primaryTagId":105925,"series":"12553","createdAt":"2026-08-07T21:41:19.879539Z"}`

// teamJSON is a live GET /teams?limit=1 entry, captured 2026-08-16.
const teamJSON = `{"id":169458,"name":"United States fe","league":"csgo","record":"0-0","logo":"https://example.com/l.png","abbreviation":"usa","createdAt":"2025-10-21T20:03:50.140246Z","updatedAt":"2025-11-08T03:39:32.338468Z","providerId":135839,"color":"#9A6A7A"}`

// readyzJSON is a live GET /readyz response, captured 2026-08-16.
const readyzJSON = `{"$schema":"https://gamma-api.polymarket.com/schemas/ReadyzStatus.json","db":"ok","replica":"ok","cache":"ok"}`

// ---------------------------------------------------------------------------
// Transcription: decode a real captured response with DisallowUnknownFields
// so a stray or misspelled json tag fails the test.

func decodeStrict(t *testing.T, data string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decoding live fixture: %v", err)
	}
}

func TestSeriesTranscription(t *testing.T) {
	var s Series
	decodeStrict(t, seriesJSON, &s)
	if s.ID != "1" || s.Title != "NFL" {
		t.Errorf("Series = %+v, want id 1, title NFL", s)
	}
	if s.Competitive != "0" {
		t.Errorf("Competitive = %q, want string \"0\" (Series' asymmetry from Market)", s.Competitive)
	}
	if s.Volume24hr != json.Number("7943.585086") {
		t.Errorf("Volume24hr = %q, want 7943.585086", s.Volume24hr)
	}
}

func TestSeriesSummaryTranscription(t *testing.T) {
	var s SeriesSummary
	decodeStrict(t, seriesSummaryJSON, &s)
	if s.ID != "2" || s.Slug != "nba" {
		t.Errorf("SeriesSummary = %+v", s)
	}
	if len(s.EventDates) != 2 || len(s.EventWeeks) != 3 {
		t.Errorf("EventDates/EventWeeks lengths = %d/%d, want 2/3", len(s.EventDates), len(s.EventWeeks))
	}
	if s.Volume24hr != json.Number("11.073004") {
		t.Errorf("Volume24hr = %q, want 11.073004", s.Volume24hr)
	}
	// The fixture omits volume, so it decodes to the empty json.Number —
	// not "0", and not a number big.Rat will parse.
	if s.Volume != "" {
		t.Errorf("Volume = %q, want empty for an omitted amount", s.Volume)
	}
}

func TestTagTranscription(t *testing.T) {
	var tag Tag
	decodeStrict(t, tagJSON, &tag)
	if tag.ID != "101867" || tag.Slug != "product-marekt-fit" {
		t.Errorf("Tag = %+v", tag)
	}
}

func TestRelatedTagTranscription(t *testing.T) {
	var rt RelatedTag
	decodeStrict(t, relatedTagJSON, &rt)
	if rt.TagID != 100215 || rt.RelatedTagID != 126 || rt.Rank != 1 {
		t.Errorf("RelatedTag = %+v", rt)
	}
}

func TestCommentTranscription(t *testing.T) {
	var c Comment
	decodeStrict(t, commentJSON, &c)
	if c.ParentEntityType != "Event" || c.ParentEntityID != 903193 {
		t.Errorf("Comment parent = %q/%d, want Event/903193", c.ParentEntityType, c.ParentEntityID)
	}
	if c.Profile.Pseudonym != "Vibrant-Headache" {
		t.Errorf("Profile.Pseudonym = %q", c.Profile.Pseudonym)
	}
}

func TestPublicSearchResponseTranscription(t *testing.T) {
	var r PublicSearchResponse
	decodeStrict(t, publicSearchJSON, &r)
	if len(r.Tags) != 1 || r.Tags[0].Label != "Trump" || r.Tags[0].EventCount != 178 {
		t.Errorf("Tags = %+v", r.Tags)
	}
	if len(r.Profiles) != 1 || r.Profiles[0].ProxyWallet != "0x455d126afb13bcb59e386a0d59208ac83c35c04b" {
		t.Errorf("Profiles = %+v", r.Profiles)
	}
	if !r.Pagination.HasMore || r.Pagination.TotalResults != 3833 {
		t.Errorf("Pagination = %+v", r.Pagination)
	}
}

func TestPublicProfileResponseTranscription(t *testing.T) {
	var p PublicProfileResponse
	decodeStrict(t, publicProfileJSON, &p)
	if p.TakerTier != 2 || p.TakerTierName != "Silver" || p.WeightedVolume != json.Number("77546.069434") {
		t.Errorf("taker tier fields = %d/%q/%q, want 2/Silver/77546.069434", p.TakerTier, p.TakerTierName, p.WeightedVolume)
	}
	if len(p.Users) != 1 || !p.Users[0].CommunityMod {
		t.Errorf("Users = %+v", p.Users)
	}
}

func TestPublicProfilesBatchTranscription(t *testing.T) {
	var b publicProfilesBatch
	decodeStrict(t, publicProfilesBatchJSON, &b)
	if len(b.Profiles) != 1 || b.Profiles[0].Address != "0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b" {
		t.Errorf("Profiles = %+v", b.Profiles)
	}
}

func TestSportsMetadataTranscriptionAndDecode(t *testing.T) {
	var s SportsMetadata
	decodeStrict(t, sportJSON, &s)
	if s.ID != 630 || s.Sport != "ufl" || s.PrimaryTagID != 105925 {
		t.Errorf("SportsMetadata = %+v", s)
	}
	tagIDs, err := s.DecodeTagIDs()
	if err != nil {
		t.Fatalf("DecodeTagIDs: %v", err)
	}
	if want := []int64{1, 100639, 1186, 105925}; !int64sEqual(tagIDs, want) {
		t.Errorf("DecodeTagIDs() = %v, want %v", tagIDs, want)
	}
	seriesIDs, err := s.DecodeSeriesIDs()
	if err != nil {
		t.Fatalf("DecodeSeriesIDs: %v", err)
	}
	if want := []int64{12553}; !int64sEqual(seriesIDs, want) {
		t.Errorf("DecodeSeriesIDs() = %v, want %v", seriesIDs, want)
	}
}

func TestDecodeCommaIDsEmpty(t *testing.T) {
	got, err := decodeCommaIDs("")
	if err != nil || got != nil {
		t.Errorf("decodeCommaIDs(\"\") = %v, %v, want nil, nil", got, err)
	}
	if _, err := decodeCommaIDs("1,x,3"); err == nil {
		t.Error("decodeCommaIDs(\"1,x,3\") returned nil error, want one")
	}
}

func int64sEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTeamTranscription(t *testing.T) {
	var team Team
	decodeStrict(t, teamJSON, &team)
	if team.ID != 169458 || team.ProviderID != 135839 || team.Color != "#9A6A7A" {
		t.Errorf("Team = %+v", team)
	}
}

func TestReadyzStatusTranscription(t *testing.T) {
	var r ReadyzStatus
	decodeStrict(t, readyzJSON, &r)
	if r.DB != "ok" || r.Replica != "ok" || r.Cache != "ok" {
		t.Errorf("ReadyzStatus = %+v", r)
	}
}

// ---------------------------------------------------------------------------
// HTTP-level behavior: method/path/query wiring, and the parameter
// asymmetries this file's top-level doc comment calls out.

func TestSeriesListAndByID(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		switch r.URL.Path {
		case epSeries:
			if r.URL.Query().Get("closed") != "true" {
				t.Errorf("query = %v, want closed=true", r.URL.Query())
			}
			w.Write([]byte("[" + seriesJSON + "]"))
		case "/series/1":
			w.Write([]byte(seriesJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	closed := true
	got, err := c.Series(context.Background(), SeriesParams{Closed: &closed})
	if err != nil || len(got) != 1 {
		t.Fatalf("Series() = %v, %v", got, err)
	}
	s, err := c.SeriesByID(context.Background(), 1, SeriesDetailParams{})
	if err != nil || s.ID != "1" {
		t.Errorf("SeriesByID() = %+v, %v", s, err)
	}
}

func TestSeriesCommentsCountAndSummary(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/2/comments/count":
			w.Write([]byte(`{"count":6279}`))
		case "/series-summary/2":
			w.Write([]byte(seriesSummaryJSON))
		case "/series-summary/slug/nba":
			w.Write([]byte(seriesSummaryJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	cc, err := c.SeriesCommentsCount(context.Background(), 2)
	if err != nil || cc != 6279 {
		t.Errorf("SeriesCommentsCount() = %d, %v, want 6279", cc, err)
	}
	sum, err := c.SeriesSummaryByID(context.Background(), 2)
	if err != nil || sum.Slug != "nba" {
		t.Errorf("SeriesSummaryByID() = %+v, %v", sum, err)
	}
	sum2, err := c.SeriesSummaryBySlug(context.Background(), "nba")
	if err != nil || sum2.ID != "2" {
		t.Errorf("SeriesSummaryBySlug() = %+v, %v", sum2, err)
	}
}

func TestTagsListByIDAndBySlug(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epTags:
			w.Write([]byte("[" + tagJSON + "]"))
		case "/tags/101867":
			if r.URL.Query().Get("include_template") != "true" {
				t.Errorf("query = %v, want include_template=true", r.URL.Query())
			}
			w.Write([]byte(tagJSON))
		case "/tags/slug/product-marekt-fit":
			if r.URL.Query().Has("include_template") {
				t.Errorf("slug lookup carries include_template, want none (route does not accept it)")
			}
			w.Write([]byte(tagJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	if _, err := c.Tags(context.Background(), TagsParams{}); err != nil {
		t.Fatal(err)
	}
	inc := true
	if _, err := c.Tag(context.Background(), 101867, TagDetailParams{IncludeTemplate: &inc}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TagBySlug(context.Background(), "product-marekt-fit", TagBySlugParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestTagRelatedTagsRoutesAndAsymmetry(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tags/100215/related-tags":
			w.Write([]byte("[" + relatedTagJSON + "]"))
		case "/tags/slug/trump/related-tags":
			if len(r.URL.Query()) != 0 {
				t.Errorf("query = %v, want none: this route accepts no parameters", r.URL.Query())
			}
			w.Write([]byte("[" + relatedTagJSON + "]"))
		case "/tags/100215/related-tags/tags":
			w.Write([]byte("[" + tagJSON + "]"))
		case "/tags/slug/trump/related-tags/tags":
			w.Write([]byte("[" + tagJSON + "]"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	if _, err := c.TagRelatedTags(context.Background(), 100215, RelatedTagsParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TagRelatedTagsBySlug(context.Background(), "trump"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TagRelatedTagsResolved(context.Background(), 100215, RelatedTagsResolvedParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TagRelatedTagsResolvedBySlug(context.Background(), "trump", RelatedTagsResolvedParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentsRequiredParams(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("parent_entity_type") != ParentEntityEvent {
			t.Errorf("parent_entity_type = %q, want %q", q.Get("parent_entity_type"), ParentEntityEvent)
		}
		if q.Get("parent_entity_id") != "903193" {
			t.Errorf("parent_entity_id = %q, want 903193", q.Get("parent_entity_id"))
		}
		w.Write([]byte("[" + commentJSON + "]"))
	})
	got, err := c.Comments(context.Background(), CommentsParams{
		ParentEntityType: ParentEntityEvent,
		ParentEntityID:   903193,
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("Comments() = %v, %v", got, err)
	}
}

func TestCommentsByIDReturnsArray(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments/3078242" {
			t.Errorf("path = %s, want /comments/3078242", r.URL.Path)
		}
		w.Write([]byte("[" + commentJSON + "]"))
	})
	got, err := c.CommentsByID(context.Background(), 3078242, CommentParams{})
	if err != nil || len(got) != 1 || got[0].ID != "3078242" {
		t.Errorf("CommentsByID() = %+v, %v", got, err)
	}
}

func TestCommentsByUserAddress(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments/user_address/0xabc" {
			t.Errorf("path = %s, want /comments/user_address/0xabc", r.URL.Path)
		}
		w.Write([]byte("[" + commentJSON + "]"))
	})
	got, err := c.CommentsByUserAddress(context.Background(), "0xabc", CommentsByAddressParams{})
	if err != nil || len(got) != 1 {
		t.Errorf("CommentsByUserAddress() = %v, %v", got, err)
	}
}

func TestPublicSearchQuery(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "trump" {
			t.Errorf("q = %q, want trump", q.Get("q"))
		}
		if q.Get("search_tags") != "true" || q.Get("search_profiles") != "true" {
			t.Errorf("query = %v, want search_tags/search_profiles true", q)
		}
		w.Write([]byte(publicSearchJSON))
	})
	yes := true
	got, err := c.PublicSearch(context.Background(), PublicSearchParams{
		Q: "trump", SearchTags: &yes, SearchProfiles: &yes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 1 || got.Tags[0].Slug != "trump" {
		t.Errorf("Tags = %+v", got.Tags)
	}
}

func TestPublicProfileAndBatch(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epPublicProfile:
			if r.URL.Query().Get("address") != "0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b" {
				t.Errorf("address = %q", r.URL.Query().Get("address"))
			}
			w.Write([]byte(publicProfileJSON))
		case epPublicProfiles:
			w.Write([]byte(publicProfilesBatchJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	p, err := c.PublicProfile(context.Background(), "0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b")
	if err != nil || p.Name != "Car" {
		t.Errorf("PublicProfile() = %+v, %v", p, err)
	}
	batch, err := c.PublicProfiles(context.Background(), []string{"0x7c3db723f1d4d8cb9c550095203b686cb11e5c6b"})
	if err != nil || len(batch) != 1 {
		t.Errorf("PublicProfiles() = %+v, %v", batch, err)
	}
}

func TestSportsAndMarketTypesAndTeams(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epSports:
			w.Write([]byte("[" + sportJSON + "]"))
		case epSportsMktTypes:
			w.Write([]byte(`{"marketTypes":["moneyline","spreads","totals"]}`))
		case epTeams:
			w.Write([]byte("[" + teamJSON + "]"))
		case "/teams/169458":
			w.Write([]byte(teamJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	sports, err := c.Sports(context.Background())
	if err != nil || len(sports) != 1 {
		t.Fatalf("Sports() = %v, %v", sports, err)
	}
	mt, err := c.SportsMarketTypes(context.Background())
	if err != nil || len(mt) != 3 || mt[0] != "moneyline" {
		t.Errorf("SportsMarketTypes() = %v, %v", mt, err)
	}
	teams, err := c.Teams(context.Background(), TeamsParams{})
	if err != nil || len(teams) != 1 {
		t.Fatalf("Teams() = %v, %v", teams, err)
	}
	team, err := c.Team(context.Background(), 169458)
	if err != nil || team.ID != 169458 {
		t.Errorf("Team() = %+v, %v", team, err)
	}
}

func TestTeamsQueryParams(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q["league"]; len(got) != 1 || got[0] != "csgo" {
			t.Errorf("league = %v, want [csgo]", got)
		}
		if got := q["provider_id"]; len(got) != 1 || got[0] != "135839" {
			t.Errorf("provider_id = %v, want [135839]", got)
		}
		w.Write([]byte("[]"))
	})
	if _, err := c.Teams(context.Background(), TeamsParams{
		League:     []string{"csgo"},
		ProviderID: []int64{135839},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStatusIgnoresPlainTextBody proves Status tolerates the text/plain "OK"
// body /status actually returns (not valid JSON): it must not surface a
// decode error for a successful response.
func TestStatusIgnoresPlainTextBody(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epStatus {
			t.Errorf("path = %s, want %s", r.URL.Path, epStatus)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("OK"))
	})
	if err := c.Status(context.Background()); err != nil {
		t.Errorf("Status() = %v, want nil", err)
	}
}

func TestStatusSurfacesHTTPError(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if err := c.Status(context.Background()); err == nil {
		t.Error("Status() = nil, want an error for a 503 response")
	}
}

func TestReady(t *testing.T) {
	c := gammaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epReadyz {
			t.Errorf("path = %s, want %s", r.URL.Path, epReadyz)
		}
		w.Write([]byte(readyzJSON))
	})
	got, err := c.Ready(context.Background())
	if err != nil || got.DB != "ok" {
		t.Errorf("Ready() = %+v, %v", got, err)
	}
}

// ---------------------------------------------------------------------------
// Sanity check that url.Values is used as intended for repeated params
// (no accidental comma-joining).

func TestSetStrsRepeatsKeyRatherThanJoining(t *testing.T) {
	q := url.Values{}
	setStrs(q, "slug", []string{"a", "b"})
	if got := q["slug"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("slug = %v, want two repeated values [a b]", got)
	}
}

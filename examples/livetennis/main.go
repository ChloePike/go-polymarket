// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command livetennis pairs a Polymarket tennis market with the live state of
// the same match, and prints the two side by side.
//
// A Polymarket price is a probability; on a tennis market the two outcome
// tokens are the two players. Knowing the market's price is only half the
// picture — the other half is what is happening on court right now: the game
// score, who is serving, whether the current point is a break point, and
// whether the match has been retired, given as a walkover or completed. This
// command reads the first from Polymarket's public CLOB and the second from a
// live-tennis data feed, matches them by player name, and renders one block.
//
//	go run ./examples/livetennis
//	go run ./examples/livetennis -condition 0x1234...   # one specific market
//
// It is strictly read-only. The Polymarket side needs no wallet and no API
// key; nothing here signs anything or places an order. The live-match side is
// an external data source — the Live Tennis API's free keyed tier (30
// requests/minute, 100/day, no card): set LIVETENNIS_API_KEY, get a key at
// https://livetennisapi.com/subscribe/free.
//
// Vendor disclosure: this example, and the live-tennis data feed it reads,
// are contributed by the Live Tennis API team (https://livetennisapi.com).
// It adds no dependency to the module — the feed is read with net/http from
// the standard library — and touches no Polymarket write path.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ChloePike/go-polymarket/clob"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	condition := flag.String("condition", "", "condition id of one market; empty scans for a live tennis fixture")
	base := flag.String("base", liveTennisBaseURL, "live-tennis API base URL")
	flag.Parse()

	key := os.Getenv("LIVETENNIS_API_KEY")
	if key == "" {
		slog.Error("no live-tennis API key",
			"fix", "set LIVETENNIS_API_KEY",
			"free_key", "https://livetennisapi.com/subscribe/free")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := clob.New()
	tennis := &tennisClient{base: strings.TrimRight(*base, "/"), key: key, http: http.DefaultClient}

	// The live-tennis feed is the scarce, rate-limited side, so fetch it once
	// and match every market against the same snapshot.
	live, err := tennis.liveMatches(ctx)
	if err != nil {
		slog.Error("fetching live tennis matches", "err", err)
		os.Exit(1)
	}
	if len(live) == 0 {
		fmt.Println("no tennis matches are live right now")
		return
	}

	if *condition != "" {
		market, err := c.Market(ctx, *condition)
		if err != nil {
			slog.Error("fetching the market", "condition", *condition, "err", err)
			os.Exit(1)
		}
		decision := matchMarket(market, live)
		if decision == nil {
			slog.Error("no live tennis match confidently pairs with this market",
				"question", market.Question)
			os.Exit(1)
		}
		render(ctx, c, market, decision)
		return
	}

	market, decision, err := discover(ctx, c, live)
	if err != nil {
		slog.Error("scanning for a live tennis fixture", "err", err)
		os.Exit(1)
	}
	if decision == nil {
		fmt.Printf("scanned Polymarket fixtures but none paired with the %d live tennis match(es); pass -condition\n", len(live))
		return
	}
	render(ctx, c, market, decision)
}

// discover walks the CLOB sampling markets, which are the markets Polymarket
// is actively running, and returns the first fixture that pairs with a live
// tennis match. Restricting to sampling markets keeps the scan bounded and,
// like the book and watch examples, keeps a demo pointed at a sporting
// fixture rather than a contested subject.
func discover(ctx context.Context, c *clob.Client, live []liveMatch) (clob.Market, *matchDecision, error) {
	const scanLimit = 2000
	scanned := 0
	for m, err := range clob.Pages(ctx, c.SamplingMarkets) {
		if err != nil {
			return clob.Market{}, nil, err
		}
		if scanned++; scanned > scanLimit {
			break
		}
		if !m.Active || m.Closed || !looksLikeFixture(m.Question) {
			continue
		}
		if decision := matchMarket(m, live); decision != nil {
			return m, decision, nil
		}
	}
	return clob.Market{}, nil, nil
}

// looksLikeFixture is a cheap pre-filter — "A vs. B" is how Polymarket writes
// a head-to-head — before the real test, which is whether a live tennis match
// actually pairs with it.
func looksLikeFixture(question string) bool {
	q := strings.ToLower(question)
	return strings.Contains(q, " vs. ") || strings.Contains(q, " vs ")
}

// render prints one market next to its live match. Prices stay as the decimal
// strings the wire carries — a Polymarket price is money and never a float64.
func render(ctx context.Context, c *clob.Client, m clob.Market, d *matchDecision) {
	fmt.Printf("%s  [%s]\n", m.Question, m.MarketSlug)

	priceBits := make([]string, 0, len(m.Tokens))
	for _, t := range m.Tokens {
		price := t.Price.String()
		if price == "" {
			price = "?"
		}
		priceBits = append(priceBits, fmt.Sprintf("%s %s", t.Outcome, price))
	}
	midpoint := "?"
	if len(m.Tokens) > 0 {
		if mid, err := c.Midpoint(ctx, m.Tokens[0].TokenID); err == nil {
			midpoint = mid
		}
	}
	fmt.Printf("  market: %s   (midpoint %s)\n", strings.Join(priceBits, " | "), midpoint)

	match := d.match
	p1 := match.Players.P1.Name
	p2 := match.Players.P2.Name
	if p1 == "" {
		p1 = "?"
	}
	if p2 == "" {
		p2 = "?"
	}

	serving := ""
	if match.Score.Server != nil {
		switch *match.Score.Server {
		case 1:
			serving = "  serving: " + p1
		case 2:
			serving = "  serving: " + p2
		}
	}

	flags := make([]string, 0, 3)
	if deriveBreakPoint(match.Score) {
		flags = append(flags, "BREAK POINT")
	}
	if match.Score.IsTiebreak {
		flags = append(flags, "tiebreak")
	}
	if match.EventStatus != "" {
		flags = append(flags, match.EventStatus)
	}
	flagText := ""
	if len(flags) > 0 {
		flagText = fmt.Sprintf("  [%s]", strings.Join(flags, ", "))
	}

	status := match.Status
	if status == "" {
		status = "?"
	}
	fmt.Printf("  live:   %s vs %s  %s%s%s   (match %d, status %s, matched by %s, confidence %d%%)\n",
		p1, p2, scoreLine(match.Score), serving, flagText,
		match.ID, status, d.method, d.confidence)
}

// -- the live-tennis data feed -----------------------------------------------

const liveTennisBaseURL = "https://api.livetennisapi.com/api/public/v1"

// tennisClient is a minimal read-only client for the Live Tennis API's free
// keyed tier. It exists only to feed this example; the module itself takes on
// no tennis dependency.
type tennisClient struct {
	base string
	key  string
	http *http.Client
}

// matchesEnvelope is the response shape the feed's list endpoints share:
// the rows live under "data".
type matchesEnvelope struct {
	Data []liveMatch `json:"data"`
}

// liveMatch is one match as the feed reports it: identity, lifecycle status,
// the two players and the live score object.
type liveMatch struct {
	ID          int          `json:"id"`
	Status      string       `json:"status"`
	EventStatus string       `json:"event_status"`
	Players     matchPlayers `json:"players"`
	Score       scoreObject  `json:"score"`
}

// matchPlayers names the two sides. p1 serves first in the feed's convention;
// which side is serving right now is score.server.
type matchPlayers struct {
	P1 player `json:"p1"`
	P2 player `json:"p2"`
}

// player is one competitor; only the display name is needed here.
type player struct {
	Name string `json:"name"`
}

// scoreObject is the feed's live score: set counts, games per set, the
// current-game points, who is serving, and whether the game is a tiebreak.
type scoreObject struct {
	Sets       []int        `json:"sets"`
	Games      [][]int      `json:"games"`
	Points     []pointScore `json:"points"`
	Server     *int         `json:"server"`
	IsTiebreak bool         `json:"is_tiebreak"`
	Timestamp  string       `json:"timestamp"`
}

// pointScore is one player's current-game point. The feed writes it as a
// string outside a tiebreak ("0", "15", "30", "40", "AD"), as a number inside
// one, and as null when there is no live point (a completed match). ok is
// false for the null case.
type pointScore struct {
	text string
	ok   bool
}

// UnmarshalJSON accepts the string, number and null forms the feed uses.
func (p *pointScore) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "null" || trimmed == "" {
		p.text, p.ok = "", false
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		p.text, p.ok = s, true
		return nil
	}
	p.text, p.ok = trimmed, true
	return nil
}

func (t *tennisClient) liveMatches(ctx context.Context) ([]liveMatch, error) {
	q := url.Values{"status": {"live"}, "limit": {"50"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+"/matches?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", t.key)
	req.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("live-tennis rate limit hit (free tier: 30 req/min, 100/day) — slow the polling cadence")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("live-tennis /matches: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var env matchesEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding /matches: %w", err)
	}
	return env.Data, nil
}

// -- score reading -----------------------------------------------------------

// deriveBreakPoint reports whether the current point is a break point, by the
// Live Tennis API's documented rule: a break point is on when the RECEIVER is
// at AD, or the receiver is at 40 while the server is at 0/15/30. It is never
// on in a tiebreak, and it is false whenever the server or the points are
// absent — a completed match carries a null server and null points.
func deriveBreakPoint(s scoreObject) bool {
	if s.IsTiebreak {
		return false
	}
	if s.Server == nil || (*s.Server != 1 && *s.Server != 2) {
		return false
	}
	if len(s.Points) != 2 || !s.Points[0].ok || !s.Points[1].ok {
		return false
	}
	var receiver, server string
	if *s.Server == 1 {
		receiver, server = s.Points[1].text, s.Points[0].text
	} else {
		receiver, server = s.Points[0].text, s.Points[1].text
	}
	if receiver == "AD" {
		return true
	}
	return receiver == "40" && (server == "0" || server == "15" || server == "30")
}

// scoreLine renders "6-4 3-2 (40-15)" from a score object: one "a-b" per
// completed and in-progress set, then the current-game points in parentheses
// when both are present.
func scoreLine(s scoreObject) string {
	parts := make([]string, 0, len(s.Games)+1)
	if len(s.Games) == 2 && len(s.Games[0]) > 0 && len(s.Games[0]) == len(s.Games[1]) {
		for i := range s.Games[0] {
			parts = append(parts, fmt.Sprintf("%d-%d", s.Games[0][i], s.Games[1][i]))
		}
	}
	if len(s.Points) == 2 && s.Points[0].ok && s.Points[1].ok {
		parts = append(parts, fmt.Sprintf("(%s-%s)", s.Points[0].text, s.Points[1].text))
	}
	return strings.Join(parts, " ")
}

// -- pairing a market with a match -------------------------------------------

// matchDecision is a confident market-to-match pairing: which match, how it
// was decided, and a 0-100 confidence.
type matchDecision struct {
	match      liveMatch
	method     string // "names" or "names (reversed)"
	confidence int
}

// matchThreshold is the minimum confidence a pairing must clear. It is
// deliberately conservative: the command reports nothing rather than pair the
// wrong match, in the spirit of the reference matcher.
const matchThreshold = 70

// matchMarket returns the live match that pairs with a market, or nil when
// none is confident enough or two are too close to separate.
func matchMarket(m clob.Market, live []liveMatch) *matchDecision {
	n1, n2, ok := marketNames(m)
	if !ok {
		return nil
	}
	// Doubles ("A/B vs C/D") is a different matching problem; skip it here.
	if strings.Contains(n1, "/") || strings.Contains(n2, "/") {
		return nil
	}

	bestScore, secondScore := -1, -1
	var best liveMatch
	var reversed bool
	for _, candidate := range live {
		c1, c2 := candidate.Players.P1.Name, candidate.Players.P2.Name
		direct := min(nameSimilarity(n1, c1), nameSimilarity(n2, c2))
		flip := min(nameSimilarity(n1, c2), nameSimilarity(n2, c1))
		score := direct
		rev := false
		if flip > direct {
			score, rev = flip, true
		}
		switch {
		case score > bestScore:
			secondScore = bestScore
			bestScore, best, reversed = score, candidate, rev
		case score > secondScore:
			secondScore = score
		}
	}

	if bestScore < matchThreshold {
		return nil
	}
	// Ambiguous when the runner-up is within 10 points of the leader.
	if secondScore >= 0 && bestScore-secondScore < 10 {
		return nil
	}
	method := "names"
	if reversed {
		method = "names (reversed)"
	}
	return &matchDecision{match: best, method: method, confidence: bestScore}
}

// marketNames pulls the two player names from a market: the two outcome
// labels when they are not a Yes/No question, otherwise the "… A vs. B" tail
// of the question. ok is false when two sides cannot be identified.
func marketNames(m clob.Market) (string, string, bool) {
	if len(m.Tokens) == 2 {
		a, b := strings.TrimSpace(m.Tokens[0].Outcome), strings.TrimSpace(m.Tokens[1].Outcome)
		la, lb := strings.ToLower(a), strings.ToLower(b)
		if a != "" && b != "" && !(la == "yes" || la == "no") && !(lb == "yes" || lb == "no") {
			return a, b, true
		}
	}
	title := m.Question
	if i := strings.LastIndex(title, ":"); i >= 0 {
		title = title[i+1:]
	}
	lower := strings.ToLower(title)
	sep := " vs. "
	if !strings.Contains(lower, sep) {
		sep = " vs "
	}
	if idx := strings.Index(lower, sep); idx >= 0 {
		a := strings.TrimSpace(title[:idx])
		b := strings.TrimSpace(title[idx+len(sep):])
		if a != "" && b != "" {
			return a, b, true
		}
	}
	return "", "", false
}

// nameSimilarity scores two player names 0-100: 100 when they fold equal, 90
// when one is a token-subset of the other ("J. Lehecka" vs "Jiri Lehecka"),
// 75 on surname-only agreement, 0 otherwise. Names are money-free, so an
// integer scale is used deliberately — no float confidences.
func nameSimilarity(a, b string) int {
	fa, fb := foldName(a), foldName(b)
	if fa == "" || fb == "" {
		return 0
	}
	if fa == fb {
		return 100
	}
	ta, tb := strings.Fields(fa), strings.Fields(fb)
	if isSubset(ta, tb) || isSubset(tb, ta) {
		return 90
	}
	if len(ta) > 0 && len(tb) > 0 && ta[len(ta)-1] == tb[len(tb)-1] {
		return 75
	}
	return 0
}

// isSubset reports whether every token of a appears in b.
func isSubset(a, b []string) bool {
	set := make(map[string]struct{}, len(b))
	for _, tok := range b {
		set[tok] = struct{}{}
	}
	for _, tok := range a {
		if _, present := set[tok]; !present {
			return false
		}
	}
	return true
}

// foldName lowercases a name, folds the common Latin diacritics tennis draws
// carry, turns punctuation into spaces and collapses whitespace. It is a
// lightweight fold — the reference toolkit does full Unicode NFKD — chosen so
// this example adds no dependency to the module.
func foldName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	folded := diacriticFolder.Replace(lowered)
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// diacriticFolder maps the accented Latin letters common in tennis names to
// their ASCII base. Applied after lowercasing, so only lowercase forms appear.
var diacriticFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "ā", "a",
	"ç", "c", "ć", "c", "č", "c",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "ě", "e", "ę", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ñ", "n", "ń", "n", "ň", "n",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"š", "s", "ś", "s", "ş", "s",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ū", "u",
	"ý", "y", "ÿ", "y",
	"ž", "z", "ź", "z", "ż", "z",
	"ð", "d", "đ", "d", "ł", "l", "ß", "ss",
)

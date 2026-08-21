package main

import (
	"testing"

	"github.com/ChloePike/go-polymarket/clob"
)

func srv(n int) *int { return &n }

func clobMarketStub(a, b string) clob.Market {
	return clob.Market{
		Question: a + " vs. " + b,
		Tokens: []clob.MarketToken{
			{Outcome: a, TokenID: "t1"},
			{Outcome: b, TokenID: "t2"},
		},
	}
}

func TestSanity(t *testing.T) {
	// receiver at 40, server 30 -> break point (server=1 means p1 serves, so
	// receiver is points[1])
	bp := scoreObject{Server: srv(1), Points: []pointScore{{"30", true}, {"40", true}}}
	if !deriveBreakPoint(bp) {
		t.Fatal("expected break point at 30-40 with p1 serving")
	}
	// receiver AD
	adv := scoreObject{Server: srv(2), Points: []pointScore{{"AD", true}, {"40", true}}}
	if !deriveBreakPoint(adv) {
		t.Fatal("expected break point at receiver AD (p2 serving)")
	}
	// tiebreak never a break point
	tb := scoreObject{Server: srv(1), IsTiebreak: true, Points: []pointScore{{"6", true}, {"5", true}}}
	if deriveBreakPoint(tb) {
		t.Fatal("tiebreak must not be a break point")
	}
	// null points -> false
	none := scoreObject{Server: srv(1), Points: []pointScore{{"", false}, {"", false}}}
	if deriveBreakPoint(none) {
		t.Fatal("null points must not be a break point")
	}
	// score line
	sl := scoreLine(scoreObject{Games: [][]int{{6, 3}, {4, 2}}, Points: []pointScore{{"40", true}, {"15", true}}})
	if sl != "6-4 3-2 (40-15)" {
		t.Fatalf("score line: got %q", sl)
	}
	// name similarity
	if nameSimilarity("Carlos Alcaraz", "carlos alcaraz") != 100 {
		t.Fatal("exact fold should be 100")
	}
	if nameSimilarity("Davidovich Fokina", "Alejandro Davidovich Fokina") != 90 {
		t.Fatalf("token-subset should be 90, got %d", nameSimilarity("Davidovich Fokina", "Alejandro Davidovich Fokina"))
	}
	if nameSimilarity("J. Lehecka", "Jiri Lehecka") != 75 {
		t.Fatalf("surname-only agreement should be 75, got %d", nameSimilarity("J. Lehecka", "Jiri Lehecka"))
	}
	if nameSimilarity("Cerundolo", "Cerúndolo") != 100 {
		t.Fatalf("diacritic fold mismatch: %d", nameSimilarity("Cerundolo", "Cerúndolo"))
	}
	// matching a market to a live match
	m := clobMarketStub("Carlos Alcaraz", "Jannik Sinner")
	live := []liveMatch{{ID: 42, Status: "live", Players: matchPlayers{P1: player{"Jannik Sinner"}, P2: player{"Carlos Alcaraz"}}}}
	d := matchMarket(m, live)
	if d == nil || d.match.ID != 42 || d.method != "names (reversed)" {
		t.Fatalf("expected reversed pairing to match 42, got %+v", d)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBackoffDurationMonotonicAndCapped(t *testing.T) {
	prevMin := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		d := backoffDuration(attempt)
		if d < initialBackoff {
			t.Errorf("attempt %d: backoffDuration = %v, want >= %v", attempt, d, initialBackoff)
		}
		maxPossible := maxBackoff + maxBackoff*backoffJitterPercent/100
		if d > maxPossible {
			t.Errorf("attempt %d: backoffDuration = %v, want <= %v", attempt, d, maxPossible)
		}
		// The un-jittered floor should climb (or stay at the cap) with each
		// attempt; check that this attempt's value is not smaller than the
		// previous attempt's floor.
		floor := initialBackoff
		for i := 1; i < attempt && floor < maxBackoff; i++ {
			floor *= 2
		}
		if floor > maxBackoff {
			floor = maxBackoff
		}
		if floor < prevMin {
			t.Errorf("attempt %d: floor %v decreased from previous %v", attempt, floor, prevMin)
		}
		prevMin = floor
	}
}

// splitFrameCase is one table-driven case for TestSplitFrame.
type splitFrameCase struct {
	name    string
	frame   string
	wantLen int
	wantErr bool
}

var splitFrameCases = []splitFrameCase{
	{name: "empty", frame: "", wantLen: 0},
	{name: "whitespace only", frame: "   \n", wantLen: 0},
	{name: "bare object", frame: `{"a":1}`, wantLen: 1},
	{name: "array of two objects", frame: `[{"a":1},{"b":2}]`, wantLen: 2},
	{name: "empty array", frame: `[]`, wantLen: 0},
	{name: "not JSON", frame: `PONG`, wantErr: true},
	{name: "malformed array", frame: `[{"a":1}`, wantErr: true},
}

func TestSplitFrame(t *testing.T) {
	for _, tc := range splitFrameCases {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := splitFrame([]byte(tc.frame))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitFrame(%q) = %v, nil; want error", tc.frame, objs)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitFrame(%q): unexpected error: %v", tc.frame, err)
			}
			if len(objs) != tc.wantLen {
				t.Errorf("splitFrame(%q) = %d objects, want %d", tc.frame, len(objs), tc.wantLen)
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]struct{}{"c": {}, "a": {}, "b": {}}
	got := sortedKeys(m)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("sortedKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestNumberUnmarshal confirms Number accepts both a bare JSON number token
// and a JSON string, per the doc-inconsistency this type exists to paper
// over.
type numberCase struct {
	name string
	json string
	want string
}

var numberCases = []numberCase{
	{name: "bare number", json: `67234.5`, want: "67234.5"},
	{name: "quoted string", json: `"189.42"`, want: "189.42"},
	{name: "integer", json: `100`, want: "100"},
}

func TestNumberUnmarshal(t *testing.T) {
	for _, tc := range numberCases {
		t.Run(tc.name, func(t *testing.T) {
			var n Number
			if err := json.Unmarshal([]byte(tc.json), &n); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.json, err)
			}
			if n.String() != tc.want {
				t.Errorf("got %q, want %q", n.String(), tc.want)
			}
		})
	}
}

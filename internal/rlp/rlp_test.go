// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package rlp

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
)

// rlpVectors is the slice of testdata/tx-vectors.json this package needs. The
// vectors come from a general Ethereum client, because RLP is Ethereum's
// format rather than Polymarket's.
type rlpVectors struct {
	RLP      []rlpStringCase `json:"rlp"`
	RLPLists []rlpListCase   `json:"rlpLists"`
}

// rlpStringCase is one byte string and its encoding.
type rlpStringCase struct {
	Name    string `json:"name"`
	Input   string `json:"input"`
	Encoded string `json:"encoded"`
}

// rlpListCase is one list of byte strings and its encoding.
type rlpListCase struct {
	Name    string   `json:"name"`
	Items   []string `json:"items"`
	Encoded string   `json:"encoded"`
}

func loadVectors(t *testing.T) rlpVectors {
	t.Helper()
	b, err := os.ReadFile("../../testdata/tx-vectors.json")
	if err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	var v rlpVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	return v
}

// decodeHex turns a 0x string from the vectors into bytes.
func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatalf("vector %q is not hex: %v", s, err)
	}
	return b
}

// TestStringVectors pins the byte-string encoding, including the boundary at
// 55 bytes where the prefix stops carrying the length itself.
func TestStringVectors(t *testing.T) {
	v := loadVectors(t)
	if len(v.RLP) == 0 {
		t.Fatal("no string vectors")
	}
	for _, tc := range v.RLP {
		t.Run(tc.Name, func(t *testing.T) {
			got := String(decodeHex(t, tc.Input))
			if want := decodeHex(t, tc.Encoded); !equal(got, want) {
				t.Errorf("String(%s) = %x, want %x", tc.Input, got, want)
			}
		})
	}
}

// TestListVectors pins the list encoding.
func TestListVectors(t *testing.T) {
	v := loadVectors(t)
	if len(v.RLPLists) == 0 {
		t.Fatal("no list vectors")
	}
	for _, tc := range v.RLPLists {
		t.Run(tc.Name, func(t *testing.T) {
			items := make([][]byte, len(tc.Items))
			for i, item := range tc.Items {
				items[i] = String(decodeHex(t, item))
			}
			got := List(items...)
			if want := decodeHex(t, tc.Encoded); !equal(got, want) {
				t.Errorf("List(%v) = %x, want %x", tc.Items, got, want)
			}
		})
	}
}

// uintCase is one integer and the encoding it must produce.
type uintCase struct {
	name  string
	value *big.Int
	want  string
}

// uintCases pin the rule that separates an integer from a byte string: an
// integer is written without leading zeros, so zero is the empty string and
// not the byte 0x00.
var uintCases = []uintCase{
	{"nil", nil, "80"},
	{"zero", big.NewInt(0), "80"},
	{"one", big.NewInt(1), "01"},
	{"127", big.NewInt(127), "7f"},
	{"128", big.NewInt(128), "8180"},
	{"255", big.NewInt(255), "81ff"},
	{"256", big.NewInt(256), "820100"},
	{"one ether", new(big.Int).SetUint64(1000000000000000000), "880de0b6b3a7640000"},
}

func TestUint(t *testing.T) {
	for _, tc := range uintCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Uint(tc.value)
			if err != nil {
				t.Fatalf("Uint(%v): %v", tc.value, err)
			}
			if hex.EncodeToString(got) != tc.want {
				t.Errorf("Uint(%v) = %x, want %s", tc.value, got, tc.want)
			}
		})
	}
}

// TestUintRejectsNegative covers the one input that has no encoding.
func TestUintRejectsNegative(t *testing.T) {
	if _, err := Uint(big.NewInt(-1)); err == nil {
		t.Error("encoded a negative integer")
	}
}

// TestZeroByteIsNotZero pins the distinction the transaction encoder depends
// on: an empty recipient and a recipient of one zero byte are different, and
// so are a zero value and a one-byte string holding zero.
func TestZeroByteIsNotZero(t *testing.T) {
	empty := String(nil)
	zeroByte := String([]byte{0})
	if equal(empty, zeroByte) {
		t.Fatalf("the empty string and the zero byte encode alike: %x", empty)
	}
	if hex.EncodeToString(empty) != "80" {
		t.Errorf("empty string = %x, want 80", empty)
	}
	if hex.EncodeToString(zeroByte) != "00" {
		t.Errorf("zero byte = %x, want 00", zeroByte)
	}
}

// TestEmptyList pins the encoding of the empty access list every transaction
// here carries.
func TestEmptyList(t *testing.T) {
	if got := hex.EncodeToString(List()); got != "c0" {
		t.Errorf("List() = %s, want c0", got)
	}
}

// TestLongLengths covers the long forms, where the prefix holds the length of
// the length rather than the length.
func TestLongLengths(t *testing.T) {
	long := String(make([]byte, 1<<16))
	if got := hex.EncodeToString(long[:4]); got != "ba010000" {
		t.Errorf("65536-byte string prefix = %s, want ba010000", got)
	}
	if len(long) != 1<<16+4 {
		t.Errorf("65536-byte string encoded to %d bytes, want %d", len(long), 1<<16+4)
	}
}

func equal(a, b []byte) bool {
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

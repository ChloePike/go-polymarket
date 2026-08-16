// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"encoding/json"
	"os"
	"testing"
)

// orderBookHashVector is one entry of testdata/vectors.json's
// orderBookHashes array: a real order-book snapshot served by the CLOB
// REST /book endpoint, the server's own reported Hash, and an
// independently recomputed hash that was already confirmed to match it
// when the vector was generated.
type orderBookHashVector struct {
	TokenID    string `json:"tokenId"`
	Raw        string `json:"raw"`
	ServedHash string `json:"servedHash"`
	Hash       string `json:"hash"`
	Canonical  string `json:"canonical"`
}

// orderBookHashVectorFile is the subset of testdata/vectors.json this test
// reads; every other top-level key in that file belongs to other
// packages' tests and is ignored here.
type orderBookHashVectorFile struct {
	OrderBookHashes []orderBookHashVector `json:"orderBookHashes"`
}

func TestBookHashVectors(t *testing.T) {
	data, err := os.ReadFile("../testdata/vectors.json")
	if err != nil {
		t.Fatalf("read vectors.json: %v", err)
	}
	var file orderBookHashVectorFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal vectors.json: %v", err)
	}
	if len(file.OrderBookHashes) == 0 {
		t.Fatal("no orderBookHashes vectors found")
	}

	for _, v := range file.OrderBookHashes {
		t.Run(v.TokenID, func(t *testing.T) {
			var b Book
			if err := json.Unmarshal([]byte(v.Raw), &b); err != nil {
				t.Fatalf("unmarshal raw book: %v", err)
			}

			if b.Hash != v.ServedHash {
				t.Fatalf("Book.Hash after unmarshal = %s, want servedHash %s", b.Hash, v.ServedHash)
			}

			got := BookHash(b)
			if got != v.ServedHash {
				t.Errorf("BookHash = %s, want servedHash %s", got, v.ServedHash)
			}
			if got != v.Hash {
				t.Errorf("BookHash = %s, want hash %s", got, v.Hash)
			}

			// Pin the exact byte-for-byte serialization too, not just the
			// hash, so a change to field order or escaping that happened
			// to collide on SHA-1 would still be caught.
			gotJSON := string(canonicalBookJSON(b))
			if gotJSON != v.Canonical {
				t.Errorf("serialization mismatch:\n got: %s\nwant: %s", gotJSON, v.Canonical)
			}
		})
	}
}

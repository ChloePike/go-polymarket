// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
)

// Book is the order-book snapshot shape served by the CLOB REST /book
// endpoint, with fields declared in the exact order the server serializes
// them in: market, asset_id, timestamp, hash, bids, asks, min_order_size,
// tick_size, neg_risk, last_trade_price. Every field is a JSON string
// except NegRisk, a JSON bool.
//
// This is the shape BookHash's SHA-1 is computed over, confirmed against
// three real snapshots in testdata/vectors.json. It is a different shape
// than BookEvent, the one streamed over the market-channel websocket:
// BookEvent omits MinOrderSize and NegRisk and adds EventType, so a
// websocket BookEvent's Hash cannot be reproduced from the BookEvent
// alone. To verify a streamed snapshot's Hash with BookHash, fill in
// MinOrderSize and NegRisk from elsewhere (for example a REST /book call)
// before calling BookHash.
type Book struct {
	Market         string       `json:"market"`
	AssetID        string       `json:"asset_id"`
	Timestamp      string       `json:"timestamp"`
	Hash           string       `json:"hash"`
	Bids           []PriceLevel `json:"bids"`
	Asks           []PriceLevel `json:"asks"`
	MinOrderSize   string       `json:"min_order_size"`
	TickSize       string       `json:"tick_size"`
	NegRisk        bool         `json:"neg_risk"`
	LastTradePrice string       `json:"last_trade_price"`
}

// canonicalBookJSON returns b marshaled the way Polymarket's server does
// when it computes Hash: b.Hash forced to the empty string first, and
// with '<', '>', and '&' left unescaped -- matching JavaScript's
// JSON.stringify, which does not HTML-escape those bytes the way
// encoding/json does by default. Go struct field order matches Book's
// declared field order, which matches the server's own serialization
// order, so the JSON this produces is byte-for-byte what the server
// hashed.
func canonicalBookJSON(b Book) []byte {
	b.Hash = ""
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// Encode cannot fail for this concrete type: every field is a string,
	// a bool, or a slice of PriceLevel, itself two strings.
	_ = enc.Encode(b)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// BookHash computes the SHA-1 content hash Polymarket serves as Book.Hash,
// letting a client detect local order-book drift by recomputing it after
// applying price_change updates and comparing against the server's next
// reported hash. The result is a lowercase hex-encoded digest; it should
// equal b.Hash for a snapshot that has not drifted.
func BookHash(b Book) string {
	sum := sha1.Sum(canonicalBookJSON(b))
	return hex.EncodeToString(sum[:])
}

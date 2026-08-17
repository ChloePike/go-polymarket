// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package rlp encodes the recursive-length prefix form Ethereum serialises a
// transaction in.
//
// It encodes only. Nothing this client reads comes back as RLP: a node answers
// JSON, so the decoder that would pair with this has no call site.
//
// Like the abi and eip712 packages it is deliberately not general. There is no
// reflection over Go values and no struct tags; a caller builds a list from
// already-encoded items, which keeps the encoding rules visible at the one
// place that knows what the fields mean.
package rlp

import (
	"fmt"
	"math/big"
)

// Offsets from the specification. A single byte below 0x80 encodes as itself;
// everything else carries a prefix that says whether it is a string or a list,
// and whether its length is written directly or as a length of a length.
const (
	stringOffset     = 0x80
	stringLongOffset = 0xb7
	listOffset       = 0xc0
	listLongOffset   = 0xf7

	// shortLimit is the largest payload whose length fits in the prefix byte.
	shortLimit = 55
)

// String encodes a byte string.
//
// The empty string encodes as the single byte 0x80, which is how an absent
// field — no recipient, no calldata — is written. That is not the same as the
// zero byte 0x00, which is a one-byte string and encodes as itself.
func String(b []byte) []byte {
	if len(b) == 1 && b[0] < stringOffset {
		return []byte{b[0]}
	}
	return append(header(len(b), stringOffset, stringLongOffset), b...)
}

// List encodes a list whose items are already encoded.
func List(items ...[]byte) []byte {
	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	return append(header(len(payload), listOffset, listLongOffset), payload...)
}

// Uint encodes a non-negative integer as the shortest big-endian string with
// no leading zero bytes. Zero is the empty string, so a zero value and a zero
// nonce cost one byte each and are indistinguishable from an omitted field —
// which is what the specification intends.
func Uint(x *big.Int) ([]byte, error) {
	if x == nil {
		return String(nil), nil
	}
	if x.Sign() < 0 {
		return nil, fmt.Errorf("rlp: negative integer %s", x)
	}
	return String(x.Bytes()), nil
}

// Uint64 encodes a small non-negative integer, as Uint does.
func Uint64(v uint64) []byte {
	return String(new(big.Int).SetUint64(v).Bytes())
}

// header returns the prefix for a payload of n bytes, given the offsets for
// the short and long forms of its kind.
func header(n, short, long int) []byte {
	if n <= shortLimit {
		return []byte{byte(short + n)}
	}
	size := big.NewInt(int64(n)).Bytes()
	return append([]byte{byte(long + len(size))}, size...)
}

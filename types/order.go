// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike
//
// This file is part of go-polymarket.
// go-polymarket is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.

// Package types holds the wire and domain types for the Polymarket CLOB V2 API.
//
// All constants and struct field layouts here are transcribed from the public
// Polymarket protocol (as exposed by the official @polymarket/clob-client-v2
// SDK). They are protocol facts, not copied source. See DESIGN.md.
package types

// Side is the order direction. On the signed EIP-712 struct it is encoded as a
// uint8 (BUY=0, SELL=1); on the wire JSON it is carried as the string form.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Uint8 returns the on-chain/EIP-712 encoding of the side.
func (s Side) Uint8() uint8 {
	if s == SideSell {
		return 1
	}
	return 0
}

// SignatureType selects the on-chain signature verification path.
// V1 only for now (EOA). Others are documented for the roadmap.
type SignatureType uint8

const (
	SigEOA           SignatureType = 0 // ECDSA EIP-712 by an EOA
	SigPolyProxy     SignatureType = 1 // EOA that owns a Polymarket proxy wallet
	SigPolyGnosis    SignatureType = 2 // EOA that owns a Polymarket Gnosis safe
	SigPoly1271      SignatureType = 3 // EIP-1271 smart-contract wallet (deposit wallet)
)

// OrderType is the time-in-force / matching policy.
type OrderType string

const (
	OrderGTC OrderType = "GTC" // good-til-cancelled (resting limit order)
	OrderGTD OrderType = "GTD" // good-til-date
	OrderFOK OrderType = "FOK" // fill-or-kill (market)
	OrderFAK OrderType = "FAK" // fill-and-kill (market)
)

// Bytes32Zero is the default value for the metadata and builder fields.
const Bytes32Zero = "0x0000000000000000000000000000000000000000000000000000000000000000"

// UserOrder is the ergonomic input a caller provides. The client turns it into
// a signed Order (see order.Build).
type UserOrder struct {
	TokenID string // ERC1155 token id of the outcome being traded
	Price   string // decimal price in (0,1), e.g. "0.52"
	Size    string // number of shares
	Side    Side
	// BuilderCode is a bytes32 hex string. When set it is written into the
	// signed order's `builder` field for fee attribution. Empty => zero.
	BuilderCode string
	// Expiration is a unix-seconds string carried on the wire but NOT signed.
	// "0" means no expiration.
	Expiration string
}

// Order is the fully-resolved order prior to signing. The 11 fields that are
// actually part of the EIP-712 signed struct are marked below; `taker` and
// `expiration` travel on the wire but are excluded from the signature.
type Order struct {
	Salt          string        // uint256  [signed]
	Maker         string        // address  [signed]  funds owner
	Signer        string        // address  [signed]  == Maker for EOA
	Taker         string        // address  [wire-only] zero address = public
	TokenID       string        // uint256  [signed]
	MakerAmount   string        // uint256  [signed]  integer, 6-dp fixed point
	TakerAmount   string        // uint256  [signed]  integer, 6-dp fixed point
	Side          Side          // uint8    [signed]  BUY=0 SELL=1
	SignatureType SignatureType // uint8    [signed]
	Timestamp     string        // uint256  [signed]  Date.now() milliseconds
	Expiration    string        // uint256  [wire-only]
	Metadata      string        // bytes32  [signed]  default zero
	Builder       string        // bytes32  [signed]  builder code, default zero
}

// SignedOrder is an Order plus its 65-byte ECDSA signature (0x-prefixed hex).
type SignedOrder struct {
	Order
	Signature string
}

// APICreds are the L2 credentials returned by the auth endpoints.
type APICreds struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

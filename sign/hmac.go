// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package sign implements the three signing primitives the Polymarket CLOB V2
// API requires:
//
//   - L2 request HMAC (this file) — pure stdlib, fully implemented.
//   - L1 ClobAuth EIP-712 (clobauth.go) — used to create/derive API keys.
//   - Order EIP-712 (eip712.go) — the core order signature.
//
// The two EIP-712 pieces need secp256k1 + keccak256; they are stubbed and
// wired to go-ethereum in M1 (see DESIGN.md).
package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// BuildHMAC produces the canonical Polymarket CLOB L2 request signature.
//
// message   = timestamp + method + requestPath [+ body]
// key       = base64url-decode(secret)
// signature = base64url(HMAC-SHA256(key, message))
//
// requestPath must include any query string. body is the exact marshalled
// request body (omit for GET). timestamp is unix seconds as a string.
func BuildHMAC(secret, timestamp, method, requestPath, body string) (string, error) {
	key, err := decodeBase64URL(secret)
	if err != nil {
		return "", err
	}
	msg := timestamp + method + requestPath + body
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeBase64URL decodes a base64url secret, tolerating missing padding and
// the standard-alphabet variant (the SDK normalizes -/_ before decoding).
func decodeBase64URL(s string) ([]byte, error) {
	if b, err := base64.URLEncoding.DecodeString(pad(s)); err == nil {
		return b, nil
	}
	// Fall back to raw (unpadded) URL encoding.
	return base64.RawURLEncoding.DecodeString(trimPad(s))
}

func pad(s string) string {
	if m := len(s) % 4; m != 0 {
		return s + "===="[m:]
	}
	return s
}

func trimPad(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

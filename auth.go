// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// The CLOB has two levels of authentication.
//
// Level 1 proves control of the wallet with an EIP-712 signature, and is used
// only to create or derive an API key. Level 2 authenticates each ordinary
// request with an HMAC over the request line, using the credentials level 1
// handed out. Trading uses level 2; the private key is not involved once the
// credentials exist.

// APICreds are the level-2 credentials the auth endpoints return. Treat all
// three as secrets: together they can trade on the account.
type APICreds struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// ClobAuthDigest returns the level-1 EIP-712 digest for an address at a
// timestamp and nonce.
//
// Two of the four fields are strings, which EIP-712 hashes rather than pads:
// timestamp is a string even though it holds a number, while nonce is a real
// uint256. Encoding timestamp as a number produces a well-formed signature
// that authenticates as nobody.
func ClobAuthDigest(address string, chainID int64, timestamp string, nonce int64) ([32]byte, error) {
	domain := eip712.Domain{
		Name:    clobAuthDomainName,
		Version: clobAuthDomainVersion,
		ChainID: big.NewInt(chainID),
		// No verifying contract: the field leaves the type string entirely.
	}
	separator, err := domain.Separator()
	if err != nil {
		return [32]byte{}, err
	}
	var enc eip712.Encoder
	enc.Address("address", address)
	enc.String("timestamp", timestamp)
	enc.Uint256("nonce", big.NewInt(nonce))
	enc.String("message", clobAuthMessage)

	structHash, err := enc.StructHash(eip712.TypeHash(clobAuthTypeString))
	if err != nil {
		return [32]byte{}, fmt.Errorf("polymarket: clob auth: %w", err)
	}
	return eip712.Digest(separator, structHash), nil
}

// L1Headers are the headers that authenticate a key-management request.
type L1Headers struct {
	Address   string
	Signature string
	Timestamp string // unix seconds
	Nonce     string
}

func (h L1Headers) header() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":   h.Address,
		"POLY_SIGNATURE": h.Signature,
		"POLY_TIMESTAMP": h.Timestamp,
		"POLY_NONCE":     h.Nonce,
	}
}

// BuildL1Headers signs the level-1 payload for a wallet.
func BuildL1Headers(s Signer, chainID int64, timestamp string, nonce int64) (L1Headers, error) {
	if s == nil {
		return L1Headers{}, fmt.Errorf("polymarket: level-1 authentication needs a Signer")
	}
	digest, err := ClobAuthDigest(s.Address(), chainID, timestamp, nonce)
	if err != nil {
		return L1Headers{}, err
	}
	sig, err := s.SignDigest(digest)
	if err != nil {
		return L1Headers{}, fmt.Errorf("polymarket: signing level-1 payload: %w", err)
	}
	return L1Headers{
		Address:   s.Address(),
		Signature: "0x" + hex.EncodeToString(sig),
		Timestamp: timestamp,
		Nonce:     strconv.FormatInt(nonce, 10),
	}, nil
}

// L2Headers authenticate one ordinary request.
type L2Headers struct {
	Address    string
	Signature  string
	APIKey     string
	Passphrase string
	Timestamp  string // unix seconds
}

func (h L2Headers) header() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":    h.Address,
		"POLY_SIGNATURE":  h.Signature,
		"POLY_API_KEY":    h.APIKey,
		"POLY_PASSPHRASE": h.Passphrase,
		"POLY_TIMESTAMP":  h.Timestamp,
	}
}

// BuildL2Headers signs one request with level-2 credentials.
//
// requestPath is the path ALONE, without a query string. The exchange
// excludes the query from the signature, which is what lets one set of
// headers authenticate every page of a paginated walk. Including it produces
// a 401 as soon as any parameter is present.
func BuildL2Headers(creds APICreds, address, timestamp, method, requestPath, body string) (L2Headers, error) {
	sig, err := SignHMAC(creds.Secret, timestamp, method, requestPath, body)
	if err != nil {
		return L2Headers{}, err
	}
	return L2Headers{
		Address:    address,
		Signature:  sig,
		APIKey:     creds.Key,
		Passphrase: creds.Passphrase,
		Timestamp:  timestamp,
	}, nil
}

// SignHMAC produces the level-2 request signature:
//
//	message   = timestamp ‖ method ‖ requestPath ‖ body
//
// requestPath is the bare path; the exchange does not sign the query string.
//
//	key       = base64url-decode(secret)
//	signature = base64url(HMAC-SHA256(key, message))
//
// The secret arrives base64url-encoded and is decoded before use; signing with
// the encoded text produces a signature the server rejects.
func SignHMAC(secret, timestamp, method, requestPath, body string) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", fmt.Errorf("polymarket: api secret: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeSecret decodes a base64url secret, tolerating missing padding and the
// standard alphabet, which different parts of the API have both been seen to
// emit.
func decodeSecret(s string) ([]byte, error) {
	if b, err := base64.URLEncoding.DecodeString(padBase64(s)); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(padBase64(s))
}

func padBase64(s string) string {
	if m := len(s) % 4; m != 0 {
		return s + "===="[m:]
	}
	return s
}

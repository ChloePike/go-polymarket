// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package sign

import "github.com/ChloePike/go-polymarket/types"

// L1Headers are the headers required to create or derive an API key.
type L1Headers struct {
	Address   string
	Signature string
	Timestamp string // unix seconds
	Nonce     string
}

// Map renders the headers with their canonical POLY_ names.
func (h L1Headers) Map() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":   h.Address,
		"POLY_SIGNATURE": h.Signature,
		"POLY_TIMESTAMP": h.Timestamp,
		"POLY_NONCE":     h.Nonce,
	}
}

// L2Headers authenticate a single private request.
type L2Headers struct {
	Address    string
	Signature  string // BuildHMAC output
	APIKey     string
	Passphrase string
	Timestamp  string
}

// Map renders the L2 headers.
func (h L2Headers) Map() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":    h.Address,
		"POLY_SIGNATURE":  h.Signature,
		"POLY_API_KEY":    h.APIKey,
		"POLY_PASSPHRASE": h.Passphrase,
		"POLY_TIMESTAMP":  h.Timestamp,
	}
}

// BuildL2Headers signs a request with the L2 credentials.
func BuildL2Headers(creds types.APICreds, address, timestamp, method, requestPath, body string) (L2Headers, error) {
	sig, err := BuildHMAC(creds.Secret, timestamp, method, requestPath, body)
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

// BuildClobAuthSignature signs the L1 ClobAuth EIP-712 payload.
//
// EIP-712:
//
//	domain = {name:"ClobAuthDomain", version:"1", chainId}   (no verifyingContract)
//	type   = ClobAuth(address address,string timestamp,uint256 nonce,string message)
//	value  = {address, timestamp(string), nonce(uint256),
//	          message:"This message attests that I control the given wallet"}
//
// TODO(M1): implement alongside OrderHash; the digest construction is the same
// shape (a domain with no verifyingContract). Golden-test against the SDK.
func BuildClobAuthSignature(signer Signer, chainID int64, timestamp string, nonce int64) (string, error) {
	return "", ErrNotImplemented
}

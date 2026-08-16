// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package eip712

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// vectors mirrors the subset of testdata/vectors.json this package pins. The
// file is generated from the official TypeScript SDK; see testdata/gen-vectors.mjs.
type vectors struct {
	TypeHashes struct {
		Order  string `json:"order"`
		Domain string `json:"domain"`
	} `json:"typeHashes"`
	Accounts []struct {
		PrivateKey string `json:"privateKey"`
		Address    string `json:"address"`
	} `json:"accounts"`
	Orders []struct {
		Name   string `json:"name"`
		Domain struct {
			Name              string `json:"name"`
			Version           string `json:"version"`
			ChainID           int64  `json:"chainId"`
			VerifyingContract string `json:"verifyingContract"`
		} `json:"domain"`
		Order struct {
			Salt          string `json:"salt"`
			Maker         string `json:"maker"`
			Signer        string `json:"signer"`
			TokenID       string `json:"tokenId"`
			MakerAmount   string `json:"makerAmount"`
			TakerAmount   string `json:"takerAmount"`
			Side          string `json:"side"`
			SignatureType uint8  `json:"signatureType"`
			Timestamp     string `json:"timestamp"`
			Metadata      string `json:"metadata"`
			Builder       string `json:"builder"`
		} `json:"order"`
		Digest    string `json:"digest"`
		Signature string `json:"signature"`
	} `json:"orders"`
	ClobAuth []struct {
		ChainID   int64  `json:"chainId"`
		Address   string `json:"address"`
		Timestamp string `json:"timestamp"`
		Nonce     int64  `json:"nonce"`
		Message   string `json:"message"`
		Digest    string `json:"digest"`
		Signature string `json:"signature"`
	} `json:"clobAuth"`
}

func load(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("../../testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return v
}

const orderTypeString = "Order(uint256 salt,address maker,address signer," +
	"uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side," +
	"uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

const clobAuthTypeString = "ClobAuth(address address,string timestamp,uint256 nonce,string message)"

func TestTypeHashes(t *testing.T) {
	v := load(t)
	if got := TypeHash(orderTypeString).Hex(); got != v.TypeHashes.Order {
		t.Errorf("order type hash = %s, want %s", got, v.TypeHashes.Order)
	}
	domainType := "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
	if got := TypeHash(domainType).Hex(); got != v.TypeHashes.Domain {
		t.Errorf("domain type hash = %s, want %s", got, v.TypeHashes.Domain)
	}
}

func TestOrderDigest(t *testing.T) {
	v := load(t)
	for _, tc := range v.Orders {
		t.Run(tc.Name, func(t *testing.T) {
			d := Domain{
				Name:              tc.Domain.Name,
				Version:           tc.Domain.Version,
				ChainID:           big.NewInt(tc.Domain.ChainID),
				VerifyingContract: tc.Domain.VerifyingContract,
			}
			sep, err := d.Separator()
			if err != nil {
				t.Fatal(err)
			}

			o := tc.Order
			var side uint8
			if o.Side == "SELL" {
				side = 1
			}
			words := make([]Word, 0, 11)
			for _, f := range []struct {
				name string
				fn   func() (Word, error)
			}{
				{"salt", func() (Word, error) { return UintString(o.Salt) }},
				{"maker", func() (Word, error) { return Address(o.Maker) }},
				{"signer", func() (Word, error) { return Address(o.Signer) }},
				{"tokenId", func() (Word, error) { return UintString(o.TokenID) }},
				{"makerAmount", func() (Word, error) { return UintString(o.MakerAmount) }},
				{"takerAmount", func() (Word, error) { return UintString(o.TakerAmount) }},
				{"side", func() (Word, error) { return Uint8(side), nil }},
				{"signatureType", func() (Word, error) { return Uint8(o.SignatureType), nil }},
				{"timestamp", func() (Word, error) { return UintString(o.Timestamp) }},
				{"metadata", func() (Word, error) { return Bytes32(o.Metadata) }},
				{"builder", func() (Word, error) { return Bytes32(o.Builder) }},
			} {
				w, err := f.fn()
				if err != nil {
					t.Fatalf("%s: %v", f.name, err)
				}
				words = append(words, w)
			}

			sh := StructHash(TypeHash(orderTypeString), words...)
			digest := Digest(sep, sh)
			if got := Word(digest).Hex(); got != tc.Digest {
				t.Fatalf("digest = %s, want %s", got, tc.Digest)
			}
		})
	}
}

func TestClobAuthDigest(t *testing.T) {
	v := load(t)
	for _, tc := range v.ClobAuth {
		t.Run(tc.Timestamp, func(t *testing.T) {
			// The ClobAuth domain carries no verifyingContract, so the field
			// leaves the type string entirely.
			d := Domain{Name: "ClobAuthDomain", Version: "1", ChainID: big.NewInt(tc.ChainID)}
			sep, err := d.Separator()
			if err != nil {
				t.Fatal(err)
			}
			addr, err := Address(tc.Address)
			if err != nil {
				t.Fatal(err)
			}
			nonce, err := Uint(big.NewInt(tc.Nonce))
			if err != nil {
				t.Fatal(err)
			}
			// timestamp and message are string fields: hashed, not padded.
			sh := StructHash(TypeHash(clobAuthTypeString),
				addr, String(tc.Timestamp), nonce, String(tc.Message))
			if got := Word(Digest(sep, sh)).Hex(); got != tc.Digest {
				t.Fatalf("digest = %s, want %s", got, tc.Digest)
			}
		})
	}
}

// TestSignatureBytes pins the signature encoding: decred returns compact
// V‖R‖S, while Ethereum expects R‖S‖V with V in {27,28}.
func TestSignatureBytes(t *testing.T) {
	v := load(t)
	priv := v.Accounts[0].PrivateKey
	kb, err := hex.DecodeString(priv[2:])
	if err != nil {
		t.Fatal(err)
	}
	key := secp256k1.PrivKeyFromBytes(kb)

	for _, tc := range v.Orders {
		t.Run(tc.Name, func(t *testing.T) {
			digest, err := hex.DecodeString(tc.Digest[2:])
			if err != nil {
				t.Fatal(err)
			}
			compact := ecdsa.SignCompact(key, digest, false)
			if len(compact) != 65 {
				t.Fatalf("compact signature is %d bytes, want 65", len(compact))
			}
			sig := make([]byte, 65)
			copy(sig, compact[1:])
			sig[64] = compact[0]
			if got := "0x" + hex.EncodeToString(sig); got != tc.Signature {
				t.Fatalf("signature = %s, want %s", got, tc.Signature)
			}
		})
	}
}

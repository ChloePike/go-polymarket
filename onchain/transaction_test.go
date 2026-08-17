// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// The vectors in testdata/tx-vectors.json come from a general Ethereum client,
// because a transaction's encoding is Ethereum's and not Polymarket's. They
// exist for the same reason the other vector files do: a transaction with a
// mis-encoded field is not rejected with an explanation, it is either dropped
// by the node as an invalid sender or mined as something the caller did not
// intend, and it is paid for either way.

// txVectors is the whole vector file, as this package reads it.
type txVectors struct {
	PrivateKey   string           `json:"privateKey"`
	Address      string           `json:"address"`
	Transactions []txVectorCase   `json:"transactions"`
	Calls        []callVectorCase `json:"calls"`
	Positions    []callVectorCase `json:"positions"`
}

// txVectorCase is one transaction, and everything signing it must produce.
type txVectorCase struct {
	Name                 string `json:"name"`
	ChainID              int64  `json:"chainId"`
	Nonce                uint64 `json:"nonce"`
	To                   string `json:"to"`
	Value                string `json:"value"`
	Data                 string `json:"data"`
	Gas                  uint64 `json:"gas"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`

	Unsigned    string `json:"unsigned"`
	SigningHash string `json:"signingHash"`
	Raw         string `json:"raw"`
	Hash        string `json:"hash"`
	YParity     int    `json:"yParity"`
	R           string `json:"r"`
	S           string `json:"s"`
}

// callVectorCase is one piece of calldata and the function it encodes.
type callVectorCase struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Data      string `json:"data"`
}

func loadTxVectors(t *testing.T) txVectors {
	t.Helper()
	b, err := os.ReadFile("../testdata/tx-vectors.json")
	if err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	var v txVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	if len(v.Transactions) == 0 {
		t.Fatal("tx vectors: no transactions")
	}
	return v
}

// transaction turns a vector into the transaction this package signs.
func (c txVectorCase) transaction(t *testing.T) Transaction {
	t.Helper()
	value, ok := new(big.Int).SetString(c.Value, 10)
	if !ok {
		t.Fatalf("vector value %q is not a number", c.Value)
	}
	maxFee, ok := new(big.Int).SetString(c.MaxFeePerGas, 10)
	if !ok {
		t.Fatalf("vector max fee %q is not a number", c.MaxFeePerGas)
	}
	tip, ok := new(big.Int).SetString(c.MaxPriorityFeePerGas, 10)
	if !ok {
		t.Fatalf("vector priority fee %q is not a number", c.MaxPriorityFeePerGas)
	}
	return Transaction{
		ChainID:              big.NewInt(c.ChainID),
		Nonce:                c.Nonce,
		MaxPriorityFeePerGas: tip,
		MaxFeePerGas:         maxFee,
		Gas:                  c.Gas,
		To:                   c.To,
		Value:                value,
		Data:                 mustHex(t, c.Data),
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatalf("vector %q is not hex: %v", s, err)
	}
	return b
}

// TestTransactionsMatchTheReferenceClient is the golden gate on this package's
// signing: encoding, digest, signature and hash, over both chains, with and
// without calldata, with and without a recipient, at both parities.
func TestTransactionsMatchTheReferenceClient(t *testing.T) {
	v := loadTxVectors(t)
	key, err := polymarket.NewPrivateKey(v.PrivateKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	if !strings.EqualFold(key.Address(), v.Address) {
		t.Fatalf("key derives %s, vectors were signed by %s", key.Address(), v.Address)
	}

	for _, tc := range v.Transactions {
		t.Run(tc.Name, func(t *testing.T) {
			tx := tc.transaction(t)

			unsigned, err := tx.Unsigned()
			if err != nil {
				t.Fatalf("Unsigned: %v", err)
			}
			if got := hexData(unsigned); !strings.EqualFold(got, tc.Unsigned) {
				t.Errorf("unsigned = %s, want %s", got, tc.Unsigned)
			}

			digest, err := tx.SigningHash()
			if err != nil {
				t.Fatalf("SigningHash: %v", err)
			}
			if got := hexData(digest[:]); !strings.EqualFold(got, tc.SigningHash) {
				t.Errorf("signing hash = %s, want %s", got, tc.SigningHash)
			}

			signed, err := SignTransaction(key, tx)
			if err != nil {
				t.Fatalf("SignTransaction: %v", err)
			}
			if got := signed.RawHex(); !strings.EqualFold(got, tc.Raw) {
				t.Errorf("raw = %s, want %s", got, tc.Raw)
			}
			if !strings.EqualFold(signed.Hash, tc.Hash) {
				t.Errorf("hash = %s, want %s", signed.Hash, tc.Hash)
			}
			if !strings.EqualFold(signed.From, v.Address) {
				t.Errorf("from = %s, want %s", signed.From, v.Address)
			}
		})
	}
}

// TestBothParitiesAreCovered guards the vector file itself: a set of vectors
// that happened to sign to one parity would not exercise the conversion from
// v to yParity at all.
func TestBothParitiesAreCovered(t *testing.T) {
	v := loadTxVectors(t)
	seen := make(map[int]bool)
	for _, tc := range v.Transactions {
		seen[tc.YParity] = true
	}
	if !seen[0] || !seen[1] {
		t.Errorf("vectors cover parities %v, want both 0 and 1", seen)
	}
}

// TestParityIsNotV pins the trap this package's signing turns on. Every other
// signature in this module carries v as 27 or 28, because the contracts that
// verify them expect it. A typed transaction stores the parity bit alone, so
// the encoded value must be 0 or 1 — and a raw transaction ending in a 0x1b
// is the failure this test exists to catch.
func TestParityIsNotV(t *testing.T) {
	v := loadTxVectors(t)
	key, err := polymarket.NewPrivateKey(v.PrivateKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	for _, tc := range v.Transactions {
		signed, err := SignTransaction(key, tc.transaction(t))
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		// The parity is the first item after the nine signed fields. Rather
		// than re-walking the encoding, take it from the vector's own r: the
		// item before r in the encoding is the parity, and RLP writes 0 as
		// 0x80 and 1 as 0x01.
		want := byte(0x80)
		if tc.YParity == 1 {
			want = 0x01
		}
		r := mustHex(t, tc.R)
		at := indexOf(signed.Raw, r)
		if at <= 0 {
			t.Fatalf("%s: r not found in the encoding", tc.Name)
		}
		// r is preceded by its own length byte, and the parity by that.
		if got := signed.Raw[at-2]; got != want {
			t.Errorf("%s: parity byte = %#x, want %#x", tc.Name, got, want)
		}
	}
}

// A constantSigner is a Signer that returns a fixed signature. It exists to
// feed SignTransaction the signatures a real key never produces.
type constantSigner struct {
	address string
	sig     []byte
}

func (s constantSigner) Address() string { return s.address }

func (s constantSigner) SignDigest(digest [32]byte) ([]byte, error) { return s.sig, nil }

// TestSignRejectsAMalformedSignature covers the checks between a Signer and
// the wire: a signature that does not recover to the signing address, and one
// whose v is neither 27 nor 28.
func TestSignRejectsAMalformedSignature(t *testing.T) {
	v := loadTxVectors(t)
	tx := v.Transactions[0].transaction(t)

	bad := make([]byte, 65)
	bad[64] = 27
	if _, err := SignTransaction(constantSigner{address: v.Address, sig: bad}, tx); err == nil {
		t.Error("signed with a signature that recovers to nothing")
	}

	key, err := polymarket.NewPrivateKey(v.PrivateKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	digest, err := tx.SigningHash()
	if err != nil {
		t.Fatalf("SigningHash: %v", err)
	}
	sig, err := key.SignDigest(digest)
	if err != nil {
		t.Fatalf("SignDigest: %v", err)
	}
	raw := append([]byte(nil), sig...)
	raw[64] = 0 // already reduced to a parity bit, which is not what a Signer returns
	if _, err := SignTransaction(constantSigner{address: v.Address, sig: raw}, tx); err == nil {
		t.Error("accepted a signature whose v was already a parity bit")
	}
}

// TestSignRejectsAnIncompleteTransaction covers the fields a node fills in.
// Signing without them produces a transaction that is rejected after it has
// been broadcast, which is the expensive place to find out.
func TestSignRejectsAnIncompleteTransaction(t *testing.T) {
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	full := Transaction{
		ChainID:              big.NewInt(polymarket.ChainPolygon),
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(2),
		Gas:                  21000,
		To:                   "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
	}

	noChain := full
	noChain.ChainID = nil
	if _, err := SignTransaction(key, noChain); err == nil {
		t.Error("signed without a chain id")
	}

	noFees := full
	noFees.MaxFeePerGas = nil
	if _, err := SignTransaction(key, noFees); err == nil {
		t.Error("signed without a max fee")
	}

	noGas := full
	noGas.Gas = 0
	if _, err := SignTransaction(key, noGas); err == nil {
		t.Error("signed without a gas limit")
	}

	inverted := full
	inverted.MaxPriorityFeePerGas = big.NewInt(3)
	if _, err := SignTransaction(key, inverted); err == nil {
		t.Error("signed with a priority fee above the max fee")
	}

	badTo := full
	badTo.To = "0x1234"
	if _, err := SignTransaction(key, badTo); err == nil {
		t.Error("signed with a malformed recipient")
	}

	if _, err := SignTransaction(nil, full); err == nil {
		t.Error("signed without a signer")
	}
}

// TestContractCreationHasNoRecipient pins that an empty To encodes as the
// empty string rather than the zero address, which is a real account that
// value sent to is lost in.
func TestContractCreationHasNoRecipient(t *testing.T) {
	v := loadTxVectors(t)
	var creation *txVectorCase
	for i := range v.Transactions {
		if v.Transactions[i].To == "" {
			creation = &v.Transactions[i]
			break
		}
	}
	if creation == nil {
		t.Skip("no contract-creation vector")
	}
	unsigned, err := creation.transaction(t).Unsigned()
	if err != nil {
		t.Fatalf("Unsigned: %v", err)
	}
	zeroAddress := make([]byte, 20)
	if indexOf(unsigned, zeroAddress) >= 0 {
		t.Error("contract creation encoded a zero address recipient")
	}
}

// hardhatKey is the well-known Hardhat development account. It is public and
// holds nothing.
const hardhatKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// indexOf returns where needle starts in haystack, or -1.
func indexOf(haystack, needle []byte) int {
	return strings.Index(string(haystack), string(needle))
}

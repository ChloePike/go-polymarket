// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package eip712 implements the slice of EIP-712 typed-data hashing that the
// Polymarket API needs: a domain separator, a struct hash over a fixed list of
// already-encoded fields, and the final signing digest.
//
// It is deliberately not a general typed-data library. Polymarket signs two
// structs, both with a known field list, so callers encode each field with the
// helpers here and pass the words in order. That keeps the encoding rules
// visible at the call site instead of hidden behind a schema walker.
package eip712

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// A Word is one ABI-encoded 32-byte EIP-712 field.
type Word [32]byte

// Keccak256 returns the Keccak-256 hash of the concatenated inputs.
//
// This is the original Keccak padding, not the FIPS-202 SHA-3 that the
// standard library's crypto/sha3 provides. The two differ and are not
// interchangeable: Ethereum uses Keccak.
func Keccak256(parts ...[]byte) Word {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	var w Word
	h.Sum(w[:0])
	return w
}

// TypeHash returns keccak256 of an EIP-712 type string such as
// "Order(uint256 salt,address maker,...)".
func TypeHash(typeString string) Word {
	return Keccak256([]byte(typeString))
}

// Uint encodes a uint256/uint128/.../uint8 field: big-endian, left-padded.
// Negative values are rejected; every Polymarket numeric field is unsigned.
func Uint(x *big.Int) (Word, error) {
	var w Word
	if x.Sign() < 0 {
		return w, fmt.Errorf("eip712: negative uint %s", x)
	}
	b := x.Bytes()
	if len(b) > 32 {
		return w, fmt.Errorf("eip712: uint overflows 32 bytes: %s", x)
	}
	copy(w[32-len(b):], b)
	return w, nil
}

// UintString encodes a decimal numeric string, the form every Polymarket
// amount, salt and timestamp arrives in.
func UintString(s string) (Word, error) {
	x, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return Word{}, fmt.Errorf("eip712: invalid decimal integer %q", s)
	}
	return Uint(x)
}

// Uint8 encodes a small unsigned field such as side or signatureType.
func Uint8(v uint8) Word {
	var w Word
	w[31] = v
	return w
}

// Address encodes a 20-byte address, left-padded to 32 bytes. The input is
// case-insensitive; no checksum is enforced here because the exchange does not
// enforce one either.
func Address(s string) (Word, error) {
	b, err := decodeHex(s, 20)
	if err != nil {
		return Word{}, fmt.Errorf("eip712: bad address %q: %w", s, err)
	}
	var w Word
	copy(w[12:], b)
	return w, nil
}

// Bytes32 encodes a bytes32 field verbatim. A bytes32 is an atomic type: it is
// carried through as-is and must never be hashed, unlike bytes or string.
func Bytes32(s string) (Word, error) {
	b, err := decodeHex(s, 32)
	if err != nil {
		return Word{}, fmt.Errorf("eip712: bad bytes32 %q: %w", s, err)
	}
	var w Word
	copy(w[:], b)
	return w, nil
}

// String encodes a string field, which EIP-712 defines as a dynamic type: the
// encoded value is the keccak256 of its UTF-8 bytes.
func String(s string) Word {
	return Keccak256([]byte(s))
}

// StructHash returns keccak256(typeHash ‖ field₀ ‖ … ‖ fieldₙ), the hashStruct
// of EIP-712. Fields must be supplied in the order the type string declares.
func StructHash(typeHash Word, fields ...Word) Word {
	buf := make([]byte, 0, 32*(1+len(fields)))
	buf = append(buf, typeHash[:]...)
	for _, f := range fields {
		buf = append(buf, f[:]...)
	}
	return Keccak256(buf)
}

// An Encoder accumulates a struct's fields in the order its type string
// declares them, holding the first failure until the caller asks for the hash.
// It exists so a field list reads as a field list instead of as eleven
// repetitions of the same error check.
//
// The zero Encoder is ready to use.
type Encoder struct {
	words []Word
	err   error
}

// Uint appends a uint256 field given as a decimal string.
func (e *Encoder) Uint(name, decimal string) {
	w, err := UintString(decimal)
	e.append(name, w, err)
}

// Uint8 appends a uint8 field.
func (e *Encoder) Uint8(name string, v uint8) {
	e.append(name, Uint8(v), nil)
}

// Uint256 appends a uint256 field given as an integer.
func (e *Encoder) Uint256(name string, v *big.Int) {
	w, err := Uint(v)
	e.append(name, w, err)
}

// Address appends an address field.
func (e *Encoder) Address(name, address string) {
	w, err := Address(address)
	e.append(name, w, err)
}

// Bytes32 appends a bytes32 field, which is carried verbatim.
func (e *Encoder) Bytes32(name, value string) {
	w, err := Bytes32(value)
	e.append(name, w, err)
}

// String appends a string field, which EIP-712 hashes rather than pads.
func (e *Encoder) String(name, value string) {
	e.append(name, String(value), nil)
}

func (e *Encoder) append(name string, w Word, err error) {
	if err != nil {
		if e.err == nil {
			e.err = fmt.Errorf("field %s: %w", name, err)
		}
		return
	}
	e.words = append(e.words, w)
}

// StructHash returns hashStruct over the accumulated fields, or the first
// error a field encoding produced.
func (e *Encoder) StructHash(typeHash Word) (Word, error) {
	if e.err != nil {
		return Word{}, e.err
	}
	return StructHash(typeHash, e.words...), nil
}

// A Domain is an EIP-712 domain. An empty VerifyingContract means the domain
// omits that field entirely — the field is dropped from the type string, not
// encoded as the zero address. Polymarket's ClobAuth domain relies on this.
type Domain struct {
	Name              string
	Version           string
	ChainID           *big.Int
	VerifyingContract string
}

// Separator returns the domain separator, hashStruct(EIP712Domain).
func (d Domain) Separator() (Word, error) {
	chainID, err := Uint(d.ChainID)
	if err != nil {
		return Word{}, err
	}
	fields := []Word{String(d.Name), String(d.Version), chainID}

	typeString := "EIP712Domain(string name,string version,uint256 chainId"
	if d.VerifyingContract != "" {
		typeString += ",address verifyingContract"
		addr, err := Address(d.VerifyingContract)
		if err != nil {
			return Word{}, err
		}
		fields = append(fields, addr)
	}
	typeString += ")"

	return StructHash(TypeHash(typeString), fields...), nil
}

// Digest returns the 32 bytes actually signed:
//
//	keccak256(0x19 ‖ 0x01 ‖ domainSeparator ‖ structHash)
func Digest(domainSeparator, structHash Word) [32]byte {
	return Keccak256([]byte{0x19, 0x01}, domainSeparator[:], structHash[:])
}

// Hex renders a word as a 0x-prefixed lowercase hex string.
func (w Word) Hex() string {
	return "0x" + hex.EncodeToString(w[:])
}

// decodeHex decodes a 0x-prefixed hex string and checks its byte length.
func decodeHex(s string, want int) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	if err != nil {
		return nil, err
	}
	if len(b) != want {
		return nil, fmt.Errorf("got %d bytes, want %d", len(b), want)
	}
	return b, nil
}

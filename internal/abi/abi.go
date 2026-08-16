// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package abi implements the slice of Solidity ABI encoding this client needs:
// CREATE2 arguments, the calldata for a handful of known functions, and the
// packed encoding a few hashes are defined over.
//
// Like the eip712 package it is deliberately not general. Every call site here
// knows its own signature, so there is no ABI JSON to parse and no runtime
// type dispatch — the encoding rules stay visible where they are used.
package abi

import (
	"fmt"
	"math/big"

	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// Word is one 32-byte ABI slot, the same shape EIP-712 encodes fields into.
// The two encodings agree on every static type, which is why the helpers in
// the eip712 package produce arguments usable here.
type Word = eip712.Word

// Encode concatenates already-encoded words. That is the whole of abi.encode
// for static types, which is all the fixed-shape arguments here are.
func Encode(words ...Word) []byte {
	out := make([]byte, 0, 32*len(words))
	for _, w := range words {
		out = append(out, w[:]...)
	}
	return out
}

// Selector returns the four bytes that begin calldata for a function: the
// leading bytes of keccak256 over its canonical signature, such as
// "approve(address,uint256)". The signature carries no argument names and no
// spaces; either changes the hash and calls a function that does not exist.
func Selector(signature string) []byte {
	h := eip712.Keccak256([]byte(signature))
	return h[:4]
}

// EncodeCall builds calldata for a function whose arguments are all static:
// the selector followed by one 32-byte word each.
func EncodeCall(signature string, args ...Word) []byte {
	return append(Selector(signature), Encode(args...)...)
}

// EncodeBytesCall builds calldata for a function taking a single dynamic
// bytes argument, such as multiSend(bytes).
//
// A dynamic argument is passed by offset: the head holds where the value
// starts, and the tail holds its length and then its bytes padded up to a
// multiple of 32. With one argument the offset is always 32.
func EncodeBytesCall(signature string, payload []byte) []byte {
	offset, length := Uint64(32), Uint64(uint64(len(payload)))
	out := Selector(signature)
	out = append(out, offset[:]...)
	out = append(out, length[:]...)
	out = append(out, payload...)
	if pad := len(payload) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return out
}

// Uint64 encodes a small unsigned integer as one word.
func Uint64(v uint64) Word {
	var w Word
	for i := 0; i < 8; i++ {
		w[31-i] = byte(v >> (8 * i))
	}
	return w
}

// PackedUint encodes x big-endian into exactly size bytes, the encodePacked
// form. Unlike the padded encoding this is how a uint8 becomes one byte and a
// uint256 becomes thirty-two.
func PackedUint(x *big.Int, size int) ([]byte, error) {
	if x.Sign() < 0 {
		return nil, fmt.Errorf("abi: negative uint %s", x)
	}
	b := x.Bytes()
	if len(b) > size {
		return nil, fmt.Errorf("abi: %s does not fit in %d bytes", x, size)
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out, nil
}

// PackedAddress encodes an address as its twenty raw bytes, with no padding.
// The distinction matters: the Polymarket proxy factory salts CREATE2 with
// keccak256 over these twenty bytes, while the Safe factory salts it with
// keccak256 over the address padded to a full word. Same address, different
// wallet.
func PackedAddress(address string) ([]byte, error) {
	w, err := eip712.Address(address)
	if err != nil {
		return nil, err
	}
	return w[12:], nil
}

// Create2 returns the address a contract deploys to under CREATE2:
//
//	keccak256(0xff ‖ deployer ‖ salt ‖ initCodeHash)[12:]
//
// The result is lowercase and unchecksummed; callers that show it to a user
// should run it through EIP-55 first.
func Create2(deployer string, salt, initCodeHash Word) (string, error) {
	address, err := PackedAddress(deployer)
	if err != nil {
		return "", fmt.Errorf("abi: create2 deployer: %w", err)
	}
	h := eip712.Keccak256([]byte{0xff}, address, salt[:], initCodeHash[:])
	return "0x" + hexLower(h[12:]), nil
}

const hexDigits = "0123456789abcdef"

func hexLower(b []byte) string {
	out := make([]byte, 2*len(b))
	for i, c := range b {
		out[2*i] = hexDigits[c>>4]
		out[2*i+1] = hexDigits[c&0x0f]
	}
	return string(out)
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package abi

import (
	"fmt"
	"math/big"
)

// An Arg is one argument of a call, encoded.
//
// The ABI splits an argument list into a head and a tail. A static argument
// writes its value into the head; a dynamic one — an array, a byte string, or
// a tuple containing either — writes a byte offset into the head and its value
// into the tail. Arg carries both halves so a caller can assemble a list
// without tracking the offsets, which is where this encoding is usually got
// wrong: an offset is measured from the start of the argument list, and a
// tuple's members measure theirs from the start of the tuple.
//
// Build one with Word32, UintArray, ByteString, Tuple or List. The zero value
// is not usable.
type Arg struct {
	// head is the fixed part: the value itself when static, and nothing when
	// dynamic, where the offset is computed at assembly time.
	head []byte
	// tail is the variable part, empty for a static argument.
	tail []byte
	// dynamic says which of the two the value went into.
	dynamic bool
}

// Word32 makes an argument of one already-encoded word: any static type, so
// an address, a uint256, a bytes32 or a bool.
func Word32(w Word) Arg { return Arg{head: append([]byte(nil), w[:]...)} }

// UintArray makes a uint256[] argument, the shape a CTF partition and a set of
// index sets both take.
func UintArray(values []*big.Int) (Arg, error) {
	items := make([]Arg, len(values))
	for i, v := range values {
		w, err := uintWord(v)
		if err != nil {
			return Arg{}, fmt.Errorf("abi: array element %d: %w", i, err)
		}
		items[i] = Word32(w)
	}
	return List(items...), nil
}

// ByteString makes a bytes argument: its length, then its bytes padded up to a
// multiple of 32.
func ByteString(b []byte) Arg {
	length := Uint64(uint64(len(b)))
	tail := append([]byte(nil), length[:]...)
	tail = append(tail, b...)
	if pad := len(b) % 32; pad != 0 {
		tail = append(tail, make([]byte, 32-pad)...)
	}
	return Arg{tail: tail, dynamic: true}
}

// Tuple makes a tuple argument.
//
// A tuple is dynamic when any of its members is. That rule is what decides
// whether the members are written inline or behind an offset, and getting it
// wrong shifts every following argument.
func Tuple(members ...Arg) Arg {
	if !anyDynamic(members) {
		var head []byte
		for _, m := range members {
			head = append(head, m.head...)
		}
		return Arg{head: head}
	}
	return Arg{tail: assemble(members), dynamic: true}
}

// List makes an array argument of a fixed element type: the element count,
// then the elements encoded as their own argument list. It is always dynamic,
// element type notwithstanding, because the count is not known to the type.
func List(items ...Arg) Arg {
	length := Uint64(uint64(len(items)))
	tail := append([]byte(nil), length[:]...)
	return Arg{tail: append(tail, assemble(items)...), dynamic: true}
}

// EncodeArgs encodes an argument list: the whole of abi.encode.
func EncodeArgs(args ...Arg) []byte { return assemble(args) }

// EncodeArgsCall builds calldata: the four-byte selector for signature,
// followed by the encoded arguments.
func EncodeArgsCall(signature string, args ...Arg) []byte {
	return append(Selector(signature), assemble(args)...)
}

// assemble lays out a head and a tail, filling in the offset of every dynamic
// argument. An offset counts from the start of this list, not from the start
// of the calldata, which is why a nested tuple encodes on its own.
func assemble(args []Arg) []byte {
	headSize := 0
	for _, a := range args {
		if a.dynamic {
			headSize += 32
			continue
		}
		headSize += len(a.head)
	}

	head := make([]byte, 0, headSize)
	var tail []byte
	for _, a := range args {
		if !a.dynamic {
			head = append(head, a.head...)
			continue
		}
		offset := Uint64(uint64(headSize + len(tail)))
		head = append(head, offset[:]...)
		tail = append(tail, a.tail...)
	}
	return append(head, tail...)
}

// anyDynamic reports whether a tuple must be encoded behind an offset.
func anyDynamic(args []Arg) bool {
	for _, a := range args {
		if a.dynamic {
			return true
		}
	}
	return false
}

// uintWord encodes a non-negative integer into one word.
func uintWord(v *big.Int) (Word, error) {
	if v == nil {
		return Word{}, fmt.Errorf("abi: nil integer")
	}
	if v.Sign() < 0 {
		return Word{}, fmt.Errorf("abi: negative uint %s", v)
	}
	b := v.Bytes()
	if len(b) > 32 {
		return Word{}, fmt.Errorf("abi: %s does not fit in 32 bytes", v)
	}
	var w Word
	copy(w[32-len(b):], b)
	return w, nil
}

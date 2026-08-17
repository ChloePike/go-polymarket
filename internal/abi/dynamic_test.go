// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package abi

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// The vectors are a general Ethereum client's encoding of the same calls this
// package builds. An offset computed wrongly does not fail to decode: it
// decodes to different arguments, which for these calls means a different
// amount of a different position, or a different set of calls entirely.

// abiVectors is the slice of testdata/tx-vectors.json this package needs.
type abiVectors struct {
	Positions []abiCallCase  `json:"positions"`
	Batches   []abiBatchCase `json:"batches"`
}

// abiCallCase is one piece of calldata and the function it encodes.
type abiCallCase struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Data      string `json:"data"`
}

// abiBatchCase is a deposit wallet's batch: a tuple holding an array of tuples
// holding byte strings, which is every nesting rule at once. Nothing in this
// module sends one — the wallet takes a batch only from Polymarket's own
// factory — but it is the deepest shape the encoder must get right, so it is
// pinned here rather than thrown away.
type abiBatchCase struct {
	Name      string        `json:"name"`
	Wallet    string        `json:"wallet"`
	Nonce     string        `json:"nonce"`
	Deadline  string        `json:"deadline"`
	Signature string        `json:"signature"`
	Calls     []abiCallItem `json:"calls"`
	Data      string        `json:"data"`
}

// abiCallItem is one call inside a batch vector.
type abiCallItem struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Data   string `json:"data"`
}

func loadABIVectors(t *testing.T) abiVectors {
	t.Helper()
	b, err := os.ReadFile("../../testdata/tx-vectors.json")
	if err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	var v abiVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("tx vectors: %v", err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatalf("vector %q is not hex: %v", s, err)
	}
	return b
}

// mustWord unwraps an encoding that cannot fail for a constant argument. It
// panics rather than taking a *testing.T, so that it composes directly with
// the eip712 helpers it wraps.
func mustWord(w Word, err error) Word {
	if err != nil {
		panic("abi test: " + err.Error())
	}
	return w
}

// TestUintArrayVectors pins an argument list holding one dynamic array, the
// shape every conditional-token call takes.
func TestUintArrayVectors(t *testing.T) {
	v := loadABIVectors(t)
	byName := make(map[string]abiCallCase, len(v.Positions))
	for _, c := range v.Positions {
		byName[c.Name] = c
	}
	want, ok := byName["split"]
	if !ok {
		t.Fatal("no split vector")
	}

	token := mustWord(eip712.Address("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"))
	condition := mustWord(eip712.Bytes32("0x1763261a2bf8884e1cfce3c83522810db637064a17cf0695846762e9b2600aa1"))
	partition, err := UintArray([]*big.Int{big.NewInt(1), big.NewInt(2)})
	if err != nil {
		t.Fatalf("UintArray: %v", err)
	}
	amount := mustWord(eip712.Uint(big.NewInt(1_000_000)))

	got := EncodeArgsCall(want.Signature,
		Word32(token), Word32(Word{}), Word32(condition), partition, Word32(amount))
	if !strings.EqualFold("0x"+hex.EncodeToString(got), want.Data) {
		t.Errorf("split = 0x%x\nwant %s", got, want.Data)
	}
}

// TestNestedTupleVectors pins the deepest nesting: a dynamic tuple whose
// fourth member is an array of dynamic tuples, followed by a byte string.
func TestNestedTupleVectors(t *testing.T) {
	v := loadABIVectors(t)
	if len(v.Batches) == 0 {
		t.Fatal("no batch vectors")
	}
	for _, tc := range v.Batches {
		t.Run(tc.Name, func(t *testing.T) {
			calls := make([]Arg, len(tc.Calls))
			for i, call := range tc.Calls {
				calls[i] = Tuple(
					Word32(mustWord(eip712.Address(call.Target))),
					Word32(mustWord(eip712.UintString(call.Value))),
					ByteString(mustHex(t, call.Data)),
				)
			}
			batch := Tuple(
				Word32(mustWord(eip712.Address(tc.Wallet))),
				Word32(mustWord(eip712.UintString(tc.Nonce))),
				Word32(mustWord(eip712.UintString(tc.Deadline))),
				List(calls...),
			)
			got := EncodeArgsCall(
				"execute((address,uint256,uint256,(address,uint256,bytes)[]),bytes)",
				batch, ByteString(mustHex(t, tc.Signature)))
			if !strings.EqualFold("0x"+hex.EncodeToString(got), tc.Data) {
				t.Errorf("execute = 0x%x\nwant %s", got, tc.Data)
			}
		})
	}
}

// TestStaticTupleIsInline pins the rule that decides where a tuple's members
// go: a tuple of static members is written into the head, and only a tuple
// with a dynamic member is written behind an offset.
func TestStaticTupleIsInline(t *testing.T) {
	pair := Tuple(Word32(Uint64(1)), Word32(Uint64(2)))
	inline := EncodeArgs(pair, Word32(Uint64(3)))
	if len(inline) != 96 {
		t.Errorf("a static tuple encoded to %d bytes, want 96 with no offset", len(inline))
	}
	if got := hex.EncodeToString(inline[64:]); !strings.HasSuffix(got, "03") {
		t.Errorf("the following argument moved: %s", got)
	}

	withBytes := Tuple(Word32(Uint64(1)), ByteString([]byte{0xff}))
	dynamic := EncodeArgs(withBytes, Word32(Uint64(3)))
	// head: offset, 3; tail: the tuple's own head and tail.
	if len(dynamic) != 64+128 {
		t.Errorf("a dynamic tuple encoded to %d bytes, want %d", len(dynamic), 64+128)
	}
	offset := new(big.Int).SetBytes(dynamic[:32])
	if offset.Int64() != 64 {
		t.Errorf("tuple offset = %s, want 64", offset)
	}
}

// TestByteStringPadding covers the padding rule at its boundaries: a byte
// string is padded up to a multiple of 32, and one that already fits gains
// nothing.
func TestByteStringPadding(t *testing.T) {
	for _, size := range []int{0, 1, 31, 32, 33, 64} {
		arg := ByteString(make([]byte, size))
		want := 32 + 32*((size+31)/32)
		if len(arg.tail) != want {
			t.Errorf("a %d-byte string encoded to %d bytes, want %d", size, len(arg.tail), want)
		}
	}
}

// TestUintArrayRejectsNonsense covers the values that have no encoding.
func TestUintArrayRejectsNonsense(t *testing.T) {
	if _, err := UintArray([]*big.Int{big.NewInt(-1)}); err == nil {
		t.Error("encoded a negative array element")
	}
	if _, err := UintArray([]*big.Int{nil}); err == nil {
		t.Error("encoded a nil array element")
	}
	tooBig := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := UintArray([]*big.Int{tooBig}); err == nil {
		t.Error("encoded an element wider than a word")
	}
}

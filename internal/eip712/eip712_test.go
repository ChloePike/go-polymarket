// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package eip712

import (
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
)

// goldenTypeHashes is the slice of testdata/vectors.json this package pins.
// The order and ClobAuth digests themselves are checked end to end by the root
// package, which owns the field lists.
type goldenTypeHashes struct {
	Order  string `json:"order"`
	Domain string `json:"domain"`
}

// goldenFile is the subset of testdata/vectors.json this package reads. The
// file holds far more than this; everything else is another package's golden.
type goldenFile struct {
	TypeHashes goldenTypeHashes `json:"typeHashes"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	b, err := os.ReadFile("../../testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return g
}

const orderTypeString = "Order(uint256 salt,address maker,address signer," +
	"uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side," +
	"uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

const domainTypeString = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"

func TestTypeHash(t *testing.T) {
	g := loadGolden(t)
	if got := TypeHash(orderTypeString).Hex(); got != g.TypeHashes.Order {
		t.Errorf("order type hash = %s, want %s", got, g.TypeHashes.Order)
	}
	if got := TypeHash(domainTypeString).Hex(); got != g.TypeHashes.Domain {
		t.Errorf("domain type hash = %s, want %s", got, g.TypeHashes.Domain)
	}
}

// TestKeccak256 checks that this is Keccak and not FIPS-202 SHA-3. The two
// differ only in padding, so the empty-input digest is the cheapest way to
// tell them apart: SHA3-256("") starts a7ffc6f8, Keccak-256("") c5d24601.
func TestKeccak256(t *testing.T) {
	const emptyKeccak = "0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got := Keccak256().Hex(); got != emptyKeccak {
		t.Errorf("Keccak256() = %s, want %s", got, emptyKeccak)
	}
	const abcKeccak = "0x4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
	if got := Keccak256([]byte("abc")).Hex(); got != abcKeccak {
		t.Errorf("Keccak256(abc) = %s, want %s", got, abcKeccak)
	}
	// Writing in pieces must equal writing the whole.
	if Keccak256([]byte("ab"), []byte("c")) != Keccak256([]byte("abc")) {
		t.Error("split input differs from whole input")
	}
}

// encodeCase is one field-encoding expectation.
type encodeCase struct {
	name string
	got  Word
	want string
}

func TestEncoding(t *testing.T) {
	addr, err := Address("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	if err != nil {
		t.Fatal(err)
	}
	b32, err := Bytes32("0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417")
	if err != nil {
		t.Fatal(err)
	}
	big1, err := Uint(big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := UintString("52000000")
	if err != nil {
		t.Fatal(err)
	}

	cases := []encodeCase{
		{"address is left-padded", addr,
			"0x000000000000000000000000f39fd6e51aad88f6f4ce6ab8827279cfffb92266"},
		{"bytes32 is verbatim", b32,
			"0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417"},
		{"uint256 is big-endian", big1,
			"0x0000000000000000000000000000000000000000000000000000000000000001"},
		{"decimal string is parsed", dec,
			"0x0000000000000000000000000000000000000000000000000000000003197500"},
		{"uint8 sits in the last byte", Uint8(1),
			"0x0000000000000000000000000000000000000000000000000000000000000001"},
		{"string is hashed", String(""),
			"0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"},
	}
	for _, c := range cases {
		if got := c.got.Hex(); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// badInputCase is one input the encoders must reject rather than silently
// truncate or pad.
type badInputCase struct {
	name string
	err  error
}

func TestEncodingRejects(t *testing.T) {
	_, errShortAddr := Address("0xf39Fd6")
	_, errLongAddr := Address("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb9226600")
	_, errNotHex := Address("0xzzzzd6e51aad88F6F4ce6aB8827279cffFb92266")
	_, errShort32 := Bytes32("0x1234")
	_, errNotDecimal := UintString("0x10")
	_, errEmpty := UintString("")
	_, errNegative := Uint(big.NewInt(-1))
	_, errTooBig := Uint(new(big.Int).Lsh(big.NewInt(1), 256))

	cases := []badInputCase{
		{"short address", errShortAddr},
		{"long address", errLongAddr},
		{"address is not hex", errNotHex},
		{"short bytes32", errShort32},
		{"hex where decimal is required", errNotDecimal},
		{"empty decimal", errEmpty},
		{"negative uint", errNegative},
		{"uint wider than 32 bytes", errTooBig},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Errorf("%s: got nil error", c.name)
		}
	}
}

// TestDomainSeparatorOmitsVerifyingContract pins the difference between the two
// domains Polymarket uses. Encoding the missing contract as the zero address
// instead of dropping the field yields a different, wrong separator.
func TestDomainSeparatorOmitsVerifyingContract(t *testing.T) {
	withContract := Domain{
		Name:              "ClobAuthDomain",
		Version:           "1",
		ChainID:           big.NewInt(137),
		VerifyingContract: "0x0000000000000000000000000000000000000000",
	}
	without := Domain{
		Name:    "ClobAuthDomain",
		Version: "1",
		ChainID: big.NewInt(137),
	}
	a, err := withContract.Separator()
	if err != nil {
		t.Fatal(err)
	}
	b, err := without.Separator()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("omitting verifyingContract produced the same separator as zeroing it")
	}
}

func TestEncoderHoldsFirstError(t *testing.T) {
	var enc Encoder
	enc.Uint("salt", "1")
	enc.Address("maker", "not an address")
	enc.Bytes32("metadata", "also wrong")
	if _, err := enc.StructHash(TypeHash(orderTypeString)); err == nil {
		t.Fatal("got nil error")
	} else if want := "field maker"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the first bad field (%s)", err, want)
	}

	var ok Encoder
	ok.Uint("salt", "1")
	ok.Uint8("side", 0)
	if _, err := ok.StructHash(TypeHash(orderTypeString)); err != nil {
		t.Errorf("valid fields: %v", err)
	}
}

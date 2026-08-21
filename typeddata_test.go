// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestOrderTypedDataDigest checks that the payload an auditor reads hashes to
// the digest a wallet signs, for every golden order. Both halves of the API go
// through TypedData.Digest, so this pins them together rather than separately.
func TestOrderTypedDataDigest(t *testing.T) {
	g := loadGolden(t)
	for _, want := range g.Orders {
		t.Run(want.Name, func(t *testing.T) {
			order, opts := goldenToOrder(t, want)
			td, err := OrderTypedData(order, g.ChainID, opts)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := td.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if got := "0x" + hex.EncodeToString(digest[:]); got != want.Digest {
				t.Fatalf("digest = %s, want %s", got, want.Digest)
			}
			if td.Domain.VerifyingContract != want.Domain.VerifyingContract {
				t.Errorf("verifyingContract = %s, want %s",
					td.Domain.VerifyingContract, want.Domain.VerifyingContract)
			}
			if td.Domain.Version != want.Domain.Version {
				t.Errorf("domain version = %s, want %s", td.Domain.Version, want.Domain.Version)
			}
		})
	}
}

// goldenToOrder rebuilds an Order and its options from a golden vector.
func goldenToOrder(t *testing.T, g goldenOrder) (Order, OrderOptions) {
	t.Helper()
	order := Order{
		Salt:          g.Order.Salt,
		Maker:         g.Order.Maker,
		Signer:        g.Order.Signer,
		TokenID:       g.Order.TokenID,
		MakerAmount:   g.Order.MakerAmount,
		TakerAmount:   g.Order.TakerAmount,
		Side:          Side(g.Order.Side),
		SignatureType: SignatureType(g.Order.SignatureType),
		Timestamp:     g.Order.Timestamp,
		Metadata:      g.Order.Metadata,
		Builder:       g.Order.Builder,
	}
	opts := OrderOptions{
		TickSize: g.Input.TickSize,
		NegRisk:  g.Input.NegRisk,
		Version:  g.Input.Version,
	}
	return order, opts
}

// TestTypedDataSurvivesJSON is the property the audit use case rests on: the
// payload can be written out, read back — by another process, another
// language, a hardware wallet — and still hash to the same digest.
func TestTypedDataSurvivesJSON(t *testing.T) {
	g := loadGolden(t)
	for _, want := range g.Orders {
		t.Run(want.Name, func(t *testing.T) {
			order, opts := goldenToOrder(t, want)
			td, err := OrderTypedData(order, g.ChainID, opts)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(td)
			if err != nil {
				t.Fatal(err)
			}

			// Decode with UseNumber so integers do not become float64 and
			// quietly lose precision; typedUint accepts json.Number.
			var back TypedData
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.UseNumber()
			if err := dec.Decode(&back); err != nil {
				t.Fatal(err)
			}
			digest, err := back.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if got := "0x" + hex.EncodeToString(digest[:]); got != want.Digest {
				t.Fatalf("after a JSON round trip digest = %s, want %s", got, want.Digest)
			}
		})
	}
}

// TestClobAuthTypedDataDigest pins the level-1 payload, whose domain carries
// no verifying contract at all.
func TestClobAuthTypedDataDigest(t *testing.T) {
	g := loadGolden(t)
	for _, want := range g.ClobAuth {
		td := ClobAuthTypedData(want.Address, want.ChainID, want.Timestamp, want.Nonce)
		if td.Domain.VerifyingContract != "" {
			t.Errorf("the level-1 domain carries a verifying contract: %q", td.Domain.VerifyingContract)
		}
		if n := len(td.Types["EIP712Domain"]); n != 3 {
			t.Errorf("the level-1 domain type has %d fields, want 3", n)
		}
		digest, err := td.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if got := "0x" + hex.EncodeToString(digest[:]); got != want.Digest {
			t.Errorf("ts=%s digest = %s, want %s", want.Timestamp, got, want.Digest)
		}
	}
}

// TestTypeString renders the text whose hash goes into hashStruct.
func TestTypeString(t *testing.T) {
	g := loadGolden(t)
	order, opts := goldenToOrder(t, g.Orders[0])
	td, err := OrderTypedData(order, g.ChainID, opts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := td.TypeString()
	if err != nil {
		t.Fatal(err)
	}
	const want = "Order(uint256 salt,address maker,address signer,uint256 tokenId," +
		"uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType," +
		"uint256 timestamp,bytes32 metadata,bytes32 builder)"
	if got != want {
		t.Errorf("type string =\n%s\nwant\n%s", got, want)
	}
}

// brokenPayloadCase is one malformed payload TypedData.Digest must refuse.
type brokenPayloadCase struct {
	name   string
	mangle func(*TypedData)
}

// TestDigestRejectsBrokenPayloads checks that a payload it cannot encode
// faithfully produces an error rather than a hash over fewer fields. A digest
// computed from a partial struct is a valid signature over an order nobody
// wrote.
func TestDigestRejectsBrokenPayloads(t *testing.T) {
	cases := []brokenPayloadCase{
		{"unknown solidity type", func(td *TypedData) {
			td.Types["Order"][0].Type = "bytes16"
		}},
		{"missing message field", func(td *TypedData) {
			delete(td.Message, "builder")
		}},
		{"primary type absent", func(td *TypedData) {
			td.PrimaryType = "NotAType"
		}},
		{"uint given as text", func(td *TypedData) {
			td.Message["salt"] = "not a number"
		}},
		{"address given as a number", func(td *TypedData) {
			td.Message["maker"] = 1
		}},
		{"bytes32 of the wrong length", func(td *TypedData) {
			td.Message["builder"] = "0xdeadbeef"
		}},
		{"uint too large for a JSON number", func(td *TypedData) {
			td.Message["salt"] = float64(uint64(1) << 60)
		}},
		{"uint that is not whole", func(td *TypedData) {
			td.Message["salt"] = 1.5
		}},
	}

	g := loadGolden(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order, opts := goldenToOrder(t, g.Orders[0])
			td, err := OrderTypedData(order, g.ChainID, opts)
			if err != nil {
				t.Fatal(err)
			}
			tc.mangle(&td)
			if _, err := td.Digest(); err == nil {
				t.Fatal("got nil error")
			}
		})
	}
}

// recordingSigner is a TypedDataSigner that remembers the payload it was shown
// and signs it honestly, which is what an audit integration looks like.
type recordingSigner struct {
	inner *PrivateKey
	seen  []TypedData
}

func (s *recordingSigner) Address() string { return s.inner.Address() }

func (s *recordingSigner) SignDigest(digest [32]byte) ([]byte, error) {
	return nil, errSignDigestUsed
}

func (s *recordingSigner) SignTypedData(td TypedData) ([]byte, error) {
	s.seen = append(s.seen, td)
	digest, err := td.Digest()
	if err != nil {
		return nil, err
	}
	return s.inner.SignDigest(digest)
}

// errSignDigestUsed marks the digest-only path being taken when the payload
// path should have been.
var errSignDigestUsed = errTest("SignDigest was called on a TypedDataSigner")

type errTest string

func (e errTest) Error() string { return string(e) }

// TestTypedDataSignerIsPreferred checks that a signer which can see the
// payload is given it, rather than being handed a bare hash.
func TestTypedDataSignerIsPreferred(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	signer := &recordingSigner{inner: key}

	want := g.Orders[0]
	order, opts := goldenToOrder(t, want)
	signed, err := SignOrder(order, g.ChainID, opts, signer)
	if err != nil {
		t.Fatal(err)
	}
	if signed.Signature != want.Signature {
		t.Errorf("signature = %s, want %s", signed.Signature, want.Signature)
	}
	if len(signer.seen) != 1 {
		t.Fatalf("the signer saw %d payloads, want 1", len(signer.seen))
	}
	if signer.seen[0].PrimaryType != "Order" {
		t.Errorf("the signer was shown a %q payload", signer.seen[0].PrimaryType)
	}
	if signer.seen[0].Message["makerAmount"] != want.Order.MakerAmount {
		t.Errorf("the payload the signer saw carries makerAmount %v, want %s",
			signer.seen[0].Message["makerAmount"], want.Order.MakerAmount)
	}

	// The level-1 handshake takes the same path.
	if _, err := BuildL1Headers(signer, g.ChainID, "1740000000", 0); err != nil {
		t.Fatal(err)
	}
	if len(signer.seen) != 2 || signer.seen[1].PrimaryType != "ClobAuth" {
		t.Errorf("level-1 did not reach the payload signer: %d payloads seen", len(signer.seen))
	}
}

// wrongSigner signs something other than what it was shown, which is exactly
// the integration mistake this package refuses to let through.
type wrongSigner struct {
	inner *PrivateKey
}

func (s wrongSigner) Address() string { return s.inner.Address() }

func (s wrongSigner) SignDigest(digest [32]byte) ([]byte, error) {
	return s.inner.SignDigest(digest)
}

func (s wrongSigner) SignTypedData(td TypedData) ([]byte, error) {
	var other [32]byte
	other[0] = 0xff
	return s.inner.SignDigest(other)
}

// TestTypedDataSignerMustSignWhatItSaw checks the recovery guard: a signature
// over different data is caught here rather than by the exchange.
func TestTypedDataSignerMustSignWhatItSaw(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	order, opts := goldenToOrder(t, g.Orders[0])
	_, err = SignOrder(order, g.ChainID, opts, wrongSigner{inner: key})
	if err == nil {
		t.Fatal("a signature over different data was accepted")
	}
	if !strings.Contains(err.Error(), "different data") {
		t.Errorf("error %q does not explain the mismatch", err)
	}
}

// TestVerifySignature checks the recovery helper directly, including that a
// signature from the right key over the wrong digest is rejected.
func TestVerifySignature(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	want := g.Orders[0]
	raw, err := hex.DecodeString(want.Digest[2:])
	if err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	copy(digest[:], raw)

	sig, err := key.SignDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(digest, sig, key.Address()); err != nil {
		t.Fatalf("a valid signature was rejected: %v", err)
	}
	// Lowercase input must still verify: the checksum is presentation.
	if err := VerifySignature(digest, sig, strings.ToLower(key.Address())); err != nil {
		t.Errorf("a lowercase address was rejected: %v", err)
	}
	// A different digest must not verify.
	var other [32]byte
	copy(other[:], raw)
	other[0] ^= 0xff
	if err := VerifySignature(other, sig, key.Address()); err == nil {
		t.Error("a signature over a different digest verified")
	}
	// A different address must not verify.
	if err := VerifySignature(digest, sig, g.Accounts[1].Address); err == nil {
		t.Error("a signature verified against the wrong address")
	}
	// A malformed signature must not panic.
	if err := VerifySignature(digest, sig[:10], key.Address()); err == nil {
		t.Error("a short signature verified")
	}
}

// TestTypedDataSaltIsAString guards the JSON shape an external signer sees:
// a uint256 carried as a JSON number stops being exact above 2^53, so every
// numeric field except the two small enums travels as text.
func TestTypedDataSaltIsAString(t *testing.T) {
	g := loadGolden(t)
	order, opts := goldenToOrder(t, g.Orders[0])
	td, err := OrderTypedData(order, g.ChainID, opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(td.Message)
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"salt", "tokenId", "makerAmount", "takerAmount", "timestamp"} {
		if !strings.HasPrefix(string(message[field]), `"`) {
			t.Errorf("%s is a JSON number (%s); a uint256 must travel as a string",
				field, message[field])
		}
	}
	// The salt must also survive being read back as an integer.
	if _, err := strconv.ParseInt(order.Salt, 10, 64); err != nil {
		t.Errorf("salt %q is not an integer: %v", order.Salt, err)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// siweTestKey is the well-known Hardhat development key: public, holds
// nothing, and used here only so signatures are reproducible.
const siweTestKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// siweTestTime is a fixed issue time, so the message text and the token are
// reproducible.
var siweTestTime = time.Date(2026, 8, 21, 3, 47, 46, 0, time.UTC)

// siweTestMessage returns the message the tests here pin. The address is the
// test key's own; the layout around it is the one production accepted.
func siweTestMessage() SIWEMessage {
	return SIWEMessage{
		Domain:         SIWEDomain,
		Address:        "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		Statement:      SIWEStatement,
		URI:            SIWEURI,
		Version:        SIWEVersion,
		ChainID:        ChainPolygon,
		Nonce:          "siwe-test-nonce-1",
		IssuedAt:       siweTestTime,
		ExpirationTime: siweTestTime.Add(time.Hour),
	}
}

// TestSIWEMessageText pins the exact EIP-4361 text. The server rebuilds this
// string from the JSON half of the token and recovers the signer from it, so a
// byte that differs here authenticates as nobody. This layout was accepted by
// production.
func TestSIWEMessageText(t *testing.T) {
	want := "polymarket.com wants you to sign in with your Ethereum account:\n" +
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266\n" +
		"\n" +
		"Welcome to Polymarket! Sign to connect.\n" +
		"\n" +
		"URI: https://polymarket.com\n" +
		"Version: 1\n" +
		"Chain ID: 137\n" +
		"Nonce: siwe-test-nonce-1\n" +
		"Issued At: 2026-08-21T03:47:46Z\n" +
		"Expiration Time: 2026-08-21T04:47:46Z"
	if got := siweTestMessage().String(); got != want {
		t.Errorf("SIWEMessage.String() =\n%q\nwant\n%q", got, want)
	}
}

// TestSIWETokenLayout pins where the separator sits. The token is one base64
// blob whose plaintext is json:::signature — NOT a base64 body joined to a
// signature by the same three bytes. Both spellings look right and only this
// one authenticates; the other is a 401 that says nothing about which half is
// wrong.
func TestSIWETokenLayout(t *testing.T) {
	key, err := NewPrivateKey(siweTestKey)
	if err != nil {
		t.Fatal(err)
	}
	msg := siweTestMessage()
	msg.Address = key.Address()

	token, err := SignSIWE(key, msg)
	if err != nil {
		t.Fatal(err)
	}

	// The whole token decodes as base64. If the separator were outside, it
	// would not: ":" is not a base64 character.
	plain, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not one base64 blob, so the separator is outside it: %v", err)
	}
	body, sigHex, ok := strings.Cut(string(plain), siweTokenSeparator)
	if !ok {
		t.Fatalf("no %q inside the token", siweTokenSeparator)
	}

	var got siweFields
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("the half before the separator is not the fields JSON: %v", err)
	}
	if got != msg.fields() {
		t.Errorf("fields = %+v, want %+v", got, msg.fields())
	}

	// The signature half is 0x-prefixed hex of the 65 signed bytes, and it
	// must recover to the signing address over the message text.
	if !strings.HasPrefix(sigHex, "0x") {
		t.Fatalf("signature half %q has no 0x prefix", sigHex)
	}
	sig, err := hex.DecodeString(sigHex[2:])
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Fatalf("signature is %d bytes, want 65", len(sig))
	}
	if err := VerifySignature(msg.Digest(), sig, key.Address()); err != nil {
		t.Errorf("signature does not recover to the signer: %v", err)
	}
}

// TestSIWEDigestIsAPersonalMessage pins the EIP-191 step. The message is signed
// as a personal message, not hashed raw and not as typed data.
func TestSIWEDigestIsAPersonalMessage(t *testing.T) {
	msg := siweTestMessage()
	if msg.Digest() != PersonalDigest([]byte(msg.String())) {
		t.Error("SIWEMessage.Digest() is not the personal-message digest of its text")
	}
}

// TestNewSIWEMessageFillsTheConstants checks that the four fixed fields come
// from the constants rather than from a caller.
func TestNewSIWEMessageFillsTheConstants(t *testing.T) {
	m := NewSIWEMessage("0xabc", "nonce-1", ChainPolygon, time.Hour)
	if m.Domain != SIWEDomain || m.URI != SIWEURI ||
		m.Statement != SIWEStatement || m.Version != SIWEVersion {
		t.Errorf("constants not applied: %+v", m)
	}
	if got := m.ExpirationTime.Sub(m.IssuedAt); got != time.Hour {
		t.Errorf("lifetime = %v, want 1h", got)
	}
}

// TestSIWETokenRejectsAMalformedSignature checks the length guard: a short
// signature would produce a token the server answers 401 to, with nothing
// locally to say why.
func TestSIWETokenRejectsAMalformedSignature(t *testing.T) {
	if _, err := siweTestMessage().Token(make([]byte, 64)); err == nil {
		t.Error("a 64-byte signature was accepted")
	}
}

// personalOnlySigner is a key that will not sign a bare digest, the way a
// custodial signing service will not.
type personalOnlySigner struct {
	inner   Signer
	message []byte
	digests int
}

func (p *personalOnlySigner) Address() string { return p.inner.Address() }

func (p *personalOnlySigner) SignDigest(d [32]byte) ([]byte, error) {
	p.digests++
	return nil, fmt.Errorf("this signer refuses unframed digests")
}

func (p *personalOnlySigner) SignPersonal(message []byte) ([]byte, error) {
	p.message = append([]byte(nil), message...)
	return p.inner.SignDigest(PersonalDigest(message))
}

// TestSignSIWEPrefersThePersonalPath is why PersonalSigner exists.
//
// A signer that only signs framed messages must be able to log in, and the
// token it produces has to be byte-for-byte what a digest signer would have
// produced — the framing moves across the boundary, the signed bytes do not
// change. If SignSIWE ever reaches for SignDigest first, a custodial signer
// fails at login with an error that looks like a credential problem.
func TestSignSIWEPrefersThePersonalPath(t *testing.T) {
	key, err := NewPrivateKey(siweTestKey)
	if err != nil {
		t.Fatal(err)
	}
	msg := siweTestMessage()
	msg.Address = key.Address()

	want, err := SignSIWE(key, msg)
	if err != nil {
		t.Fatal(err)
	}

	custodial := &personalOnlySigner{inner: key}
	got, err := SignSIWE(custodial, msg)
	if err != nil {
		t.Fatalf("a personal-only signer could not sign in: %v", err)
	}
	if custodial.digests != 0 {
		t.Errorf("SignDigest was called %d times; the personal path must be preferred", custodial.digests)
	}
	if string(custodial.message) != msg.String() {
		t.Errorf("signed the wrong bytes:\n got %q\nwant %q", custodial.message, msg.String())
	}
	if got != want {
		t.Errorf("token differs from the digest path:\n got %s\nwant %s", got, want)
	}
}

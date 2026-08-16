// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"testing"
)

func TestSignHMAC(t *testing.T) {
	g := loadGolden(t)
	if len(g.HMAC) == 0 {
		t.Fatal("golden HMAC vectors are empty")
	}
	for _, tc := range g.HMAC {
		got, err := SignHMAC(tc.Secret, tc.Timestamp, tc.Method, tc.RequestPath, tc.Body)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.Method, tc.RequestPath, err)
		}
		if got != tc.Signature {
			t.Errorf("%s %s body=%q: signature = %s, want %s",
				tc.Method, tc.RequestPath, tc.Body, got, tc.Signature)
		}
	}
}

// TestSignHMACSecretForms checks that the same key expressed with and without
// padding, and in either base64 alphabet, produces the same signature.
func TestSignHMACSecretForms(t *testing.T) {
	const (
		padded   = "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM="
		unpadded = "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM"
	)
	want, err := SignHMAC(padded, "1740000000", "GET", "/order", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := SignHMAC(unpadded, "1740000000", "GET", "/order", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("unpadded secret gave %s, want %s", got, want)
	}
	if _, err := SignHMAC("not base64!!", "1", "GET", "/order", ""); err == nil {
		t.Error("invalid secret: got nil error")
	}
}

func TestClobAuthDigestAndHeaders(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.ClobAuth) == 0 {
		t.Fatal("golden ClobAuth vectors are empty")
	}
	for _, tc := range g.ClobAuth {
		digest, err := ClobAuthDigest(tc.Address, tc.ChainID, tc.Timestamp, tc.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		if h := "0x" + hex.EncodeToString(digest[:]); h != tc.Digest {
			t.Errorf("ts=%s nonce=%d digest = %s, want %s", tc.Timestamp, tc.Nonce, h, tc.Digest)
		}

		hdr, err := BuildL1Headers(key, tc.ChainID, tc.Timestamp, tc.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Signature != tc.Signature {
			t.Errorf("ts=%s nonce=%d signature = %s, want %s",
				tc.Timestamp, tc.Nonce, hdr.Signature, tc.Signature)
		}
		if hdr.Address != tc.Address {
			t.Errorf("address = %s, want %s", hdr.Address, tc.Address)
		}
		m := hdr.header()
		for _, k := range []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_TIMESTAMP", "POLY_NONCE"} {
			if m[k] == "" {
				t.Errorf("header %s is empty", k)
			}
		}
	}
}

func TestBuildL2Headers(t *testing.T) {
	g := loadGolden(t)
	tc := g.HMAC[0]
	creds := APICreds{Key: "key-1", Secret: tc.Secret, Passphrase: "pass-1"}
	h, err := BuildL2Headers(creds, "0xabc", tc.Timestamp, tc.Method, tc.RequestPath, tc.Body)
	if err != nil {
		t.Fatal(err)
	}
	if h.Signature != tc.Signature {
		t.Errorf("signature = %s, want %s", h.Signature, tc.Signature)
	}
	m := h.header()
	if m["POLY_API_KEY"] != "key-1" || m["POLY_PASSPHRASE"] != "pass-1" {
		t.Errorf("credentials not carried into headers: %v", m)
	}
}

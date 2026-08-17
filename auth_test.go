// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"strings"
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
		m := hdr.Header()
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
	m := h.Header()
	if m["POLY_API_KEY"] != "key-1" || m["POLY_PASSPHRASE"] != "pass-1" {
		t.Errorf("credentials not carried into headers: %v", m)
	}
}

// A recordingAuthenticator is an L2Authenticator that holds no secret: it
// stands in for a signing service, records what it was asked to sign, and
// answers with a fixed signature. It exists to prove a session can
// authenticate with nothing secret in this process.
type recordingAuthenticator struct {
	key      string
	requests []signedRequest
}

// A signedRequest is one request an authenticator was asked to sign.
type signedRequest struct {
	Address     string
	Method      string
	RequestPath string
	Body        string
}

func (a *recordingAuthenticator) APIKey() string { return a.key }

func (a *recordingAuthenticator) AuthHeaders(address, method, requestPath, body string) (L2Headers, error) {
	a.requests = append(a.requests, signedRequest{address, method, requestPath, body})
	return L2Headers{
		Address:    address,
		Signature:  "signed-elsewhere",
		APIKey:     a.key,
		Passphrase: "from-the-service",
		Timestamp:  "1740000000",
	}, nil
}

// TestAPICredsAuthenticateAsThemselves checks that the in-memory
// implementation signs exactly what BuildL2Headers does. It cannot control the
// clock, so it signs again with the timestamp that came back: a header set
// carrying one timestamp and a signature over another is rejected by the
// exchange, and that is the mistake this catches.
func TestAPICredsAuthenticateAsThemselves(t *testing.T) {
	creds := APICreds{
		Key:        "0c9e0eaa-f4f0-4d1e-8b7e-b3b1b7b0b0b0",
		Secret:     "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM=",
		Passphrase: "a passphrase",
	}
	const address = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

	got, err := creds.AuthHeaders(address, "POST", "/order", `{"order":1}`)
	if err != nil {
		t.Fatalf("AuthHeaders: %v", err)
	}
	want, err := BuildL2Headers(creds, address, got.Timestamp, "POST", "/order", `{"order":1}`)
	if err != nil {
		t.Fatalf("BuildL2Headers: %v", err)
	}
	if got != want {
		t.Errorf("headers = %+v, want %+v", got, want)
	}
	if creds.APIKey() != creds.Key {
		t.Errorf("APIKey() = %s, want %s", creds.APIKey(), creds.Key)
	}
}

// TestCredentialsFromEnv covers the loader, including the partial set that
// authenticates as nobody.
func TestCredentialsFromEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	t.Setenv(EnvAPISecret, "secret")
	t.Setenv(EnvAPIPassphrase, "passphrase")
	creds, err := CredentialsFromEnv()
	if err != nil {
		t.Fatalf("CredentialsFromEnv: %v", err)
	}
	if creds.Key != "key" || creds.Secret != "secret" || creds.Passphrase != "passphrase" {
		t.Errorf("credentials = %+v", creds)
	}

	t.Setenv(EnvAPISecret, "")
	if _, err := CredentialsFromEnv(); err == nil {
		t.Error("loaded a partial credential set")
	} else if !strings.Contains(err.Error(), EnvAPISecret) {
		t.Errorf("error %v does not name the missing variable", err)
	}
}

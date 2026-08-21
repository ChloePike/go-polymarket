// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package gamma

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// authTestKey is the well-known Hardhat development key: public, holds
// nothing, and used here only so signatures are reproducible.
const authTestKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// loginServer is a test server standing in for the sign-in pair. It records
// what the login request carried.
type loginServer struct {
	server *httptest.Server
	// Nonce is the value the nonce endpoint handed out.
	Nonce string
	// Token is the bearer token the login request presented.
	Token string
	// NonceCookie is the value of the nonce cookie the login sent back, empty
	// if it sent none.
	NonceCookie string
	// LoginMethod is the HTTP method the login request used.
	LoginMethod string
}

// newLoginServer returns a server answering /nonce and /login.
func newLoginServer(t *testing.T) *loginServer {
	t.Helper()
	ls := &loginServer{Nonce: "siwe-test-nonce-1"}
	ls.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case epNonce:
			http.SetCookie(w, &http.Cookie{Name: "polymarketnonce", Value: "nonce-cookie", Path: "/"})
			json.NewEncoder(w).Encode(NonceResponse{Nonce: ls.Nonce})
		case epLogin:
			ls.LoginMethod = r.Method
			ls.Token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if c, err := r.Cookie("polymarketnonce"); err == nil {
				ls.NonceCookie = c.Value
			}
			json.NewEncoder(w).Encode(LoginResponse{Type: "metamask", Address: "0x0"})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(ls.server.Close)
	return ls
}

// loginClient returns a client with a signer and a jar, pointed at the server.
func loginClient(t *testing.T, host string) *Client {
	t.Helper()
	key, err := polymarket.NewPrivateKey(authTestKey)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := NewCookieJar()
	if err != nil {
		t.Fatal(err)
	}
	return New(WithHost(host), WithSigner(key), WithCookieJar(jar))
}

// TestLoginSignsTheNonceAndReturnsTheCookie walks the whole handshake: the
// nonce is fetched, the cookie it set travels with the login, and the token
// carries a signature over the message the nonce belongs to.
func TestLoginSignsTheNonceAndReturnsTheCookie(t *testing.T) {
	ls := newLoginServer(t)
	c := loginClient(t, ls.server.URL)

	got, err := c.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "metamask" {
		t.Errorf("Type = %q, want metamask", got.Type)
	}
	if ls.LoginMethod != http.MethodGet {
		t.Errorf("login method = %s, want GET: a POST is answered 405", ls.LoginMethod)
	}
	if ls.NonceCookie != "nonce-cookie" {
		t.Error("the login did not send the cookie the nonce response set; the nonce is bound to it")
	}

	// The token must be one base64 blob carrying the nonce that was issued.
	plain, err := base64.StdEncoding.DecodeString(ls.Token)
	if err != nil {
		t.Fatalf("token is not base64: %v", err)
	}
	body, sigHex, ok := strings.Cut(string(plain), ":::")
	if !ok {
		t.Fatal("no separator inside the token")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["nonce"] != ls.Nonce {
		t.Errorf("token nonce = %v, want %q", fields["nonce"], ls.Nonce)
	}
	if fields["domain"] != polymarket.SIWEDomain {
		t.Errorf("token domain = %v, want %q", fields["domain"], polymarket.SIWEDomain)
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil || len(sig) != 65 {
		t.Fatalf("signature half is not 65 bytes of hex: %v", err)
	}
}

// TestLoginNeedsASigner checks that the handshake refuses locally rather than
// sending an unsigned request.
func TestLoginNeedsASigner(t *testing.T) {
	ls := newLoginServer(t)
	jar, err := NewCookieJar()
	if err != nil {
		t.Fatal(err)
	}
	c := New(WithHost(ls.server.URL), WithCookieJar(jar))
	if _, err := c.Login(context.Background()); err == nil {
		t.Fatal("login without a signer succeeded")
	}
}

// TestLoginNeedsACookieJar checks the other half of the guard. Without a jar
// the requests would succeed and the session cookie would be dropped, leaving
// the failure to surface much later as an unauthorized mint.
func TestLoginNeedsACookieJar(t *testing.T) {
	ls := newLoginServer(t)
	key, err := polymarket.NewPrivateKey(authTestKey)
	if err != nil {
		t.Fatal(err)
	}
	c := New(WithHost(ls.server.URL), WithSigner(key))
	if _, err := c.Login(context.Background()); err == nil {
		t.Fatal("login without a cookie jar succeeded")
	}
	if ls.Token != "" {
		t.Error("the login reached the network without a jar to keep its result")
	}
}

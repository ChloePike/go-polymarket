// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package relayer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// mintedKeyJSON is the body the mint endpoint returns, with invented values in
// the shape production sends. It is the same shape APIKeys lists, which is why
// the mint needs no type of its own.
const mintedKeyJSON = `{"apiKey":"01967c03-b8c8-7000-8f68-8b8eaec6fd3d",` +
	`"address":"0x77837466dd64fb52ecd00c737f060d0ff5ccb575",` +
	`"createdAt":"2026-08-21T03:38:08.947483382Z",` +
	`"updatedAt":"2026-08-21T03:38:08.947483382Z"}`

// mintServer records the session cookie a mint request presented.
type mintServer struct {
	server *httptest.Server
	// Path is the path the mint request used.
	Path string
	// Method is the method it used.
	Method string
	// SessionCookie is the session cookie value it sent, empty if none.
	SessionCookie string
	// Credentials reports whether any header credential travelled too.
	Credentials bool
}

// newMintServer returns a server answering the mint endpoint.
func newMintServer(t *testing.T) *mintServer {
	t.Helper()
	ms := &mintServer{}
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms.Path = r.URL.Path
		ms.Method = r.Method
		if c, err := r.Cookie("polymarketsession"); err == nil {
			ms.SessionCookie = c.Value
		}
		for _, k := range credentialHeaders {
			if r.Header.Get(k) != "" {
				ms.Credentials = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mintedKeyJSON))
	}))
	t.Cleanup(ms.server.Close)
	return ms
}

// TestMintAPIKeyUsesTheSessionCookie is the bootstrap this call exists for: a
// caller with no relayer credential at all trades a session cookie for one.
func TestMintAPIKeyUsesTheSessionCookie(t *testing.T) {
	ms := newMintServer(t)
	jar, err := polymarket.NewCookieJar()
	if err != nil {
		t.Fatal(err)
	}
	// Seed the jar the way a Gamma login would have.
	u, err := url.Parse(ms.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "polymarketsession", Value: "session-value", Path: "/"}})

	c := New(WithHost(ms.server.URL), WithCookieJar(jar))
	got, err := c.MintAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if ms.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", ms.Method)
	}
	if ms.Path != epMintAPIKey {
		t.Errorf("path = %s, want %s", ms.Path, epMintAPIKey)
	}
	if ms.SessionCookie != "session-value" {
		t.Error("the mint did not present the session cookie")
	}
	if ms.Credentials {
		t.Error("the mint sent a header credential: it authenticates by cookie alone")
	}
	if got.Key != "01967c03-b8c8-7000-8f68-8b8eaec6fd3d" {
		t.Errorf("Key = %q", got.Key)
	}
	if got.Address != "0x77837466dd64fb52ecd00c737f060d0ff5ccb575" {
		t.Errorf("Address = %q", got.Address)
	}
	// The minted pair must be usable as a credential without further work.
	if creds := got.Credentials(); creds.Key != got.Key || creds.Address != got.Address {
		t.Errorf("Credentials() = %+v, does not match the minted key", creds)
	}
}

// TestMintAPIKeyNeedsACookieJar checks the guard. Without a jar there is no
// session to present, and the failure would otherwise arrive from the server
// as an unauthorized caller rather than from the client as a misconfiguration.
func TestMintAPIKeyNeedsACookieJar(t *testing.T) {
	ms := newMintServer(t)
	c := New(WithHost(ms.server.URL))
	if _, err := c.MintAPIKey(context.Background()); err == nil {
		t.Fatal("minting without a cookie jar succeeded")
	}
	if ms.Method != "" {
		t.Error("the mint reached the network with no session to present")
	}
}

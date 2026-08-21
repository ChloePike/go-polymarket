// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// cookieRecorder is a test server that sets a cookie on the first request and
// records what every later request sent back.
type cookieRecorder struct {
	server *httptest.Server
	seen   []string
}

// newCookieRecorder returns a server that answers every request with an empty
// JSON object, setting the named cookie once.
func newCookieRecorder(t *testing.T, name, value string) *cookieRecorder {
	t.Helper()
	rec := &cookieRecorder{}
	first := true
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(name); err == nil {
			rec.seen = append(rec.seen, c.Value)
		} else {
			rec.seen = append(rec.seen, "")
		}
		if first {
			http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
			first = false
		}
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

// TestCookieJarCarriesACookieBetweenRequests checks the jar the sign-in
// handshake needs: a cookie a response sets must travel on the next request.
func TestCookieJarCarriesACookieBetweenRequests(t *testing.T) {
	rec := newCookieRecorder(t, "polymarketsession", "session-value")
	jar, err := NewCookieJar()
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(rec.server.URL, WithHost(rec.server.URL), WithCookieJar(jar))

	for i := 0; i < 2; i++ {
		if err := s.Get(context.Background(), "/anything", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.seen) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(rec.seen))
	}
	if rec.seen[0] != "" {
		t.Errorf("first request already carried a cookie: %q", rec.seen[0])
	}
	if rec.seen[1] != "session-value" {
		t.Errorf("second request carried %q, want the cookie the first response set", rec.seen[1])
	}
}

// TestNoCookieJarStoresNothing checks the default. A session without a jar
// must not accumulate cookies, so an ordinary client never sends one.
func TestNoCookieJarStoresNothing(t *testing.T) {
	rec := newCookieRecorder(t, "polymarketsession", "session-value")
	s := NewSession(rec.server.URL, WithHost(rec.server.URL))

	for i := 0; i < 2; i++ {
		if err := s.Get(context.Background(), "/anything", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if rec.seen[1] != "" {
		t.Errorf("a session with no jar sent a cookie: %q", rec.seen[1])
	}
	if s.CookieJar() != nil {
		t.Error("CookieJar() is not nil by default")
	}
}

// TestSharedCookieJarSpansSessions is the property that carries a Gamma login
// to the relayer host: two sessions given the same jar share the login.
func TestSharedCookieJarSpansSessions(t *testing.T) {
	rec := newCookieRecorder(t, "polymarketsession", "session-value")
	jar, err := NewCookieJar()
	if err != nil {
		t.Fatal(err)
	}
	a := NewSession(rec.server.URL, WithHost(rec.server.URL), WithCookieJar(jar))
	b := NewSession(rec.server.URL, WithHost(rec.server.URL), WithCookieJar(jar))

	if err := a.Get(context.Background(), "/login", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Get(context.Background(), "/mint", nil, nil); err != nil {
		t.Fatal(err)
	}
	if rec.seen[1] != "session-value" {
		t.Errorf("the second session sent %q, want the cookie the first one obtained", rec.seen[1])
	}
}

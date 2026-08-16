// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// testCreds are level-2 credentials with a syntactically valid secret. They
// authenticate as nobody; the tests here check what is signed, not who.
var testCreds = APICreds{
	Key:        "8f1b3a6e-0000-4000-8000-000000000000",
	Secret:     "c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0LXNlY3JldA==",
	Passphrase: "passphrase",
}

// testSession returns a session pointed at a handler, with a signer and
// credentials, and with pacing off so a test never waits on a token.
func testSession(t *testing.T, h http.Handler) (*Session, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return NewSession(srv.URL, WithSigner(key), WithCredentials(testCreds)), srv
}

// TestLevel2SignatureCoversThePathNotTheQuery is the regression test for a
// bug that no golden vector could have caught.
//
// The level-2 signature covers the method, the path and the body, and NOT the
// query string. That is the exchange's rule: its own client signs one set of
// headers and then reuses them across every page of a pagination loop while
// the cursor changes underneath. Signing the query instead looks perfectly
// correct locally and returns 401 for every filtered or paginated request.
func TestLevel2SignatureCoversThePathNotTheQuery(t *testing.T) {
	var got http.Header
	s, _ := testSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{}`))
	}))

	const path = "/data/orders"
	query := url.Values{"next_cursor": {"MA=="}, "market": {"0xabc"}}
	if err := s.GetL2(context.Background(), path, query, nil); err != nil {
		t.Fatal(err)
	}

	timestamp := got.Get("POLY_TIMESTAMP")
	if timestamp == "" {
		t.Fatal("no timestamp header")
	}
	bare, err := BuildL2Headers(testCreds, s.Signer().Address(), timestamp, http.MethodGet, path, "")
	if err != nil {
		t.Fatal(err)
	}
	if sent := got.Get("POLY_SIGNATURE"); sent != bare.Signature {
		t.Errorf("signature = %s, want the signature over the bare path %s", sent, bare.Signature)
	}

	// And it must not be the signature over the path with its query, which is
	// the shape a reasonable reading of "sign the request" produces.
	withQuery, err := BuildL2Headers(testCreds, s.Signer().Address(), timestamp,
		http.MethodGet, path+"?"+query.Encode(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("POLY_SIGNATURE") == withQuery.Signature {
		t.Error("the query string was signed")
	}
}

// TestRequestHeadersReachTheWire checks the channel for a header that is not
// authentication, such as the builder code a bridge deposit is attributed
// with.
func TestRequestHeadersReachTheWire(t *testing.T) {
	var got http.Header
	s, _ := testSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{}`))
	}))

	err := s.Do(context.Background(), Request{
		Method:  http.MethodGet,
		Path:    "/anything",
		Headers: map[string]string{"X-Builder-Code": "0xabc", "X-Other": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Get("X-Builder-Code"); v != "0xabc" {
		t.Errorf("X-Builder-Code = %q, want 0xabc", v)
	}
	if v := got.Get("X-Other"); v != "1" {
		t.Errorf("X-Other = %q, want 1", v)
	}
}

// TestAuthHeadersOutrankCallerHeaders checks the precedence that matters. A
// caller may add a header but must not be able to replace the one that proves
// who is sending the request — by accident or otherwise.
//
// Note the underscores. Polymarket's credential headers are POLY_SIGNATURE
// and friends, not the hyphenated form every other header in HTTP uses, and
// the two are different headers rather than two spellings of one.
func TestAuthHeadersOutrankCallerHeaders(t *testing.T) {
	var got http.Header
	s, _ := testSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{}`))
	}))

	err := s.Do(context.Background(), Request{
		Method:  http.MethodGet,
		Path:    "/data/orders",
		Auth:    AuthL2,
		Headers: map[string]string{"POLY_SIGNATURE": "forged", "POLY_API_KEY": "forged"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("POLY_SIGNATURE") == "forged" {
		t.Error("a caller header replaced the signature")
	}
	if got.Get("POLY_API_KEY") != testCreds.Key {
		t.Errorf("api key = %q, want %q", got.Get("POLY_API_KEY"), testCreds.Key)
	}
}

// TestReadsRetryAndWritesDoNot pins the asymmetry the whole retry policy
// exists for. A read that never reached an answer costs nothing to repeat; a
// write that may have arrived costs a second position.
func TestReadsRetryAndWritesDoNot(t *testing.T) {
	var attempts atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang up without answering, the failure a retry is for.
		attempts.Add(1)
		hijacked, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		hijacked.Close()
	})

	s, _ := testSession(t, handler)
	if err := s.Get(context.Background(), "/book", nil, nil); err == nil {
		t.Fatal("a dropped connection reported success")
	}
	if got := attempts.Load(); got != defaultRetries+1 {
		t.Errorf("a read was attempted %d times, want %d", got, defaultRetries+1)
	}

	attempts.Store(0)
	if err := s.PostL2(context.Background(), "/order", map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("a dropped connection reported success")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("a write was attempted %d times, want exactly 1: a resubmitted order is a second position", got)
	}
}

// TestErrorCarriesTheStatus checks that a refusal becomes an *Error a caller
// can branch on, rather than a string to match against.
func TestErrorCarriesTheStatus(t *testing.T) {
	s, _ := testSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"slow down"}`))
	}))

	err := s.Get(context.Background(), "/book", nil, nil)
	if err == nil {
		t.Fatal("a 429 reported success")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", apiErr.StatusCode)
	}
	if apiErr.Message != "slow down" {
		t.Errorf("message = %q, want the server's own", apiErr.Message)
	}
	if Indeterminate(err) {
		t.Error("a 429 was reported as indeterminate: the exchange answered, so nothing was created")
	}
}

// TestMissingCredentialsIsNotSent checks that a level-2 call with no
// credentials fails before reaching the network, and says so — a caller
// reconciling a failed write needs to know nothing was sent.
func TestMissingCredentialsIsNotSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a request with no credentials was sent")
	}))
	t.Cleanup(srv.Close)

	s := NewSession(srv.URL)
	err := s.PostL2(context.Background(), "/order", map[string]string{}, nil)
	if err == nil {
		t.Fatal("a call with no credentials reported success")
	}
	if Indeterminate(err) {
		t.Error("a request that was never sent was reported as indeterminate")
	}
}

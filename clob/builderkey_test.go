// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
)

// testBuilderKey is a builder credential. Every value is invented; only the
// secret has to be well formed base64url, because the test recomputes the HMAC
// from it rather than trusting this package's own output.
var testBuilderKey = BuilderAPIKey{
	Key:        "builder-key-1",
	Secret:     "Y2xvYi1idWlsZGVyLXRlc3Qtc2VjcmV0LTMyYnl0ZXM=",
	Passphrase: "builder-passphrase-1",
}

// TestCreateBuilderAPIKeyDecodesTheWireNames pins the field spelling. The
// builder endpoints answer with "key", not the "apiKey" the level-1 handshake
// uses; decoding the wrong name yields a credential with an empty Key that
// looks fine until it is used.
func TestCreateBuilderAPIKeyDecodesTheWireNames(t *testing.T) {
	reply := `{"key":"builder-key-1",` +
		`"secret":"Y2xvYi1idWlsZGVyLXRlc3Qtc2VjcmV0LTMyYnl0ZXM=",` +
		`"passphrase":"builder-passphrase-1"}`
	ts := newTradingServer(t, reply)
	c := authedClient(t, ts.server.URL)

	got, err := c.CreateBuilderAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != testBuilderKey {
		t.Errorf("CreateBuilderAPIKey() = %+v, want %+v", got, testBuilderKey)
	}
	if ts.seen.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", ts.seen.Method)
	}
	if ts.seen.Headers.Get("POLY_SIGNATURE") == "" {
		t.Error("no level-2 signature: creating a builder key authenticates as the account")
	}
}

// TestBuilderAPIKeysListsWithoutSecrets pins the narrower listing shape. The
// listing returns no secret and no passphrase, and it keeps revoked keys.
func TestBuilderAPIKeysListsWithoutSecrets(t *testing.T) {
	reply := `[{"key":"live-key","createdAt":"2026-08-21T03:47:46.241822Z"},` +
		`{"key":"dead-key","createdAt":"2026-08-21T03:47:46.241822Z",` +
		`"revokedAt":"2026-08-21T03:47:47.159739Z"}]`
	ts := newTradingServer(t, reply)
	c := authedClient(t, ts.server.URL)

	got, err := c.BuilderAPIKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2", len(got))
	}
	if got[0].Revoked() {
		t.Error("live key reports revoked")
	}
	if !got[1].Revoked() {
		t.Error("revoked key reports live: a revoked key stays in the listing")
	}
	if got[1].RevokedAt != "2026-08-21T03:47:47.159739Z" {
		t.Errorf("RevokedAt = %q", got[1].RevokedAt)
	}
}

// TestRevokeBuilderAPIKeyUsesTheBuilderScheme pins the authentication the
// revoke takes. Production refuses the account's level-2 headers here, refuses
// the credential under the plain POLY_ names, and refuses both families sent
// together — so the request must carry the POLY_BUILDER_ headers and nothing
// else.
func TestRevokeBuilderAPIKeyUsesTheBuilderScheme(t *testing.T) {
	ts := newTradingServer(t, `"OK"`)
	c := authedClient(t, ts.server.URL)

	if err := c.RevokeBuilderAPIKey(context.Background(), testBuilderKey); err != nil {
		t.Fatal(err)
	}
	if ts.seen.Method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", ts.seen.Method)
	}
	if got := ts.seen.Headers.Get("POLY_BUILDER_API_KEY"); got != testBuilderKey.Key {
		t.Errorf("POLY_BUILDER_API_KEY = %q, want %q", got, testBuilderKey.Key)
	}
	if got := ts.seen.Headers.Get("POLY_BUILDER_PASSPHRASE"); got != testBuilderKey.Passphrase {
		t.Errorf("POLY_BUILDER_PASSPHRASE = %q", got)
	}
	for _, h := range []string{"POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE", "POLY_ADDRESS"} {
		if got := ts.seen.Headers.Get(h); got != "" {
			t.Errorf("%s = %q, want none: the two schemes do not compose and sending both is a 401", h, got)
		}
	}

	// The signature is the level-2 construction under other names: the
	// timestamp sent must be the timestamp signed, over method and bare path
	// with an empty body.
	ts2 := ts.seen.Headers.Get("POLY_BUILDER_TIMESTAMP")
	if ts2 == "" {
		t.Fatal("no POLY_BUILDER_TIMESTAMP")
	}
	secret, err := base64.URLEncoding.DecodeString(testBuilderKey.Secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts2 + http.MethodDelete + epRevokeBuilderKey))
	want := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if got := ts.seen.Headers.Get("POLY_BUILDER_SIGNATURE"); got != want {
		t.Errorf("POLY_BUILDER_SIGNATURE = %q, want %q", got, want)
	}
}

// TestRevokeBuilderAPIKeyRejectsAnIncompleteCredential checks the guard. A
// listing entry has no secret, so passing one back would sign with an empty
// key and authenticate as nobody.
func TestRevokeBuilderAPIKeyRejectsAnIncompleteCredential(t *testing.T) {
	ts := newTradingServer(t, `"OK"`)
	c := authedClient(t, ts.server.URL)

	err := c.RevokeBuilderAPIKey(context.Background(), BuilderAPIKey{Key: "only-a-key"})
	if err == nil {
		t.Fatal("revoking with no secret succeeded")
	}
	if ts.seen.Method != "" {
		t.Error("an incomplete credential still reached the network")
	}
}

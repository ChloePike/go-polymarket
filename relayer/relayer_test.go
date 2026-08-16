// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package relayer

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

// ---------------------------------------------------------------------------
// Fixtures.

// transactionJSON is one Transaction as the relayer's own specification
// documents it, with all fifteen fields present. Every value is a JSON
// string: the relayer sends no numbers here, not even for nonce.
//
// It is transcribed from the specification rather than captured live. Reading
// a transaction needs an id, and an id only exists once a transaction has been
// submitted, which this package deliberately cannot do.
const transactionJSON = `{
	"transactionID": "0190b317-a1d3-7bec-9b91-eeb6dcd3a620",
	"transactionHash": "0x8f1ab2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e4f50617283949a5b6c7d8e",
	"from": "0x77837466dd64fb52ECD00C737F060d0ff5CCB575",
	"to": "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
	"proxyAddress": "0x6d8c4e9aDF5748Af82Dabe2C6225207770d6B4fa",
	"data": "0x",
	"nonce": "60",
	"value": "",
	"signature": "0x2b1c9f2d3e4a5b6c7d8e9f0a1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e42b1c9f2d3e4a5b6c7d8e9f0a1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e41b",
	"state": "STATE_CONFIRMED",
	"type": "SAFE",
	"owner": "0x77837466dd64fb52ECD00C737F060d0ff5CCB575",
	"metadata": "",
	"createdAt": "2024-07-14T21:13:08.819782Z",
	"updatedAt": "2024-07-14T21:13:46.576639Z"
}`

// apiKeyJSON is one APIKey as the relayer documents it. The key is a made-up
// UUID; no real credential appears in this repository.
const apiKeyJSON = `{
	"apiKey": "01967c03-b8c8-7000-8f68-8b8eaec6fd3d",
	"address": "0x77837466dd64fb52ECD00C737F060d0ff5CCB575",
	"createdAt": "2026-02-24T18:20:11.237485Z",
	"updatedAt": "2026-02-24T18:20:11.237485Z"
}`

// testAPIKey is the static credential pair the authentication tests send.
var testAPIKey = APIKeyCredentials{
	Key:     "01967c03-b8c8-7000-8f68-8b8eaec6fd3d",
	Address: "0x77837466dd64fb52ECD00C737F060d0ff5CCB575",
}

// builderSecretBytes is the raw secret behind testBuilder. The tests HMAC
// with these bytes directly, so a signature is checked against an
// independently computed one rather than against this package's own output.
var builderSecretBytes = []byte("relayer-builder-test-secret")

// testBuilder is the builder credential set the signing tests use.
var testBuilder = BuilderCredentials{
	Key:        "builder-key-1",
	Secret:     base64.StdEncoding.EncodeToString(builderSecretBytes),
	Passphrase: "builder-passphrase-1",
}

// ---------------------------------------------------------------------------
// Helpers.

// relayerServer starts an httptest.Server and returns a Client pointed at it,
// mirroring gamma_test.go's gammaServer.
func relayerServer(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(append([]Option{WithHost(srv.URL)}, opts...)...)
}

// credentialHeaders are every header either relayer credential scheme sends.
var credentialHeaders = []string{
	headerAPIKey,
	headerAPIKeyAddress,
	headerBuilderAPIKey,
	headerBuilderPassphrase,
	headerBuilderSignature,
	headerBuilderTimestamp,
}

// checkNoCredentials fails if a request carries any credential header. The
// public relayer endpoints need none, and a static API key sent where it is
// not needed is a secret spent for nothing.
func checkNoCredentials(t *testing.T, r *http.Request) {
	t.Helper()
	for _, k := range credentialHeaders {
		if r.Header.Get(k) != "" {
			t.Errorf("request carries credential header %s, want none", k)
		}
	}
}

func checkGet(t *testing.T, r *http.Request, path string) {
	t.Helper()
	if r.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", r.Method)
	}
	if r.URL.Path != path {
		t.Errorf("path = %s, want %s", r.URL.Path, path)
	}
}

func checkQuery(t *testing.T, got url.Values, want url.Values) {
	t.Helper()
	for k, v := range want {
		if got.Get(k) != v[0] {
			t.Errorf("query %s = %q, want %q", k, got.Get(k), v[0])
		}
	}
	if len(got) != len(want) {
		t.Errorf("query = %v, want exactly %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Public endpoints.

func TestNonce(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoCredentials(t, r)
		checkGet(t, r, epNonce)
		checkQuery(t, r.URL.Query(), url.Values{
			"address": {"0x6e0c80c90ea6c15917308F820Eac91Ce2724B5b5"},
			"type":    {"PROXY"},
		})
		w.Write([]byte(`{"nonce":"31"}`))
	})
	got, err := c.Nonce(context.Background(), "0x6e0c80c90ea6c15917308F820Eac91Ce2724B5b5", WalletTypeProxy)
	if err != nil {
		t.Fatal(err)
	}
	if got != "31" {
		t.Errorf("Nonce() = %q, want the decimal string 31", got)
	}
}

// walletQueryCase is one wallet type and the query value it must send. The
// deposit wallet is the case that matters: it is absent from the published
// enum but the relayer answers it.
type walletQueryCase struct {
	Name   string
	Wallet WalletType
	Want   string
}

var walletQueryCases = []walletQueryCase{
	{Name: "proxy", Wallet: WalletTypeProxy, Want: "PROXY"},
	{Name: "safe", Wallet: WalletTypeSafe, Want: "SAFE"},
	{Name: "wallet", Wallet: WalletTypeWallet, Want: "WALLET"},
}

func TestNonceSendsEveryWalletType(t *testing.T) {
	for _, tc := range walletQueryCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("type"); got != tc.Want {
					t.Errorf("type = %q, want %q", got, tc.Want)
				}
				w.Write([]byte(`{"nonce":"0"}`))
			})
			if _, err := c.Nonce(context.Background(), "0xabc", tc.Wallet); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRelayPayload(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoCredentials(t, r)
		checkGet(t, r, epRelayPayload)
		checkQuery(t, r.URL.Query(), url.Values{
			"address": {"0x6e0c80c90ea6c15917308F820Eac91Ce2724B5b5"},
			"type":    {"SAFE"},
		})
		w.Write([]byte(`{"address":"0x0f985accdbed00544fd6f8db31c9a47345c5d4e2","nonce":"494"}`))
	})
	got, err := c.RelayPayload(context.Background(),
		"0x6e0c80c90ea6c15917308F820Eac91Ce2724B5b5", WalletTypeSafe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != "0x0f985accdbed00544fd6f8db31c9a47345c5d4e2" {
		t.Errorf("Address = %q, want the relayer's SAFE address", got.Address)
	}
	if got.Nonce != "494" {
		t.Errorf("Nonce = %q, want 494", got.Nonce)
	}
}

func TestDeployed(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoCredentials(t, r)
		checkGet(t, r, epDeployed)
		checkQuery(t, r.URL.Query(), url.Values{
			"address": {"0x6d8c4e9aDF5748Af82Dabe2C6225207770d6B4fa"},
			"type":    {"WALLET"},
		})
		w.Write([]byte(`{"deployed":true}`))
	})
	got, err := c.Deployed(context.Background(),
		"0x6d8c4e9aDF5748Af82Dabe2C6225207770d6B4fa", WalletTypeWallet)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("Deployed() = false, want true")
	}
}

// TestDeployedOmitsEmptyWalletType checks that an empty wallet type leaves the
// parameter out altogether. Sending type= is not the same as sending nothing:
// the relayer defaults an absent type to SAFE, and never rejects a value it
// does not recognise.
func TestDeployedOmitsEmptyWalletType(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkQuery(t, r.URL.Query(), url.Values{"address": {"0xabc"}})
		w.Write([]byte(`{"deployed":false}`))
	})
	got, err := c.Deployed(context.Background(), "0xabc", "")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("Deployed() = true, want false")
	}
}

func TestTransaction(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoCredentials(t, r)
		checkGet(t, r, epTransaction)
		checkQuery(t, r.URL.Query(), url.Values{"id": {"0190b317-a1d3-7bec-9b91-eeb6dcd3a620"}})
		w.Write([]byte("[" + transactionJSON + "]"))
	})
	got, err := c.Transaction(context.Background(), "0190b317-a1d3-7bec-9b91-eeb6dcd3a620")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: the relayer answers with an array", len(got))
	}
	if got[0].TransactionID != "0190b317-a1d3-7bec-9b91-eeb6dcd3a620" {
		t.Errorf("TransactionID = %q", got[0].TransactionID)
	}
}

// TestTransactionTranscription decodes the specified Transaction shape with
// unknown fields refused, so a misspelled or missing json tag fails here.
func TestTransactionTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(transactionJSON))
	dec.DisallowUnknownFields()
	var tx Transaction
	if err := dec.Decode(&tx); err != nil {
		t.Fatalf("decoding the Transaction fixture: %v", err)
	}
	if tx.State != TransactionStateConfirmed {
		t.Errorf("State = %q, want %q", tx.State, TransactionStateConfirmed)
	}
	if tx.Type != WalletTypeSafe {
		t.Errorf("Type = %q, want %q", tx.Type, WalletTypeSafe)
	}
	// Nonce and Value are uint256 amounts and stay the strings the wire
	// carries. Value's documented example really is the empty string.
	if tx.Nonce != "60" {
		t.Errorf("Nonce = %q, want the decimal string 60", tx.Nonce)
	}
	if tx.Value != "" {
		t.Errorf("Value = %q, want the empty string", tx.Value)
	}
	if tx.CreatedAt != "2024-07-14T21:13:08.819782Z" {
		t.Errorf("CreatedAt = %q", tx.CreatedAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, tx.UpdatedAt); err != nil {
		t.Errorf("UpdatedAt %q does not parse as RFC 3339: %v", tx.UpdatedAt, err)
	}
	if tx.Owner == "" || tx.Signature == "" {
		t.Error("Owner and Signature must decode: the specification carries both")
	}
}

// ---------------------------------------------------------------------------
// Authenticated endpoints.

func TestTransactionsSendsAPIKeyHeaders(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkGet(t, r, epTransactions)
		if len(r.URL.Query()) != 0 {
			t.Errorf("query = %v, want none: the relayer takes no parameters here", r.URL.Query())
		}
		if got := r.Header.Get(headerAPIKey); got != testAPIKey.Key {
			t.Errorf("%s = %q, want %q", headerAPIKey, got, testAPIKey.Key)
		}
		if got := r.Header.Get(headerAPIKeyAddress); got != testAPIKey.Address {
			t.Errorf("%s = %q, want %q", headerAPIKeyAddress, got, testAPIKey.Address)
		}
		if got := r.Header.Get(headerBuilderAPIKey); got != "" {
			t.Errorf("%s = %q, want none: the two schemes are not mixed", headerBuilderAPIKey, got)
		}
		w.Write([]byte("[" + transactionJSON + "]"))
	}, WithAPIKey(testAPIKey))

	got, err := c.Transactions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

// TestTransactionsSendsBuilderHeaders checks the four builder headers, and
// checks the signature by recomputing it from the timestamp that was actually
// sent: the value signed and the value sent must be the same, or the relayer
// rejects the request.
func TestTransactionsSendsBuilderHeaders(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerBuilderAPIKey); got != testBuilder.Key {
			t.Errorf("%s = %q, want %q", headerBuilderAPIKey, got, testBuilder.Key)
		}
		if got := r.Header.Get(headerBuilderPassphrase); got != testBuilder.Passphrase {
			t.Errorf("%s = %q, want %q", headerBuilderPassphrase, got, testBuilder.Passphrase)
		}
		ts := r.Header.Get(headerBuilderTimestamp)
		secs, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			t.Fatalf("%s = %q, want unix seconds: %v", headerBuilderTimestamp, ts, err)
		}
		// Unix seconds, not milliseconds. A millisecond timestamp is a
		// thousand times too large and fails as clock skew, not as a format
		// error, so check the magnitude.
		if skew := time.Since(time.Unix(secs, 0)); skew < -time.Minute || skew > time.Minute {
			t.Errorf("%s = %q, off by %v from now", headerBuilderTimestamp, ts, skew)
		}

		mac := hmac.New(sha256.New, builderSecretBytes)
		mac.Write([]byte(ts + http.MethodGet + epTransactions))
		want := base64.URLEncoding.EncodeToString(mac.Sum(nil))
		if got := r.Header.Get(headerBuilderSignature); got != want {
			t.Errorf("%s = %q, want %q", headerBuilderSignature, got, want)
		}
		if got := r.Header.Get(headerAPIKey); got != "" {
			t.Errorf("%s = %q, want none", headerAPIKey, got)
		}
		w.Write([]byte("[]"))
	}, WithBuilderCredentials(testBuilder))

	if _, err := c.Transactions(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestBuildBuilderHeaders pins the signature construction against an
// independently computed HMAC at a fixed timestamp. The message is the
// timestamp, the method and the bare path run together with no separator, the
// key is the base64-decoded secret, and the encoding is padded base64url. The
// passphrase is sent but never signed.
func TestBuildBuilderHeaders(t *testing.T) {
	const ts = "1755000000"
	h, err := BuildBuilderHeaders(testBuilder, ts, http.MethodGet, epTransactions, "")
	if err != nil {
		t.Fatal(err)
	}

	mac := hmac.New(sha256.New, builderSecretBytes)
	mac.Write([]byte("1755000000GET/transactions"))
	want := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if h.Signature != want {
		t.Errorf("Signature = %q, want %q", h.Signature, want)
	}
	// A 32-byte digest is 44 padded base64 characters. The relayer keeps the
	// padding, so the raw (unpadded) encoding is the wrong one.
	if len(h.Signature) != 44 || !strings.HasSuffix(h.Signature, "=") {
		t.Errorf("Signature = %q, want 44 characters ending in the base64 pad", h.Signature)
	}
	if strings.ContainsAny(h.Signature, "+/") {
		t.Errorf("Signature = %q, want the url-safe alphabet", h.Signature)
	}
	if h.Timestamp != ts {
		t.Errorf("Timestamp = %q, want %q", h.Timestamp, ts)
	}

	// The signed message is the bare path. A query string appended to it
	// changes the signature, which is why every caller must pass the path
	// alone and never the request's full target.
	withQuery, err := BuildBuilderHeaders(testBuilder, ts, http.MethodGet, epTransactions+"?limit=1", "")
	if err != nil {
		t.Fatal(err)
	}
	if withQuery.Signature == h.Signature {
		t.Error("a query string did not change the signature: the path is not being signed")
	}

	got := h.Header()
	if got[headerBuilderPassphrase] != testBuilder.Passphrase {
		t.Errorf("passphrase header = %q", got[headerBuilderPassphrase])
	}
	if len(got) != 4 {
		t.Errorf("Header() = %v, want all four builder headers", got)
	}
}

func TestAPIKeys(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkGet(t, r, epAPIKeys)
		if got := r.Header.Get(headerAPIKey); got != testAPIKey.Key {
			t.Errorf("%s = %q, want %q", headerAPIKey, got, testAPIKey.Key)
		}
		w.Write([]byte("[" + apiKeyJSON + "]"))
	}, WithAPIKey(testAPIKey))

	got, err := c.APIKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Key != testAPIKey.Key || got[0].Address != testAPIKey.Address {
		t.Errorf("APIKey = %+v", got[0])
	}
	if got[0].Credentials() != testAPIKey {
		t.Errorf("Credentials() = %+v, want %+v", got[0].Credentials(), testAPIKey)
	}
}

func TestAPIKeysTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(apiKeyJSON))
	dec.DisallowUnknownFields()
	var k APIKey
	if err := dec.Decode(&k); err != nil {
		t.Fatalf("decoding the APIKey fixture: %v", err)
	}
	if k.CreatedAt != "2026-02-24T18:20:11.237485Z" {
		t.Errorf("CreatedAt = %q", k.CreatedAt)
	}
}

// TestPublicCallsSendNoCredentials checks that a client holding an API key
// still sends nothing on the public endpoints. The key is a bearer token, and
// an endpoint that does not read it has no business receiving it.
func TestPublicCallsSendNoCredentials(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoCredentials(t, r)
		switch r.URL.Path {
		case epDeployed:
			w.Write([]byte(`{"deployed":true}`))
		case epTransaction:
			w.Write([]byte("[]"))
		default:
			w.Write([]byte(`{"nonce":"1","address":"0x0"}`))
		}
	}, WithAPIKey(testAPIKey))

	ctx := context.Background()
	if _, err := c.Nonce(ctx, "0xabc", WalletTypeSafe); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RelayPayload(ctx, "0xabc", WalletTypeSafe); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Deployed(ctx, "0xabc", WalletTypeSafe); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Transaction(ctx, "an-id"); err != nil {
		t.Fatal(err)
	}
}

// TestAuthenticatedCallsNeedCredentials checks that a client with none fails
// before sending anything, and that the failure is reported as determinate:
// nothing reached the relayer, so nothing there changed.
func TestAuthenticatedCallsNeedCredentials(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent to %s, want none", r.URL.Path)
	})

	for _, err := range []error{
		second(c.Transactions(context.Background())),
		second(c.APIKeys(context.Background())),
	} {
		if !errors.Is(err, ErrNoCredentials) {
			t.Errorf("error = %v, want ErrNoCredentials", err)
		}
		if !errors.Is(err, polymarket.ErrNoCredentials) {
			t.Errorf("error = %v, want it to wrap polymarket.ErrNoCredentials", err)
		}
		if polymarket.Indeterminate(err) {
			t.Errorf("Indeterminate(%v) = true, want false: the request was never sent", err)
		}
	}
}

// second discards a call's value and keeps its error, so the two calls above
// can be checked with one body.
func second[T any](_ T, err error) error { return err }

// ---------------------------------------------------------------------------
// Errors and construction.

func TestErrorStatusBecomesPolymarketError(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid type"}`))
	})
	_, err := c.Nonce(context.Background(), "0xabc", "proxy")
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	var apiErr *polymarket.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "invalid type" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "invalid type")
	}
}

func TestUnauthorizedIsAnError(t *testing.T) {
	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid authorization"}`))
	}, WithAPIKey(testAPIKey))

	_, err := c.Transactions(context.Background())
	var apiErr *polymarket.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestCredentialsRefuseIncompletePairs(t *testing.T) {
	if _, err := (APIKeyCredentials{Key: "k"}).AuthHeaders(http.MethodGet, epTransactions, ""); err == nil {
		t.Error("an API key with no address was accepted")
	}
	if _, err := BuildBuilderHeaders(BuilderCredentials{Key: "k"}, "1", http.MethodGet, epTransactions, ""); err == nil {
		t.Error("builder credentials with no secret were accepted")
	}
}

// countingTransport records how many requests passed through it.
type countingTransport struct {
	base  http.RoundTripper
	calls int
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return t.base.RoundTrip(req)
}

// TestWithHTTPClientIsNotModified checks that supplying an http.Client keeps
// that client's transport in the chain and leaves the caller's own client
// untouched, so a client shared with something else never starts sending
// relayer credentials.
func TestWithHTTPClientIsNotModified(t *testing.T) {
	tr := &countingTransport{base: http.DefaultTransport}
	caller := &http.Client{Transport: tr, Timeout: time.Second}

	c := relayerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerAPIKey); got != testAPIKey.Key {
			t.Errorf("%s = %q, want the key", headerAPIKey, got)
		}
		w.Write([]byte("[]"))
	}, WithHTTPClient(caller), WithAPIKey(testAPIKey))

	if _, err := c.Transactions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.calls != 1 {
		t.Errorf("caller transport saw %d requests, want 1", tr.calls)
	}
	if caller.Transport != http.RoundTripper(tr) {
		t.Error("the caller's http.Client was modified")
	}
}

func TestNewAndNewWithSession(t *testing.T) {
	c := New(WithHost("https://example.invalid"))
	if c == nil || c.session == nil {
		t.Fatal("New() returned a client with no session")
	}
	if c.session.Host() != "https://example.invalid" {
		t.Errorf("Host() = %q", c.session.Host())
	}

	s := polymarket.NewSession(polymarket.RelayerHost)
	c2 := NewWithSession(s)
	if c2.session != s {
		t.Error("NewWithSession() did not adopt the given session")
	}
	// A shared session carries no credential transport, so the
	// authenticated endpoints say so rather than sending an unsigned
	// request and reporting the relayer's 401.
	if _, err := c2.Transactions(context.Background()); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("error = %v, want ErrNoCredentials", err)
	}
}

func TestNewDefaultsToRelayerHost(t *testing.T) {
	if got := New().session.Host(); got != polymarket.RelayerHost {
		t.Errorf("Host() = %q, want %q", got, polymarket.RelayerHost)
	}
}

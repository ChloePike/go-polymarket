// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// The well-known Hardhat development key: public, holds nothing, and used
// here only so signatures are reproducible.
const testPrivateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

const testTokenID = "71321045679252212594626385532706912750332728571942532289631379312455583992563"

// capturedRequest is what a test server saw: enough to assert the method, the
// path, the level-2 headers and the exact bytes that were signed.
type capturedRequest struct {
	Method  string
	Path    string
	RawPath string
	Headers http.Header
	Body    []byte
}

// tradingServer is a test server that records one request and replies with a
// canned body.
type tradingServer struct {
	server *httptest.Server
	seen   *capturedRequest
}

// newTradingServer returns a server that records every request and answers
// each with reply.
func newTradingServer(t *testing.T, reply string) *tradingServer {
	t.Helper()
	ts := &tradingServer{seen: &capturedRequest{}}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*ts.seen = capturedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			RawPath: r.URL.RequestURI(),
			Headers: r.Header.Clone(),
			Body:    body,
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	}))
	t.Cleanup(ts.server.Close)
	return ts
}

// authedClient returns a client with a wallet and credentials, pointed at the
// test server.
func authedClient(t *testing.T, host string) *Client {
	t.Helper()
	key, err := polymarket.NewPrivateKey(testPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return New(
		WithHost(host),
		WithSigner(key),
		WithCredentials(polymarket.APICreds{
			Key:        "api-key-1",
			Secret:     "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM=",
			Passphrase: "passphrase-1",
		}),
	)
}

// signedTestOrder builds and signs an order, optionally attributed to a
// builder code.
func signedTestOrder(t *testing.T, builderCode string) polymarket.SignedOrder {
	t.Helper()
	key, err := polymarket.NewPrivateKey(testPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	opts := polymarket.OrderOptions{TickSize: "0.01", Salt: 479249096354, Timestamp: 1740000000000}
	order, err := polymarket.BuildOrder(polymarket.UserOrder{
		TokenID:     testTokenID,
		Price:       "0.52",
		Size:        "100",
		Side:        polymarket.Buy,
		BuilderCode: builderCode,
	}, key.Address(), opts)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := polymarket.SignOrder(order, polymarket.ChainPolygon, opts, key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// wireOrderJSON is the order object as it appears in a submission body, read
// back loosely so a test can assert the JSON types as well as the values.
type wireOrderJSON struct {
	Salt          json.Number `json:"salt"`
	Maker         string      `json:"maker"`
	Signer        string      `json:"signer"`
	Taker         string      `json:"taker"`
	TokenID       string      `json:"tokenId"`
	MakerAmount   string      `json:"makerAmount"`
	TakerAmount   string      `json:"takerAmount"`
	Side          string      `json:"side"`
	SignatureType int         `json:"signatureType"`
	Timestamp     string      `json:"timestamp"`
	Expiration    string      `json:"expiration"`
	Metadata      string      `json:"metadata"`
	Builder       string      `json:"builder"`
	Signature     string      `json:"signature"`
}

// submissionJSON is the whole POST /order body.
type submissionJSON struct {
	DeferExec bool          `json:"deferExec"`
	PostOnly  bool          `json:"postOnly"`
	Order     wireOrderJSON `json:"order"`
	Owner     string        `json:"owner"`
	OrderType string        `json:"orderType"`
}

const okOrderReply = `{"success":true,"orderID":"0xorder","status":"matched"}`

// TestPostOrderCarriesBuilderCode is the test that protects the revenue path.
// A builder code earns a share of the fee only if it reaches the exchange, and
// it is signed, so dropping it does not merely lose the attribution — it makes
// the signature cover a different order than the one submitted.
func TestPostOrderCarriesBuilderCode(t *testing.T) {
	ts := newTradingServer(t, okOrderReply)
	c := authedClient(t, ts.server.URL)

	order := signedTestOrder(t, testBuilderCode)
	if order.Builder != testBuilderCode {
		t.Fatalf("the signed order lost the builder code before submission: %q", order.Builder)
	}

	if _, err := c.PostOrder(context.Background(), order, polymarket.GTC, SubmitOptions{}); err != nil {
		t.Fatal(err)
	}

	var got submissionJSON
	if err := json.Unmarshal(ts.seen.Body, &got); err != nil {
		t.Fatalf("submission body is not the expected shape: %v (body %s)", err, ts.seen.Body)
	}
	if got.Order.Builder != testBuilderCode {
		t.Errorf("wire builder = %q, want %q", got.Order.Builder, testBuilderCode)
	}
	if got.Order.Signature != order.Signature {
		t.Errorf("wire signature = %q, want %q", got.Order.Signature, order.Signature)
	}
	// The body the HMAC signed must be the body that was sent, byte for byte.
	if !strings.Contains(string(ts.seen.Body), testBuilderCode) {
		t.Error("the builder code is absent from the raw submitted bytes")
	}
}

// TestBuilderCodeIsSigned pins the security property behind attribution: the
// builder field is inside the signature, so a relay cannot re-attribute an
// order to itself without invalidating it.
func TestBuilderCodeIsSigned(t *testing.T) {
	attributed := signedTestOrder(t, testBuilderCode)
	plain := signedTestOrder(t, "")

	if plain.Builder != polymarket.ZeroBytes32 {
		t.Errorf("an unattributed order carries builder %q, want the zero bytes32", plain.Builder)
	}
	if attributed.Signature == plain.Signature {
		t.Fatal("the builder code does not change the signature, so it is not covered by it")
	}

	opts := polymarket.OrderOptions{TickSize: "0.01"}
	a, err := polymarket.OrderDigest(attributed.Order, polymarket.ChainPolygon, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := polymarket.OrderDigest(plain.Order, polymarket.ChainPolygon, opts)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("the builder code does not change the digest")
	}

	// Swapping the code after signing must break verification, which is what
	// makes attribution binding rather than advisory.
	swapped := attributed.Order
	swapped.Builder = "0x2222222222222222222222222222222222222222222222222222222222222222"
	c, err := polymarket.OrderDigest(swapped, polymarket.ChainPolygon, opts)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("re-attributing a signed order left its digest unchanged")
	}
}

// TestPostOrderRejectsMalformedBuilderCode checks that a builder code which is
// not a bytes32 fails before anything is sent, rather than being padded or
// truncated into a different code.
func TestPostOrderRejectsMalformedBuilderCode(t *testing.T) {
	key, err := polymarket.NewPrivateKey(testPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	opts := polymarket.OrderOptions{TickSize: "0.01", Salt: 1, Timestamp: 1}
	_, err = polymarket.BuildOrder(polymarket.UserOrder{
		TokenID:     testTokenID,
		Price:       "0.52",
		Size:        "100",
		Side:        polymarket.Buy,
		BuilderCode: "0xdeadbeef", // four bytes, not thirty-two
	}, key.Address(), opts)
	if err == nil {
		t.Fatal("a four-byte builder code was accepted; it must not be padded into a valid one")
	}
	if !strings.Contains(err.Error(), "builder") {
		t.Errorf("error %q does not name the builder code", err)
	}
}

// TestPostOrderWireShape pins the parts of the submission body that are easy
// to get wrong and impossible to notice.
func TestPostOrderWireShape(t *testing.T) {
	ts := newTradingServer(t, okOrderReply)
	c := authedClient(t, ts.server.URL)

	order := signedTestOrder(t, testBuilderCode)
	if _, err := c.PostOrder(context.Background(), order, polymarket.GTC,
		SubmitOptions{PostOnly: true, DeferExec: true}); err != nil {
		t.Fatal(err)
	}

	if ts.seen.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", ts.seen.Method)
	}
	if ts.seen.Path != "/order" {
		t.Errorf("path = %s, want /order", ts.seen.Path)
	}

	// salt is a JSON number while every other numeric field is a string.
	// json.Number preserves the literal, so this catches a salt quoted by
	// mistake as well as one that lost precision.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(ts.seen.Body, &raw); err != nil {
		t.Fatal(err)
	}
	var orderRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["order"], &orderRaw); err != nil {
		t.Fatal(err)
	}
	if s := string(orderRaw["salt"]); strings.HasPrefix(s, `"`) {
		t.Errorf("salt is sent as a string (%s); the wire format is a number", s)
	}
	if s := string(orderRaw["makerAmount"]); !strings.HasPrefix(s, `"`) {
		t.Errorf("makerAmount is sent as a number (%s); the wire format is a string", s)
	}

	var got submissionJSON
	if err := json.Unmarshal(ts.seen.Body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Owner != "api-key-1" {
		t.Errorf("owner = %q, want the api key", got.Owner)
	}
	if got.OrderType != "GTC" {
		t.Errorf("orderType = %q, want GTC", got.OrderType)
	}
	if !got.PostOnly || !got.DeferExec {
		t.Errorf("submit options lost: postOnly=%v deferExec=%v", got.PostOnly, got.DeferExec)
	}
	// taker and expiration ride on the wire even though neither is signed.
	if got.Order.Taker != polymarket.ZeroAddress {
		t.Errorf("taker = %q, want the zero address", got.Order.Taker)
	}
	if got.Order.Expiration != "0" {
		t.Errorf("expiration = %q, want \"0\"", got.Order.Expiration)
	}
	if got.Order.Salt.String() != order.Salt {
		t.Errorf("salt = %s, want %s", got.Order.Salt, order.Salt)
	}
}

// TestPostOrderAuth checks that a submission is signed at level 2 and that the
// signature covers the path without the query.
func TestPostOrderAuth(t *testing.T) {
	ts := newTradingServer(t, okOrderReply)
	c := authedClient(t, ts.server.URL)

	if _, err := c.PostOrder(context.Background(), signedTestOrder(t, ""),
		polymarket.GTC, SubmitOptions{}); err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE", "POLY_TIMESTAMP"} {
		if ts.seen.Headers.Get(h) == "" {
			t.Errorf("header %s is missing", h)
		}
	}
	if got := ts.seen.Headers.Get("POLY_API_KEY"); got != "api-key-1" {
		t.Errorf("POLY_API_KEY = %q, want api-key-1", got)
	}

	// Recompute the HMAC over method + bare path + body and compare.
	want, err := polymarket.SignHMAC(
		"PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM=",
		ts.seen.Headers.Get("POLY_TIMESTAMP"),
		http.MethodPost, "/order", string(ts.seen.Body))
	if err != nil {
		t.Fatal(err)
	}
	if got := ts.seen.Headers.Get("POLY_SIGNATURE"); got != want {
		t.Errorf("POLY_SIGNATURE = %q, want %q (the HMAC covers method+path+body)", got, want)
	}
}

// postOnlyCase is one order type and whether post-only may accompany it.
type postOnlyCase struct {
	orderType polymarket.OrderType
	postOnly  bool
	wantErr   bool
}

// TestPostOnlyWithMarketOrders pins a combination the exchange always refuses:
// a fill-or-kill order exists to take liquidity, so it cannot also insist on
// making it.
func TestPostOnlyWithMarketOrders(t *testing.T) {
	cases := []postOnlyCase{
		{polymarket.GTC, true, false},
		{polymarket.GTD, true, false},
		{polymarket.FOK, true, true},
		{polymarket.FAK, true, true},
		{polymarket.FOK, false, false},
		{polymarket.FAK, false, false},
	}
	for _, tc := range cases {
		ts := newTradingServer(t, okOrderReply)
		c := authedClient(t, ts.server.URL)
		_, err := c.PostOrder(context.Background(), signedTestOrder(t, ""), tc.orderType,
			SubmitOptions{PostOnly: tc.postOnly})
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s with postOnly: got nil error", tc.orderType)
			}
			if len(ts.seen.Body) != 0 {
				t.Errorf("%s with postOnly: a request was sent anyway", tc.orderType)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s postOnly=%v: %v", tc.orderType, tc.postOnly, err)
		}
	}
}

// TestPostOrderRejectsUnsignedOrder checks that an order without a signature
// never reaches the wire; the exchange would refuse it, but sending it leaks
// the intended trade to anyone watching.
func TestPostOrderRejectsUnsignedOrder(t *testing.T) {
	ts := newTradingServer(t, okOrderReply)
	c := authedClient(t, ts.server.URL)

	order := signedTestOrder(t, "")
	order.Signature = ""
	if _, err := c.PostOrder(context.Background(), order, polymarket.GTC, SubmitOptions{}); err == nil {
		t.Fatal("an unsigned order was submitted")
	}
	if len(ts.seen.Body) != 0 {
		t.Error("an unsigned order was sent to the server")
	}
}

// TestPostOrdersBatch checks that a batch submission is a JSON array of the
// same per-order bodies, one per submission and in the caller's order.
func TestPostOrdersBatch(t *testing.T) {
	ts := newTradingServer(t, `[{"success":true,"orderID":"a"},{"success":true,"orderID":"b"}]`)
	c := authedClient(t, ts.server.URL)

	got, err := c.PostOrders(context.Background(), []OrderSubmission{
		{Order: signedTestOrder(t, testBuilderCode), OrderType: polymarket.GTC},
		{Order: signedTestOrder(t, ""), OrderType: polymarket.FAK},
	}, SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d responses, want 2", len(got))
	}

	var bodies []submissionJSON
	if err := json.Unmarshal(ts.seen.Body, &bodies); err != nil {
		t.Fatalf("batch body is not an array of submissions: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("sent %d orders, want 2", len(bodies))
	}
	if bodies[0].Order.Builder != testBuilderCode {
		t.Errorf("first order lost its builder code: %q", bodies[0].Order.Builder)
	}
	if bodies[1].Order.Builder != polymarket.ZeroBytes32 {
		t.Errorf("second order should be unattributed, got %q", bodies[1].Order.Builder)
	}
	if bodies[0].OrderType != "GTC" || bodies[1].OrderType != "FAK" {
		t.Errorf("order types not preserved: %q %q", bodies[0].OrderType, bodies[1].OrderType)
	}
}

// cancelCase is one cancel call and the request it must produce.
type cancelCase struct {
	name     string
	call     func(*Client) error
	wantPath string
	wantBody string
}

// TestCancelFamily pins the four cancel shapes. They differ in ways that are
// invisible until an order that should have been cancelled fills.
func TestCancelFamily(t *testing.T) {
	cases := []cancelCase{
		{
			name:     "one order",
			call:     func(c *Client) error { _, err := c.CancelOrder(context.Background(), "0xabc"); return err },
			wantPath: "/order",
			wantBody: `{"orderID":"0xabc"}`,
		},
		{
			name: "several orders",
			call: func(c *Client) error {
				_, err := c.CancelOrders(context.Background(), []string{"0xa", "0xb"})
				return err
			},
			wantPath: "/orders",
			wantBody: `["0xa","0xb"]`,
		},
		{
			name:     "everything",
			call:     func(c *Client) error { _, err := c.CancelAll(context.Background()); return err },
			wantPath: "/cancel-all",
			wantBody: "",
		},
		{
			name: "one market",
			call: func(c *Client) error {
				_, err := c.CancelMarketOrders(context.Background(), MarketCancelParams{Market: "0xm"})
				return err
			},
			wantPath: "/cancel-market-orders",
			wantBody: `{"market":"0xm"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTradingServer(t, `{"canceled":["0xabc"],"not_canceled":{}}`)
			c := authedClient(t, ts.server.URL)
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if ts.seen.Method != http.MethodDelete {
				t.Errorf("method = %s, want DELETE", ts.seen.Method)
			}
			if ts.seen.Path != tc.wantPath {
				t.Errorf("path = %s, want %s", ts.seen.Path, tc.wantPath)
			}
			if got := string(ts.seen.Body); got != tc.wantBody {
				t.Errorf("body = %s, want %s", got, tc.wantBody)
			}
		})
	}
}

// TestCancelMarketOrdersNeedsATarget checks that an empty filter is refused
// rather than sent, where the exchange might read it as "everything".
func TestCancelMarketOrdersNeedsATarget(t *testing.T) {
	ts := newTradingServer(t, `{}`)
	c := authedClient(t, ts.server.URL)
	if _, err := c.CancelMarketOrders(context.Background(), MarketCancelParams{}); err == nil {
		t.Fatal("an empty market filter was accepted")
	}
	if len(ts.seen.Body) != 0 || ts.seen.Method != "" {
		t.Error("an empty market filter was sent to the server")
	}
}

// TestOpenOrdersDecodesPaginatedObject pins the shape production actually
// serves. The official types call this a bare array; it is not, and decoding
// into a slice fails at runtime for every caller.
func TestOpenOrdersDecodesPaginatedObject(t *testing.T) {
	ts := newTradingServer(t, `{"data":[{"id":"0x1","status":"LIVE","side":"BUY","original_size":"100",
		"size_matched":"0","price":"0.52","asset_id":"`+testTokenID+`"}],
		"next_cursor":"LTE=","limit":500,"count":1}`)
	c := authedClient(t, ts.server.URL)

	orders, page, err := c.OpenOrders(context.Background(), OpenOrderParams{Market: "0xm"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ID != "0x1" {
		t.Fatalf("orders = %+v, want one order 0x1", orders)
	}
	if orders[0].Side != polymarket.Buy {
		t.Errorf("side = %q, want BUY", orders[0].Side)
	}
	if page.NextCursor != polymarket.CursorEnd || page.Count != 1 {
		t.Errorf("pagination = %+v", page)
	}
	// The filter and the seeded cursor both belong in the query, not the body.
	if !strings.Contains(ts.seen.RawPath, "market=0xm") {
		t.Errorf("query = %s, want the market filter", ts.seen.RawPath)
	}
	if !strings.Contains(ts.seen.RawPath, "next_cursor=") {
		t.Errorf("query = %s, want a seeded cursor", ts.seen.RawPath)
	}
}

// TestBalanceAllowanceQuery checks the parameters that decide which balance is
// reported, including the signature type the exchange keys the wallet by.
func TestBalanceAllowanceQuery(t *testing.T) {
	ts := newTradingServer(t, `{"balance":"1000000","allowances":{"0xexchange":"1000000"}}`)
	c := authedClient(t, ts.server.URL)

	got, err := c.BalanceAllowance(context.Background(), BalanceAllowanceParams{
		AssetType: Conditional,
		TokenID:   testTokenID,
	}, polymarket.SigPolyProxy)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != "1000000" {
		t.Errorf("balance = %q", got.Balance)
	}
	for _, want := range []string{"asset_type=CONDITIONAL", "token_id=" + testTokenID, "signature_type=1"} {
		if !strings.Contains(ts.seen.RawPath, want) {
			t.Errorf("query %s is missing %s", ts.seen.RawPath, want)
		}
	}
}

// TestCreateOrDeriveAPIKeyAdoptsCredentials checks the handshake stores what it
// obtains, so the caller does not have to.
func TestCreateOrDeriveAPIKeyAdoptsCredentials(t *testing.T) {
	ts := newTradingServer(t, `{"apiKey":"k","secret":"c2VjcmV0","passphrase":"p"}`)
	key, err := polymarket.NewPrivateKey(testPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	c := New(WithHost(ts.server.URL), WithSigner(key))

	creds, err := c.CreateOrDeriveAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.Key != "k" {
		t.Errorf("key = %q, want k", creds.Key)
	}
	if ts.seen.Headers.Get("POLY_SIGNATURE") == "" || ts.seen.Headers.Get("POLY_NONCE") != "0" {
		t.Errorf("level-1 headers missing: %v", ts.seen.Headers)
	}
	// A level-2 call must now work without the caller passing anything.
	if _, _, err := c.OpenOrders(context.Background(), OpenOrderParams{}, ""); err != nil {
		t.Errorf("credentials were not adopted: %v", err)
	}
}

// TestTradingNeedsCredentials checks that a level-2 call without credentials
// fails locally rather than sending an unauthenticated request.
func TestTradingNeedsCredentials(t *testing.T) {
	ts := newTradingServer(t, `{}`)
	c := New(WithHost(ts.server.URL))

	if _, err := c.PostOrder(context.Background(), signedTestOrder(t, ""),
		polymarket.GTC, SubmitOptions{}); err == nil {
		t.Fatal("a submission without credentials was accepted")
	}
	if len(ts.seen.Body) != 0 {
		t.Error("an unauthenticated submission was sent")
	}
}

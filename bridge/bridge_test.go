// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// ---------------------------------------------------------------------------
// Test fixtures. The two GET fixtures are live responses from
// https://bridge.polymarket.com, captured 2026-08-16 and compacted, so the
// transcription tests below decode the real wire shape rather than a
// hand-written guess. The three POST fixtures are hand-built from the
// published schema: rule 8 forbids a live write, and all three endpoints are
// POSTs.

// supportedAssetsJSON is six rows of a live GET /supported-assets response,
// chosen to cover every address form the field takes: EVM hex, the native
// sentinel on three different chains, a Solana base58 mint, and a Tron base58
// contract. It carries the undocumented top-level note too.
const supportedAssetsJSON = `{"supportedAssets":[{"chainId":"1","chainName":"Ethereum","token":{"name":"TrueUSD","symbol":"TUSD","address":"0x0000000000085d4780B73119b644AE5ecd22b376","decimals":18},"minCheckoutUsd":5},{"chainId":"8253038","chainName":"Bitcoin","token":{"name":"Bitcoin","symbol":"BTC","address":"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE","decimals":8},"minCheckoutUsd":7},{"chainId":"1151111081099710","chainName":"Solana","token":{"name":"Solana SOL","symbol":"SOL","address":"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE","decimals":9},"minCheckoutUsd":2},{"chainId":"1151111081099710","chainName":"Solana","token":{"name":"USD Coin","symbol":"USDC","address":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v","decimals":6},"minCheckoutUsd":2},{"chainId":"728126428","chainName":"Tron","token":{"name":"Tether USD","symbol":"USDT","address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","decimals":6},"minCheckoutUsd":7},{"chainId":"137","chainName":"Polygon","token":{"name":"Polygon","symbol":"POL","address":"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE","decimals":18},"minCheckoutUsd":2}],"note":"These are the currently supported chains and assets for deposits and withdrawals."}`

// statusJSON is a live GET /status/{address}?limit=2 response. It is the
// single most informative capture available: three rows came back for a limit
// of two (the pending row rides outside pagination), the DEPOSIT_DETECTED row
// carries neither txHash nor createdTimeMs, and toTokenAddress appears in two
// different hex casings within the one response.
const statusJSON = `{"transactions":[{"fromChainId":"1151111081099710","fromTokenAddress":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v","fromAmountBaseUnit":"3000000","toChainId":"137","toTokenAddress":"0x2791bca1f2de4661ed88a30c99a7a9449aa84174","status":"DEPOSIT_DETECTED"},{"fromChainId":"1151111081099710","fromTokenAddress":"11111111111111111111111111111111","fromAmountBaseUnit":"82251250","toChainId":"137","toTokenAddress":"0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174","status":"COMPLETED","txHash":"4j9VWa2RAhNGZJwB21YAEvShPouV8WQzGxVtL8PuTGo2Ke87rzDvY2Uurt4WXm4KWM23bhqLVJuGRqkQ6Swgxtrz","createdTimeMs":1773853697609},{"fromChainId":"1151111081099710","fromTokenAddress":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v","fromAmountBaseUnit":"3000000","toChainId":"137","toTokenAddress":"0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174","status":"COMPLETED","txHash":"kRwVhUNRBRsFYzRTfzra4g2cwRVwd596RKJeqKEWKL7SGB6HwNSeWkRdn7BQwfYYo2HhuMVvk8uYQgD1nyrz8u5","createdTimeMs":1771956373106}],"nextCursor":"eyJ2IjozLCJrIjp7InR4SGFzaCI6ImtSd1ZoVU5SQlJzRll6UlRmenJhNGcyY3dSVndkNTk2UktKZXFLRVdLTDdTR0I2SHdOU2VXa1JkbjdCUXdmWVlvMkhodU1Wdms4dVlRZ0Qxbnlyejh1NSIsImNyZWF0ZWRUaW1lTXMiOjE3NzE5NTYzNzMxMDZ9fQ"}`

// liveCursor is the opaque continuation token statusJSON ends with. It is
// base64url, so a round trip through a query string must not mangle it.
const liveCursor = "eyJ2IjozLCJrIjp7InR4SGFzaCI6ImtSd1ZoVU5SQlJzRll6UlRmenJhNGcyY3dSVndkNTk2UktKZXFLRVdLTDdTR0I2SHdOU2VXa1JkbjdCUXdmWVlvMkhodU1Wdms4dVlRZ0Qxbnlyejh1NSIsImNyZWF0ZWRUaW1lTXMiOjE3NzE5NTYzNzMxMDZ9fQ"

// quoteJSON is a POST /quote response built from the published schema. Its
// figures are deliberately fractional and long: GasUSD in particular is the
// value a float64 round trip would be most likely to disturb.
const quoteJSON = `{"estCheckoutTimeMs":25000,"estFeeBreakdown":{"appFeeLabel":"Fun.xyz fee","appFeePercent":0.3,"appFeeUsd":0.0435,"fillCostPercent":0.1,"fillCostUsd":0.0145,"gasUsd":0.003854,"maxSlippage":0.5,"minReceived":14.418747,"swapImpact":0.02,"swapImpactUsd":0.0029,"totalImpact":0.42,"totalImpactUsd":0.0609},"estInputUsd":14.5,"estOutputUsd":14.491203,"estToTokenBaseUnit":"14491203","quoteId":"0x00c34ba467184b0146406d62b0e60aaa24ed52460bd456222b6155a0d9de0ad5"}`

// depositJSON is a POST /deposit response built from the published schema.
const depositJSON = `{"address":{"evm":"0x23566f8b2E82aDfCf01846E54899d110e97AC053","svm":"CrvTBvzryYxBHbWu2TiQpcqD5M7Le7iBKzVmEj3f36Jb","btc":"bc1q8eau83qffxcj8ht4hsjdza3lha9r3egfqysj3g","tron":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"},"note":"Only certain chains and tokens are supported. See /supported-assets for details."}`

// withdrawJSON is a POST /withdraw response. The endpoint reuses
// DepositResponse, and a real reply routes to one destination, so only the
// matching chain family's address comes back.
const withdrawJSON = `{"address":{"evm":"0x23566f8b2E82aDfCf01846E54899d110e97AC053"},"note":"Send funds to these addresses to bridge to your destination chain and token."}`

// ---------------------------------------------------------------------------
// Transcription: decode the live captures with DisallowUnknownFields, so a
// stray or misspelled json tag anywhere in these structs fails the test.

func TestSupportedAssetsTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(supportedAssetsJSON))
	dec.DisallowUnknownFields()
	var got SupportedAssetsResponse
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decoding live supported-assets fixture: %v", err)
	}

	if got.Note == "" {
		t.Error("Note is empty: the undocumented top-level note must survive decoding")
	}
	if len(got.SupportedAssets) != 6 {
		t.Fatalf("len(SupportedAssets) = %d, want 6", len(got.SupportedAssets))
	}

	eth := got.SupportedAssets[0]
	if eth.ChainID != "1" || eth.ChainName != "Ethereum" {
		t.Errorf("first asset = %+v, want chain 1 Ethereum", eth)
	}
	// MinCheckoutUSD is json.Number: compare its text, never a float.
	if eth.MinCheckoutUSD != json.Number("5") {
		t.Errorf("MinCheckoutUSD = %q, want 5", eth.MinCheckoutUSD)
	}
	if eth.Token.Decimals != 18 {
		t.Errorf("Token.Decimals = %d, want 18", eth.Token.Decimals)
	}

	// Decimals is load-bearing and is not 6 everywhere: these three chains
	// disagree, which is why no amount here can be scaled by a constant.
	if got.SupportedAssets[1].Token.Decimals != 8 {
		t.Errorf("Bitcoin decimals = %d, want 8", got.SupportedAssets[1].Token.Decimals)
	}
	if got.SupportedAssets[2].Token.Decimals != 9 {
		t.Errorf("Solana SOL decimals = %d, want 9", got.SupportedAssets[2].Token.Decimals)
	}

	// Token.Address is not an EVM address type: base58 on Solana and Tron.
	if got.SupportedAssets[3].Token.Address != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" {
		t.Errorf("Solana USDC address = %q, want the base58 mint", got.SupportedAssets[3].Token.Address)
	}
	if got.SupportedAssets[4].Token.Address != "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" {
		t.Errorf("Tron USDT address = %q, want the base58 contract", got.SupportedAssets[4].Token.Address)
	}

	// (ChainID, Symbol) is not unique but (ChainID, Address) is: the two
	// Solana rows share a symbol space yet differ by address.
	if got.SupportedAssets[2].ChainID != got.SupportedAssets[3].ChainID {
		t.Fatal("expected two rows on the same Solana chain id")
	}
	if got.SupportedAssets[2].Token.Address == got.SupportedAssets[3].Token.Address {
		t.Error("two rows on one chain share an address: (chainId, address) must be unique")
	}
}

func TestStatusTranscription(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(statusJSON))
	dec.DisallowUnknownFields()
	var got StatusPage
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decoding live status fixture: %v", err)
	}

	if len(got.Transactions) != 3 {
		t.Fatalf("len(Transactions) = %d, want 3", len(got.Transactions))
	}
	if got.NextCursor != liveCursor {
		t.Errorf("NextCursor = %q, want the captured cursor", got.NextCursor)
	}

	// The pending row has neither a hash nor a timestamp; both decode to
	// their zero values rather than failing.
	pending := got.Transactions[0]
	if pending.Status != StatusDepositDetected {
		t.Errorf("Transactions[0].Status = %q, want %q", pending.Status, StatusDepositDetected)
	}
	if pending.TxHash != "" || pending.CreatedTimeMs != 0 {
		t.Errorf("pending row = %+v, want an empty TxHash and a zero CreatedTimeMs", pending)
	}

	done := got.Transactions[1]
	if done.Status != StatusCompleted {
		t.Errorf("Transactions[1].Status = %q, want %q", done.Status, StatusCompleted)
	}
	if done.CreatedTimeMs != 1773853697609 {
		t.Errorf("CreatedTimeMs = %d, want 1773853697609", done.CreatedTimeMs)
	}
	// The amount stays the exact integer string the wire carried.
	if done.FromAmountBaseUnit != "82251250" {
		t.Errorf("FromAmountBaseUnit = %q, want the base-unit string 82251250", done.FromAmountBaseUnit)
	}

	// One response, one token, two hex casings — the reason every address
	// comparison against this API must be case-insensitive.
	a, b := got.Transactions[0].ToTokenAddress, got.Transactions[1].ToTokenAddress
	if a == b {
		t.Fatal("fixture lost its mixed-case toTokenAddress rows")
	}
	if !strings.EqualFold(a, b) {
		t.Errorf("ToTokenAddress %q and %q differ by more than case", a, b)
	}
}

func TestStatusNullCursorDecodesEmpty(t *testing.T) {
	// null is the documented end-of-walk value and is required, not omitted.
	const lastPage = `{"transactions":[],"nextCursor":null}`
	var got StatusPage
	if err := json.Unmarshal([]byte(lastPage), &got); err != nil {
		t.Fatalf("decoding a final page: %v", err)
	}
	if got.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty: null means the walk is complete", got.NextCursor)
	}
}

func TestQuoteResponseKeepsExactDecimals(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(quoteJSON))
	dec.DisallowUnknownFields()
	var got QuoteResponse
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decoding quote fixture: %v", err)
	}

	if got.EstCheckoutTimeMs != 25000 {
		t.Errorf("EstCheckoutTimeMs = %d, want 25000", got.EstCheckoutTimeMs)
	}
	if got.EstToTokenBaseUnit != "14491203" {
		t.Errorf("EstToTokenBaseUnit = %q, want 14491203", got.EstToTokenBaseUnit)
	}
	if got.QuoteID != "0x00c34ba467184b0146406d62b0e60aaa24ed52460bd456222b6155a0d9de0ad5" {
		t.Errorf("QuoteID = %q", got.QuoteID)
	}
	// Every money and percent member is json.Number, so the assertion is
	// string equality: no float64 exists anywhere on this path to round it.
	if got.EstInputUSD != json.Number("14.5") {
		t.Errorf("EstInputUSD = %q, want 14.5", got.EstInputUSD)
	}
	if got.EstOutputUSD != json.Number("14.491203") {
		t.Errorf("EstOutputUSD = %q, want 14.491203", got.EstOutputUSD)
	}
	fees := got.EstFeeBreakdown
	if fees.AppFeeLabel != "Fun.xyz fee" {
		t.Errorf("AppFeeLabel = %q", fees.AppFeeLabel)
	}
	if fees.GasUSD != json.Number("0.003854") {
		t.Errorf("GasUSD = %q, want the exact text 0.003854", fees.GasUSD)
	}
	if fees.MinReceived != json.Number("14.418747") {
		t.Errorf("MinReceived = %q, want 14.418747", fees.MinReceived)
	}
	if fees.AppFeePercent != json.Number("0.3") || fees.TotalImpactUSD != json.Number("0.0609") {
		t.Errorf("fee breakdown = %+v", fees)
	}
}

// ---------------------------------------------------------------------------
// HTTP-level behavior: method/path/query wiring, POST bodies, error mapping.
// bridgeServer starts an httptest.Server and returns a Client pointed at it,
// mirroring gamma_test.go's gammaServer.

func bridgeServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(WithHost(srv.URL))
}

// bodyRequest records what the test server was actually sent.
type bodyRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

func recordBody(t *testing.T, rec *bodyRequest, w http.ResponseWriter, r *http.Request, respBody string) {
	t.Helper()
	rec.Method = r.Method
	rec.Path = r.URL.Path
	rec.Query = r.URL.Query()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	rec.Body = b
	w.Write([]byte(respBody))
}

var authHeaderKeys = []string{"POLY_ADDRESS", "POLY_SIGNATURE", "POLY_API_KEY", "POLY_PASSPHRASE"}

func checkNoAuth(t *testing.T, r *http.Request) {
	t.Helper()
	for _, k := range authHeaderKeys {
		if r.Header.Get(k) != "" {
			t.Errorf("request carries auth header %s, want none: every bridge endpoint is public", k)
		}
	}
}

func TestSupportedAssets(t *testing.T) {
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != epSupportedAssets {
			t.Errorf("path = %s, want %s", r.URL.Path, epSupportedAssets)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		w.Write([]byte(supportedAssetsJSON))
	})
	got, err := c.SupportedAssets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SupportedAssets) != 6 {
		t.Errorf("len(SupportedAssets) = %d, want 6", len(got.SupportedAssets))
	}
	if got.SupportedAssets[1].MinCheckoutUSD != json.Number("7") {
		t.Errorf("Bitcoin MinCheckoutUSD = %q, want 7", got.SupportedAssets[1].MinCheckoutUSD)
	}
}

func TestQuotePostsBody(t *testing.T) {
	var rec bodyRequest
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		recordBody(t, &rec, w, r, quoteJSON)
	})

	req := QuoteRequest{
		FromAmountBaseUnit: "10000000",
		FromChainID:        "137",
		FromTokenAddress:   "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359",
		RecipientAddress:   "0x17eC161f126e82A8ba337f4022d574DBEaFef575",
		ToChainID:          "137",
		ToTokenAddress:     "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
	}
	got, err := c.Quote(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.QuoteID == "" {
		t.Error("QuoteID is empty")
	}

	if rec.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.Path != epQuote {
		t.Errorf("path = %s, want %s", rec.Path, epQuote)
	}
	if len(rec.Query) != 0 {
		t.Errorf("query = %v, want none: every quote parameter travels in the body", rec.Query)
	}
	// Decode the body back through the same struct to pin the six wire names.
	dec := json.NewDecoder(strings.NewReader(string(rec.Body)))
	dec.DisallowUnknownFields()
	var sent QuoteRequest
	if err := dec.Decode(&sent); err != nil {
		t.Fatalf("decoding sent body: %v (body %s)", err, rec.Body)
	}
	if sent != req {
		t.Errorf("sent body = %+v, want %+v", sent, req)
	}
	// The amount must reach the wire as the untouched integer string.
	if !strings.Contains(string(rec.Body), `"fromAmountBaseUnit":"10000000"`) {
		t.Errorf("body = %s, want fromAmountBaseUnit as the string 10000000", rec.Body)
	}
	// recipientAddress here; /withdraw spells the same idea recipientAddr.
	if !strings.Contains(string(rec.Body), `"recipientAddress"`) {
		t.Errorf("body = %s, want the recipientAddress spelling", rec.Body)
	}
}

func TestDepositPostsAddress(t *testing.T) {
	var rec bodyRequest
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		w.WriteHeader(http.StatusCreated)
		recordBody(t, &rec, w, r, depositJSON)
	})

	const wallet = "0x56687bf447db6ffa42ffe2204a05edaa20f55839"
	got, err := c.Deposit(context.Background(), wallet, Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.Path != epDeposit {
		t.Errorf("path = %s, want %s", rec.Path, epDeposit)
	}
	var sent depositRequest
	if err := json.Unmarshal(rec.Body, &sent); err != nil {
		t.Fatalf("decoding sent body: %v (body %s)", err, rec.Body)
	}
	if sent.Address != wallet {
		t.Errorf("body.address = %q, want %q", sent.Address, wallet)
	}

	if got.Address.EVM != "0x23566f8b2E82aDfCf01846E54899d110e97AC053" {
		t.Errorf("Address.EVM = %q", got.Address.EVM)
	}
	if got.Address.SVM != "CrvTBvzryYxBHbWu2TiQpcqD5M7Le7iBKzVmEj3f36Jb" {
		t.Errorf("Address.SVM = %q", got.Address.SVM)
	}
	if got.Address.BTC == "" || got.Address.Tron == "" {
		t.Errorf("Address = %+v, want btc and tron populated", got.Address)
	}
	if got.Note == "" {
		t.Error("Note is empty")
	}
}

func TestWithdrawPostsBody(t *testing.T) {
	var rec bodyRequest
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		w.WriteHeader(http.StatusCreated)
		recordBody(t, &rec, w, r, withdrawJSON)
	})

	req := WithdrawRequest{
		Address:        "0x9156dd10bea4c8d7e2d591b633d1694b1d764756",
		ToChainID:      "1",
		ToTokenAddress: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		RecipientAddr:  "0x17eC161f126e82A8ba337f4022d574DBEaFef575",
	}
	got, err := c.Withdraw(context.Background(), req, Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.Path != epWithdraw {
		t.Errorf("path = %s, want %s", rec.Path, epWithdraw)
	}
	dec := json.NewDecoder(strings.NewReader(string(rec.Body)))
	dec.DisallowUnknownFields()
	var sent WithdrawRequest
	if err := dec.Decode(&sent); err != nil {
		t.Fatalf("decoding sent body: %v (body %s)", err, rec.Body)
	}
	if sent != req {
		t.Errorf("sent body = %+v, want %+v", sent, req)
	}
	// The spelling split is the trap: recipientAddr on this endpoint only.
	if !strings.Contains(string(rec.Body), `"recipientAddr"`) {
		t.Errorf("body = %s, want the recipientAddr spelling", rec.Body)
	}
	if strings.Contains(string(rec.Body), `"recipientAddress"`) {
		t.Errorf("body = %s, must not use /quote's recipientAddress spelling", rec.Body)
	}

	// Withdraw reuses DepositResponse, and a real reply names only the
	// destination's chain family.
	if got.Address.EVM == "" {
		t.Error("Address.EVM is empty")
	}
	if got.Address.SVM != "" || got.Address.BTC != "" || got.Address.Tron != "" {
		t.Errorf("Address = %+v, want the absent families empty", got.Address)
	}
}

func TestStatusPathAndCursorRoundTrip(t *testing.T) {
	const addr = "EXoZue2avJae1d45B3fVw2unhkrtToSYQqHtHgfZ2cbE"
	// A cursor with the three characters that must survive URL encoding.
	const cursor = "eyJ2IjozfQ+ab/cd=="
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		checkNoAuth(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != epStatus+"/"+addr {
			t.Errorf("path = %s, want %s/%s", r.URL.Path, epStatus, addr)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q, want 2", got)
		}
		if got := r.URL.Query().Get("cursor"); got != cursor {
			t.Errorf("cursor = %q, want %q: +, / and = must survive the query string", got, cursor)
		}
		if _, ok := r.URL.Query()["paginate"]; ok {
			t.Error("request carries paginate, which this client never sends")
		}
		w.Write([]byte(statusJSON))
	})
	if _, err := c.Status(context.Background(), addr, StatusParams{Limit: 2, Cursor: cursor}); err != nil {
		t.Fatal(err)
	}
}

func TestStatusZeroParamsSendNoQuery(t *testing.T) {
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none: a zero StatusParams takes the server defaults", r.URL.RawQuery)
		}
		w.Write([]byte(statusJSON))
	})
	if _, err := c.Status(context.Background(), "0x23566f8b2E82aDfCf01846E54899d110e97AC053", StatusParams{}); err != nil {
		t.Fatal(err)
	}
}

// TestStatusFirstPageMayExceedLimit pins the live finding that Limit does not
// cap page one: a transfer with no hash and no timestamp cannot be named by a
// cursor, so it rides outside pagination. The client must return every row it
// was sent.
func TestStatusFirstPageMayExceedLimit(t *testing.T) {
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(statusJSON))
	})
	got, err := c.Status(context.Background(), "EXoZue2avJae1d45B3fVw2unhkrtToSYQqHtHgfZ2cbE", StatusParams{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Transactions) != 3 {
		t.Errorf("len(Transactions) = %d, want 3 rows for a limit of 2", len(got.Transactions))
	}
	if got.NextCursor == "" {
		t.Error("NextCursor is empty, want the walk to continue")
	}
}

// TestStatusWalkStopsOnlyOnEmptyCursor drives the documented walk: an empty
// page in the middle is not the end, and only an empty NextCursor is.
func TestStatusWalkStopsOnlyOnEmptyCursor(t *testing.T) {
	pages := []string{
		statusJSON,
		`{"transactions":[],"nextCursor":"page-3"}`,
		`{"transactions":[{"fromChainId":"1","fromTokenAddress":"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE","fromAmountBaseUnit":"1","toChainId":"137","toTokenAddress":"0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174","status":"FAILED"}],"nextCursor":null}`,
	}
	var served int
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if served >= len(pages) {
			t.Fatalf("client asked for page %d, but the walk should have stopped", served+1)
		}
		w.Write([]byte(pages[served]))
		served++
	})

	var all []Transaction
	p := StatusParams{Limit: 2}
	for {
		page, err := c.Status(context.Background(), "addr", p)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Transactions...)
		if page.NextCursor == "" {
			break
		}
		p.Cursor = page.NextCursor
	}
	if served != len(pages) {
		t.Errorf("fetched %d pages, want %d", served, len(pages))
	}
	if len(all) != 4 {
		t.Errorf("collected %d transactions, want 4", len(all))
	}
	if all[len(all)-1].Status != StatusFailed {
		t.Errorf("last status = %q, want %q", all[len(all)-1].Status, StatusFailed)
	}
}

// statusErrorCase is one documented /status failure and the *polymarket.Error
// it must become.
type statusErrorCase struct {
	name    string
	status  int
	body    string
	message string
}

// TestStatusErrorsBecomePolymarketError covers the three failures live traffic
// returns. The 500 matters most: a well-formed address the bridge has never
// seen answers 500, not 404 and not an empty page, so a 500 here is not proof
// the server is broken.
func TestStatusErrorsBecomePolymarketError(t *testing.T) {
	cases := []statusErrorCase{
		{
			name:    "invalid address",
			status:  http.StatusBadRequest,
			body:    `{"error":"invalid address"}`,
			message: "invalid address",
		},
		{
			name:    "limit out of range",
			status:  http.StatusBadRequest,
			body:    `{"error":"limit must be an integer between 1 and 100"}`,
			message: "limit must be an integer between 1 and 100",
		},
		{
			name:    "unknown address",
			status:  http.StatusInternalServerError,
			body:    `{"error":"cannot get transaction status"}`,
			message: "cannot get transaction status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			_, err := c.Status(context.Background(), "notanaddress", StatusParams{})
			if err == nil {
				t.Fatal("got nil error, want one")
			}
			var apiErr *polymarket.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error type = %T, want *polymarket.Error", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Message != tc.message {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.message)
			}
		})
	}
}

// TestStatusPlainTextErrorStillFails covers the one failure that is not JSON:
// an unknown path answers 404 text/plain "404 page not found". Decoding the
// error envelope must not be what decides an error happened.
func TestStatusPlainTextErrorStillFails(t *testing.T) {
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 page not found\n"))
	})
	_, err := c.Status(context.Background(), "", StatusParams{})
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	var apiErr *polymarket.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Body, "404 page not found") {
		t.Errorf("Body = %q, want the plain-text body preserved", apiErr.Body)
	}
}

func TestQuoteMissingFieldError(t *testing.T) {
	c := bridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"toTokenAddress is required"}`))
	})
	_, err := c.Quote(context.Background(), QuoteRequest{FromChainID: "137"})
	var apiErr *polymarket.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *polymarket.Error", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "toTokenAddress is required" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

// ---------------------------------------------------------------------------
// Client construction smoke tests.

func TestNewAndNewWithSession(t *testing.T) {
	c := New(WithHost("https://example.invalid"))
	if c == nil || c.session == nil {
		t.Fatal("New() returned a client with no session")
	}
	s := polymarket.NewSession(polymarket.BridgeHost)
	c2 := NewWithSession(s)
	if c2.session != s {
		t.Error("NewWithSession() did not adopt the given session")
	}
}

// TestDefaultHostIsPaced proves the default client picks up the bridge host's
// published rate limits, which NewSession attaches by host.
func TestDefaultHostIsPaced(t *testing.T) {
	c := New()
	if c.session.Host() != polymarket.BridgeHost {
		t.Errorf("host = %q, want %q", c.session.Host(), polymarket.BridgeHost)
	}
	if c.session.Limiter() == nil {
		t.Error("session has no limiter: BridgeRateLimits should attach to BridgeHost")
	}
}

// An attributionCase is one attribution and the header value it should put on
// the wire, or "" when it should send none.
type attributionCase struct {
	name string
	code string
	want string
}

// TestAttributionReachesTheWire checks the one header these endpoints take.
// It is not signed and not authentication — a builder code on an order is a
// signed field, this one is advisory — so the only thing to get right is that
// it is sent when given and absent when not.
func TestAttributionReachesTheWire(t *testing.T) {
	cases := []attributionCase{
		{"a builder code", "0x00000000000000000000000000000000000000000000000000000000abcd1234",
			"0x00000000000000000000000000000000000000000000000000000000abcd1234"},
		{"no attribution", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var present bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("X-Builder-Code")
				_, present = r.Header["X-Builder-Code"]
				w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			c := New(WithHost(srv.URL))
			if _, err := c.Deposit(context.Background(), "0xabc", Attribution{BuilderCode: tc.code}); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("X-Builder-Code = %q, want %q", got, tc.want)
			}
			if tc.want == "" && present {
				t.Error("an empty attribution sent the header anyway")
			}
		})
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A fakeNode is a JSON-RPC server that answers from a script. Every test in
// this package that talks to a node talks to one of these: this client must
// never send a transaction to a real one, because a transaction that reaches a
// real node is paid for.
type fakeNode struct {
	t *testing.T

	mu    sync.Mutex
	calls []nodeCall

	// result answers any method that has no entry in results.
	result string
	// results answers one method each, and is consumed in order when a
	// method appears more than once.
	results map[string][]any
	// errs makes a method fail.
	errs map[string]*RPCError
}

// A nodeCall is one request the fake node received.
type nodeCall struct {
	Method string
	Params []json.RawMessage
}

// start runs the server and returns its URL. It is closed when the test ends.
func (n *fakeNode) start() string {
	server := httptest.NewServer(http.HandlerFunc(n.serve))
	n.t.Cleanup(server.Close)
	return server.URL
}

func (n *fakeNode) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      int               `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		n.t.Errorf("fake node: decoding request: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.JSONRPC != "2.0" {
		n.t.Errorf("fake node: jsonrpc = %q, want 2.0", req.JSONRPC)
	}

	n.mu.Lock()
	n.calls = append(n.calls, nodeCall{Method: req.Method, Params: req.Params})
	var result any = n.result
	if queue, ok := n.results[req.Method]; ok && len(queue) > 0 {
		result = queue[0]
		if len(queue) > 1 {
			n.results[req.Method] = queue[1:]
		}
	}
	rpcErr := n.errs[req.Method]
	n.mu.Unlock()

	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		encoded, err := json.Marshal(result)
		if err != nil {
			n.t.Errorf("fake node: encoding result: %v", err)
		}
		resp.Result = encoded
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		n.t.Errorf("fake node: writing response: %v", err)
	}
}

// received returns the calls made to the node.
func (n *fakeNode) received() []nodeCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]nodeCall(nil), n.calls...)
}

// last returns the most recent call to a method.
func (n *fakeNode) last(method string) nodeCall {
	n.t.Helper()
	calls := n.received()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			return calls[i]
		}
	}
	n.t.Fatalf("fake node: no %s call", method)
	return nodeCall{}
}

// lastData returns the calldata of the most recent eth_call.
func (n *fakeNode) lastData(t *testing.T) []byte {
	t.Helper()
	call := n.last("eth_call")
	var p callParams
	if err := json.Unmarshal(call.Params[0], &p); err != nil {
		t.Fatalf("decoding eth_call params: %v", err)
	}
	data, err := parseData(p.Data)
	if err != nil {
		t.Fatalf("decoding eth_call data: %v", err)
	}
	return data
}

// TestNonceCountsPendingTransactions pins which block a nonce is read at. The
// latest block ignores a transaction the node has accepted and not yet mined,
// and the next transaction then reuses its number.
func TestNonceCountsPendingTransactions(t *testing.T) {
	node := &fakeNode{t: t, result: "0x2a"}
	client := New(node.start())

	got, err := client.Nonce(context.Background(), vectorOwner)
	if err != nil {
		t.Fatalf("Nonce: %v", err)
	}
	if got != 42 {
		t.Errorf("nonce = %d, want 42", got)
	}
	call := node.last("eth_getTransactionCount")
	if want := `"pending"`; string(call.Params[1]) != want {
		t.Errorf("nonce read at %s, want %s", call.Params[1], want)
	}
}

// TestCheckChainIDRefusesTheWrongChain covers the mistake that costs the most
// for the least reason: signing for Polygon and sending to somewhere else.
func TestCheckChainIDRefusesTheWrongChain(t *testing.T) {
	node := &fakeNode{t: t, result: "0x1"}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))
	if err := client.CheckChainID(context.Background()); err == nil {
		t.Error("accepted a node serving another chain")
	}

	node = &fakeNode{t: t, result: "0x89"}
	client = New(node.start(), WithChainID(polymarket.ChainPolygon))
	if err := client.CheckChainID(context.Background()); err != nil {
		t.Errorf("rejected the right chain: %v", err)
	}
}

// TestSuggestFees pins the headroom this client adds. The max fee is twice the
// base fee plus the tip, and the base fee is read from the latest block rather
// than guessed.
func TestSuggestFees(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{
		"eth_getBlockByNumber":     {blockHeader{Number: "0x1", BaseFeePerGas: "0x77359400"}}, // 2 gwei
		"eth_maxPriorityFeePerGas": {"0x6fc23ac00"},                                           // 30 gwei
	}}
	client := New(node.start())

	fees, err := client.SuggestFees(context.Background())
	if err != nil {
		t.Fatalf("SuggestFees: %v", err)
	}
	if want := big.NewInt(2_000_000_000); fees.BaseFee.Cmp(want) != 0 {
		t.Errorf("base fee = %s, want %s", fees.BaseFee, want)
	}
	if want := big.NewInt(30_000_000_000); fees.MaxPriorityFeePerGas.Cmp(want) != 0 {
		t.Errorf("priority fee = %s, want %s", fees.MaxPriorityFeePerGas, want)
	}
	if want := big.NewInt(34_000_000_000); fees.MaxFeePerGas.Cmp(want) != 0 {
		t.Errorf("max fee = %s, want %s", fees.MaxFeePerGas, want)
	}
}

// TestSuggestFeesRefusesAPreLondonChain covers a node whose blocks carry no
// base fee: this client builds only EIP-1559 transactions, so it says so
// rather than signing one such a chain cannot price.
func TestSuggestFeesRefusesAPreLondonChain(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{
		"eth_getBlockByNumber": {blockHeader{Number: "0x1"}},
	}}
	client := New(node.start())
	if _, err := client.SuggestFees(context.Background()); err == nil {
		t.Error("priced a transaction for a chain with no base fee")
	}
}

// TestSendFillsSignsAndBroadcasts walks the whole path: the fields the node
// supplies, the signature over them, and the raw transaction that reaches
// eth_sendRawTransaction.
func TestSendFillsSignsAndBroadcasts(t *testing.T) {
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)

	node := &fakeNode{t: t}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))

	data, err := ApproveData(contracts.Exchange, MaxUint256())
	if err != nil {
		t.Fatalf("ApproveData: %v", err)
	}
	want := Transaction{
		ChainID:              big.NewInt(polymarket.ChainPolygon),
		Nonce:                7,
		MaxPriorityFeePerGas: big.NewInt(30_000_000_000),
		MaxFeePerGas:         big.NewInt(34_000_000_000),
		Gas:                  75_000, // the estimate below plus a quarter
		To:                   contracts.Collateral,
		Value:                new(big.Int),
		Data:                 data,
	}
	signed, err := SignTransaction(key, want)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}

	node.results = map[string][]any{
		"eth_getTransactionCount":  {"0x7"},
		"eth_getBlockByNumber":     {blockHeader{Number: "0x1", BaseFeePerGas: "0x77359400"}},
		"eth_maxPriorityFeePerGas": {"0x6fc23ac00"},
		"eth_estimateGas":          {"0xea60"}, // 60000
		"eth_sendRawTransaction":   {signed.Hash},
	}

	got, err := client.Send(context.Background(), key, Transaction{To: contracts.Collateral, Data: data})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.RawHex() != signed.RawHex() {
		t.Errorf("sent %s, want %s", got.RawHex(), signed.RawHex())
	}
	if got.Transaction.Gas != want.Gas {
		t.Errorf("gas = %d, want %d (the estimate plus a quarter)", got.Transaction.Gas, want.Gas)
	}

	broadcast := node.last("eth_sendRawTransaction")
	var raw string
	if err := json.Unmarshal(broadcast.Params[0], &raw); err != nil {
		t.Fatalf("decoding broadcast parameter: %v", err)
	}
	if !strings.EqualFold(raw, signed.RawHex()) {
		t.Errorf("broadcast %s, want %s", raw, signed.RawHex())
	}

	// The estimate must be made as the sender: a contract that checks
	// msg.sender estimates differently for anyone else.
	estimate := node.last("eth_estimateGas")
	var p callParams
	if err := json.Unmarshal(estimate.Params[0], &p); err != nil {
		t.Fatalf("decoding estimate parameter: %v", err)
	}
	if !strings.EqualFold(p.From, key.Address()) {
		t.Errorf("estimated as %s, want %s", p.From, key.Address())
	}
}

// TestSendRefusesAHashMismatch covers a node that accepts something other than
// what was signed. Waiting on the local hash would then wait forever, so this
// is reported rather than smoothed over.
func TestSendRefusesAHashMismatch(t *testing.T) {
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	node := &fakeNode{t: t, results: map[string][]any{
		"eth_getTransactionCount":  {"0x0"},
		"eth_getBlockByNumber":     {blockHeader{Number: "0x1", BaseFeePerGas: "0x1"}},
		"eth_maxPriorityFeePerGas": {"0x1"},
		"eth_estimateGas":          {"0x5208"},
		"eth_sendRawTransaction":   {"0x" + strings.Repeat("11", 32)},
	}}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))

	_, err = client.Send(context.Background(), key, Transaction{To: vectorOwner})
	if err == nil {
		t.Fatal("accepted a hash the signed transaction does not produce")
	}
	if !strings.Contains(err.Error(), "hashes to") {
		t.Errorf("error = %v, want it to name the mismatch", err)
	}
}

// TestRPCErrorSurvives covers the failure a node reports inside an HTTP 200.
// It is how a rejected transaction arrives, and it must reach the caller as an
// error rather than as an empty result.
func TestRPCErrorSurvives(t *testing.T) {
	node := &fakeNode{t: t, errs: map[string]*RPCError{
		"eth_sendRawTransaction": {Code: -32000, Message: "nonce too low"},
	}}
	client := New(node.start())

	_, err := client.SendRaw(context.Background(), []byte{0x02})
	if err == nil {
		t.Fatal("a node error was swallowed")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error %v is not an RPCError", err)
	}
	if rpcErr.Code != -32000 || rpcErr.Message != "nonce too low" {
		t.Errorf("error = %+v, want the node's own", rpcErr)
	}
}

// TestReceiptReportsAReverted covers the outcome a caller most needs to see:
// the transaction was mined, was paid for, and did nothing.
func TestReceiptReportsAReverted(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{
		"eth_getTransactionReceipt": {receiptWire{
			TransactionHash:   "0xabc",
			BlockNumber:       "0x10",
			GasUsed:           "0x5208",
			EffectiveGasPrice: "0x77359400",
			Status:            "0x0",
			Logs:              []logWire{{Address: vectorOwner, Topics: []string{"0x1"}, Data: "0x"}},
		}},
	}}
	client := New(node.start())

	r, ok, err := client.Receipt(context.Background(), "0xabc")
	if err != nil || !ok {
		t.Fatalf("Receipt: %v, ok=%v", err, ok)
	}
	if r.Succeeded() {
		t.Error("a reverted transaction reported success")
	}
	if r.BlockNumber != 16 || r.GasUsed != 21000 {
		t.Errorf("receipt = %+v, want block 16 and 21000 gas", r)
	}
	if len(r.Logs) != 1 {
		t.Errorf("receipt carries %d logs, want 1", len(r.Logs))
	}
}

// TestReceiptIsAbsentBeforeMining covers the null a node answers with while a
// transaction is still in the pool. It is not an error and not a zero receipt.
func TestReceiptIsAbsentBeforeMining(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{"eth_getTransactionReceipt": {nil}}}
	client := New(node.start())

	_, ok, err := client.Receipt(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	if ok {
		t.Error("reported a receipt the node does not have")
	}
}

// TestWaitReceiptPolls covers the wait: absent, then present.
func TestWaitReceiptPolls(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{
		"eth_getTransactionReceipt": {nil, receiptWire{TransactionHash: "0xabc", Status: "0x1"}},
	}}
	client := New(node.start())

	r, err := client.WaitReceipt(context.Background(), "0xabc", time.Millisecond)
	if err != nil {
		t.Fatalf("WaitReceipt: %v", err)
	}
	if !r.Succeeded() {
		t.Error("waited out a successful transaction and reported failure")
	}
	if len(node.received()) < 2 {
		t.Errorf("polled %d times, want at least 2", len(node.received()))
	}
}

// TestWaitReceiptStopsWithTheContext covers a transaction that never lands.
func TestWaitReceiptStopsWithTheContext(t *testing.T) {
	node := &fakeNode{t: t, results: map[string][]any{"eth_getTransactionReceipt": {nil}}}
	client := New(node.start())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.WaitReceipt(ctx, "0xabc", time.Millisecond); err == nil {
		t.Error("waited forever and returned nothing")
	}
}

// TestAddressesAreChecked covers the arguments that reach a node as strings,
// where a truncated address reads as a different account rather than an error.
func TestAddressesAreChecked(t *testing.T) {
	node := &fakeNode{t: t, result: "0x0"}
	client := New(node.start())

	if _, err := client.Nonce(context.Background(), "0x1234"); err == nil {
		t.Error("read a nonce for a malformed address")
	}
	if _, err := client.Balance(context.Background(), "not an address"); err == nil {
		t.Error("read a balance for a malformed address")
	}
	if len(node.received()) != 0 {
		t.Errorf("sent %d requests for malformed addresses, want none", len(node.received()))
	}
}

// quantityCase is one integer and the hex quantity it is written as.
type quantityCase struct {
	name  string
	value *big.Int
	hex   string
}

// quantityCases pin the JSON-RPC number format: hex, no leading zeros, and a
// bare "0x0" for zero. A quantity with a leading zero is rejected by some
// nodes and silently accepted by others.
var quantityCases = []quantityCase{
	{"zero", big.NewInt(0), "0x0"},
	{"one", big.NewInt(1), "0x1"},
	{"fifteen", big.NewInt(15), "0xf"},
	{"sixteen", big.NewInt(16), "0x10"},
	{"gwei", big.NewInt(1_000_000_000), "0x3b9aca00"},
}

func TestQuantityEncoding(t *testing.T) {
	for _, tc := range quantityCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hexQuantity(tc.value); got != tc.hex {
				t.Errorf("hexQuantity(%s) = %s, want %s", tc.value, got, tc.hex)
			}
			back, err := parseQuantity(tc.hex)
			if err != nil {
				t.Fatalf("parseQuantity(%s): %v", tc.hex, err)
			}
			if back.Cmp(tc.value) != 0 {
				t.Errorf("parseQuantity(%s) = %s, want %s", tc.hex, back, tc.value)
			}
		})
	}
	if got := hexQuantity(nil); got != "0x0" {
		t.Errorf("hexQuantity(nil) = %s, want 0x0", got)
	}
	if _, err := parseQuantity("0x"); err == nil {
		t.Error("parsed an empty quantity")
	}
	if _, err := parseQuantity("0xzz"); err == nil {
		t.Error("parsed a non-hex quantity")
	}
}

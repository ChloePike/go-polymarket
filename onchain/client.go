// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package onchain sends transactions to Polygon directly, for the steps of a
// Polymarket account that are not an API call at all: approving the token
// contracts the exchange moves funds through, deploying a wallet, or executing
// a batch a caller would rather pay for than hand to the relayer.
//
// Everything else in this module talks to a Polymarket host. This package
// talks to an Ethereum JSON-RPC node, which Polymarket does not run, so there
// is no default endpoint: New takes the URL of a node the caller trusts. A
// node sees every address and every transaction it is asked about.
//
// Two warnings, both of them about money.
//
// First, a transaction sent here costs gas and cannot be recalled. The gasless
// path — where Polymarket's relayer pays and the wallet only signs — is the
// relayer package, and it is the one an ordinary account uses. Reach for this
// package when there is no relayer in the picture: an EOA that trades for
// itself, or a recovery that must not depend on a third party.
//
// Second, an approval is not a small permission. RequiredApprovals sets the
// allowances the exchange contracts need, and an unlimited allowance lets the
// approved contract move that token out of the account for as long as it
// stands. Approve deliberately takes the amount rather than assuming one.
//
// Only EIP-1559 transactions are built here. Polygon has accepted them since
// the London fork and a node will price one for you; the legacy form exists in
// this client only as the absence of code.
package onchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	polymarket "github.com/ChloePike/go-polymarket"
)

// A Client talks to an Ethereum JSON-RPC node.
//
// It is safe for concurrent use. Every method issues one POST, except the ones
// documented as issuing several.
type Client struct {
	session *polymarket.Session
}

// New returns a client for the JSON-RPC endpoint at url.
//
// There is no default: an endpoint is a service the caller chooses and, for a
// signed transaction, trusts not to censor or front-run it. Pass the chain the
// endpoint serves with WithChainID when it is not Polygon; Contracts and the
// approval helpers read it, and CheckChainID confirms it against the node.
func New(url string, opts ...Option) *Client {
	return &Client{session: polymarket.NewSession(strings.TrimSuffix(url, "/"), opts...)}
}

// NewWithSession returns a client over an existing Session. The session's host
// must be the JSON-RPC endpoint, not a Polymarket host.
func NewWithSession(s *polymarket.Session) *Client { return &Client{session: s} }

// An Option configures a Client. These are the same options every Polymarket
// client package takes; the ones about credentials have no effect here,
// because a node authenticates nothing.
type Option = polymarket.Option

// Re-exported so a caller needs only this package's import.
var (
	WithHost       = polymarket.WithHost
	WithHTTPClient = polymarket.WithHTTPClient
	WithChainID    = polymarket.WithChainID
	WithUserAgent  = polymarket.WithUserAgent
)

// ChainID reports the chain this client believes it is on. It is what the
// caller configured, not what the node reports; CheckChainID compares them.
func (c *Client) ChainID() int64 { return c.session.ChainID() }

// Contracts returns the Polymarket addresses for the configured chain, and
// reports whether that chain is known.
func (c *Client) Contracts() (polymarket.Contracts, bool) {
	return polymarket.ContractsFor(c.session.ChainID())
}

// An RPCError is an error the node returned in a JSON-RPC response. The node
// answers HTTP 200 and puts the failure in the body, so this is the type a
// rejected transaction arrives as: "nonce too low", "insufficient funds",
// "replacement transaction underpriced".
type RPCError struct {
	// Code is the JSON-RPC error code. The range -32099 to -32000 is
	// reserved for the node's own errors, and the meaning inside it differs
	// between implementations, so match on Message when it matters.
	Code int `json:"code"`
	// Message is the node's description of the failure.
	Message string `json:"message"`
	// Data is whatever else the node attached, often the revert reason.
	Data json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("onchain: rpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("onchain: rpc error %d: %s", e.Code, e.Message)
}

// An rpcRequest is one JSON-RPC 2.0 call. The id is constant because every
// request here is sent on its own and answered before the next one starts;
// this client never batches.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// An rpcResponse is one JSON-RPC 2.0 answer. Result is left undecoded so the
// caller decides its shape, and stays absent when Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

// call issues one JSON-RPC request and decodes its result into out.
//
// A null result decodes into nothing and is not an error: it is how a node
// reports a receipt that does not exist yet. Callers that care compare against
// the zero value, or use the Optional variants below.
func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	if params == nil {
		params = []any{}
	}
	var resp rpcResponse
	req := polymarket.Request{
		Method: http.MethodPost,
		Body:   rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params},
		Out:    &resp,
	}
	if err := c.session.Do(ctx, req); err != nil {
		return fmt.Errorf("onchain: %s: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("onchain: %s: %w", method, resp.Error)
	}
	if out == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("onchain: %s: decoding result: %w", method, err)
	}
	return nil
}

// present reports whether the node answered with a value rather than null. It
// is the distinction between "no receipt yet" and "a receipt".
func (c *Client) present(ctx context.Context, method string, params []any, out any) (bool, error) {
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("onchain: %s: decoding result: %w", method, err)
	}
	return true, nil
}

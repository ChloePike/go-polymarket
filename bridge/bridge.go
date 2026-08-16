// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package bridge

// This file covers all five bridge endpoints: the asset directory, the quote
// estimator, the two address-minting calls, and the transfer status feed.
//
// MONEY: the bridge splits amounts across two encodings, and neither becomes a
// float64 here. Base-unit amounts (FromAmountBaseUnit, EstToTokenBaseUnit) are
// already integer strings on the wire and stay strings end to end. Everything
// the bridge sends as a JSON number and denominates in money or percent —
// MinCheckoutUSD, every member of FeeBreakdown, EstInputUSD, EstOutputUSD —
// is json.Number, so the decimal text the server sent survives intact. Only
// two numeric fields are genuinely integers: EstCheckoutTimeMs and
// CreatedTimeMs are millisecond timestamps, not amounts, and are int64.
//
// A base-unit amount means nothing without Token.Decimals. Live values span 2,
// 6, 7, 8, 9 and 18 — never assume 6 because the collateral is USDC.
//
// ADDRESS CASING: the bridge does not normalize hex case. A single live
// /status response returned toTokenAddress as both
// "0x2791bca1f2de4661ed88a30c99a7a9449aa84174" and
// "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174". Compare any address from this
// package with strings.EqualFold, never ==.
//
// AUTHENTICATION: every endpoint is public, and the three POSTs are public
// too. The optional X-Builder-Code attribution header documented for /deposit
// and /withdraw is NOT sent: polymarket.Request carries no per-request
// headers. Omitting it is a supported call, so nothing here fails without it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	polymarket "github.com/ChloePike/go-polymarket"
)

// Bridge host paths. /status takes its address as a path parameter and is
// built inline next to the method that uses it.
const (
	epSupportedAssets = "/supported-assets"
	epQuote           = "/quote"
	epDeposit         = "/deposit"
	epWithdraw        = "/withdraw"
	epStatus          = "/status"
)

// ---------------------------------------------------------------------------
// Supported assets.

// A Token is one asset the bridge accepts on one chain.
type Token struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
	// Address is a per-chain opaque identifier, NOT an EVM address: 0x-hex on
	// EVM chains, base58 on Solana and Tron, bech32 on Bitcoin. Do not
	// checksum it and do not compare it case-sensitively. Native assets use
	// the sentinel 0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE, which appears
	// on non-EVM chains too — BTC on Bitcoin and SOL on Solana both use it.
	Address string `json:"address"`
	// Decimals converts a base-unit amount to a human amount. It is
	// load-bearing and varies widely: 2, 6, 7, 8, 9 and 18 all occur live.
	Decimals int `json:"decimals"`
}

// A SupportedAsset is one (chain, token) pair the bridge accepts, with the
// minimum deposit that chain requires.
//
// (ChainID, Symbol) is NOT a unique key — BTC appears twice on Bitcoin and SOL
// twice on Solana, once under the native sentinel and once under the chain's
// own address form. Only (ChainID, Token.Address) is unique.
type SupportedAsset struct {
	// ChainID is a decimal string, and non-EVM chains use synthetic ids:
	// Solana "1151111081099710", Tron "728126428", Bitcoin "8253038". Keep it
	// a string; it does not map onto an EVM chain id space.
	ChainID   string `json:"chainId"`
	ChainName string `json:"chainName"`
	Token     Token  `json:"token"`
	// MinCheckoutUSD is the smallest deposit this chain processes. A deposit
	// below it is not credited. Read it per asset rather than hardcoding a
	// figure: the published documentation disagrees with production, and the
	// schema permits a fractional value even though live values are whole.
	MinCheckoutUSD json.Number `json:"minCheckoutUsd"`
}

// A SupportedAssetsResponse is the reply to GET /supported-assets.
//
// Note is undocumented — the published schema declares only supportedAssets —
// but production sends it, so it is decoded rather than dropped.
type SupportedAssetsResponse struct {
	SupportedAssets []SupportedAsset `json:"supportedAssets"`
	Note            string           `json:"note"`
}

// ---------------------------------------------------------------------------
// Quote.

// A QuoteRequest prices one bridge or swap. Every field is required; a
// missing one is answered with 400 and the message "<fieldName> is required".
type QuoteRequest struct {
	// FromAmountBaseUnit is an integer string in the source token's base
	// units, with no decimal point: "10000000" is 10 USDC at 6 decimals.
	FromAmountBaseUnit string `json:"fromAmountBaseUnit"`
	FromChainID        string `json:"fromChainId"`
	FromTokenAddress   string `json:"fromTokenAddress"`
	// RecipientAddress is spelled recipientAddress here and recipientAddr on
	// /withdraw. The two endpoints name the same concept differently.
	RecipientAddress string `json:"recipientAddress"`
	ToChainID        string `json:"toChainId"`
	ToTokenAddress   string `json:"toTokenAddress"`
}

// A FeeBreakdown itemizes what a transfer costs. In broad terms the bridge
// charges an app fee of about 0.3%, a fill cost of about 0.1%, and gas.
//
// Every member is json.Number: each is either money or a percentage, and none
// may pass through a float64. The percent members are percentages, not
// fractions — 0.3 means 0.3%.
type FeeBreakdown struct {
	// AppFeeLabel names the fee's collector, e.g. "Fun.xyz fee". The
	// documentation prose calls the same charge the "Bridge fee".
	AppFeeLabel     string      `json:"appFeeLabel"`
	AppFeePercent   json.Number `json:"appFeePercent"`
	AppFeeUSD       json.Number `json:"appFeeUsd"`
	FillCostPercent json.Number `json:"fillCostPercent"`
	FillCostUSD     json.Number `json:"fillCostUsd"`
	GasUSD          json.Number `json:"gasUsd"`
	// MaxSlippage is the worst slippage the route allows, as a percent, and
	// MinReceived is the amount that survives it.
	MaxSlippage    json.Number `json:"maxSlippage"`
	MinReceived    json.Number `json:"minReceived"`
	SwapImpact     json.Number `json:"swapImpact"`
	SwapImpactUSD  json.Number `json:"swapImpactUsd"`
	TotalImpact    json.Number `json:"totalImpact"`
	TotalImpactUSD json.Number `json:"totalImpactUsd"`
}

// A QuoteResponse estimates one transfer. It is advisory: it carries no
// executable transaction, and the real fill may differ from every figure in
// it. Above roughly $50,000 the documentation recommends a third-party bridge
// instead, to limit slippage.
type QuoteResponse struct {
	// EstCheckoutTimeMs is an estimated duration in milliseconds, not an
	// amount.
	EstCheckoutTimeMs int64        `json:"estCheckoutTimeMs"`
	EstFeeBreakdown   FeeBreakdown `json:"estFeeBreakdown"`
	// EstInputUSD is the value sent and EstOutputUSD the value received. The
	// published schema's two descriptions are swapped; these names follow the
	// field names, which are right.
	EstInputUSD  json.Number `json:"estInputUsd"`
	EstOutputUSD json.Number `json:"estOutputUsd"`
	// EstToTokenBaseUnit is the estimated output as an integer string in the
	// destination token's base units.
	EstToTokenBaseUnit string `json:"estToTokenBaseUnit"`
	// QuoteID is 0x-prefixed 32-byte hex identifying this estimate.
	QuoteID string `json:"quoteId"`
}

// ---------------------------------------------------------------------------
// Deposit and withdraw.

// depositRequest is the body of POST /deposit. It is unexported because
// Deposit takes the one address it holds directly.
type depositRequest struct {
	Address string `json:"address"`
}

// A WithdrawRequest routes a withdrawal to one destination. Every field is
// required.
//
// Each withdrawal address is bound to the destination named here, so build
// one only when the withdrawal is ready to send. Pre-generating addresses and
// reusing them later sends funds to whatever destination the address was
// originally bound to.
type WithdrawRequest struct {
	// Address is the source Polymarket wallet on Polygon, 0x-hex.
	Address string `json:"address"`
	// ToChainID is the destination chain id as a decimal string, e.g. "1" for
	// Ethereum or "1151111081099710" for Solana.
	ToChainID string `json:"toChainId"`
	// ToTokenAddress is the destination token in that chain's own address
	// form.
	ToTokenAddress string `json:"toTokenAddress"`
	// RecipientAddr is the final destination wallet. Note the spelling:
	// /quote calls the same concept recipientAddress.
	RecipientAddr string `json:"recipientAddr"`
}

// BridgeAddresses are the addresses to send funds to, one per chain family.
// Every member is optional: the bridge returns only the families that apply,
// so an absent one decodes to the empty string.
type BridgeAddresses struct {
	EVM  string `json:"evm"`
	SVM  string `json:"svm"`
	BTC  string `json:"btc"`
	Tron string `json:"tron"`
}

// A DepositResponse carries the addresses a transfer should be sent to. It is
// the reply to both POST /deposit and POST /withdraw — the published schema
// reuses one shape for the two endpoints.
//
// Funds sent to these addresses in an unsupported token, or below the
// originating chain's MinCheckoutUSD, are not processed and may be
// unrecoverable. Check SupportedAssets first.
type DepositResponse struct {
	Address BridgeAddresses `json:"address"`
	Note    string          `json:"note"`
}

// ---------------------------------------------------------------------------
// Status.

// A TransactionStatus is how far a bridge transfer has progressed. The states
// occur in the order listed, and only StatusCompleted and StatusFailed are
// terminal — poll every 10 to 30 seconds until one of those two.
type TransactionStatus string

// The six states a bridge transfer passes through, in order.
const (
	StatusDepositDetected   TransactionStatus = "DEPOSIT_DETECTED"
	StatusProcessing        TransactionStatus = "PROCESSING"
	StatusOriginTxConfirmed TransactionStatus = "ORIGIN_TX_CONFIRMED"
	StatusSubmitted         TransactionStatus = "SUBMITTED"
	StatusCompleted         TransactionStatus = "COMPLETED"
	StatusFailed            TransactionStatus = "FAILED"
)

// A Transaction is one deposit or withdrawal seen at a bridge address.
//
// TxHash and CreatedTimeMs are optional and decode to their zero values when
// absent: a transfer has no hash until it completes, and a row still in
// StatusDepositDetected carries neither.
type Transaction struct {
	FromChainID string `json:"fromChainId"`
	// FromTokenAddress is in the source chain's own address form — EVM hex,
	// Solana base58, and so on.
	FromTokenAddress string `json:"fromTokenAddress"`
	// FromAmountBaseUnit is an integer string in the source token's base
	// units. Interpret it with that token's Decimals.
	FromAmountBaseUnit string `json:"fromAmountBaseUnit"`
	ToChainID          string `json:"toChainId"`
	// ToTokenAddress arrives in inconsistent hex case, sometimes both ways
	// within one response. Compare it with strings.EqualFold.
	ToTokenAddress string            `json:"toTokenAddress"`
	Status         TransactionStatus `json:"status"`
	TxHash         string            `json:"txHash"`
	// CreatedTimeMs is Unix epoch milliseconds — a timestamp, not an amount.
	CreatedTimeMs int64 `json:"createdTimeMs"`
}

// A StatusPage is one page of transfers at a bridge address, newest first.
//
// NextCursor is the only end-of-walk signal. It is empty exactly when the wire
// value is null, which means the walk is complete; stop there and nowhere
// else. A page may be empty, shorter than the limit asked for, or — for the
// first page — longer than it, and still be followed by more.
type StatusPage struct {
	Transactions []Transaction `json:"transactions"`
	// NextCursor is opaque. Echo it back as StatusParams.Cursor and never
	// build, edit or decode one; the bridge's own documentation says so, and
	// its internal shape is not part of any contract.
	NextCursor string `json:"nextCursor"`
}

// StatusParams paginates GET /status/{address}. A zero value asks for the
// first page at the server's default size.
//
// There is deliberately no paginate parameter. The published schema allows
// only the single value "true", the server does not enforce even that, and
// new integrations are told to omit it.
type StatusParams struct {
	// Limit is the requested maximum rows per page, 1 to 100, defaulting to
	// 50 server-side. Anything outside that range is rejected with 400.
	//
	// It does NOT cap the first page. Transfers with neither a hash nor a
	// timestamp cannot be named by a cursor, so they ride outside pagination
	// and are prepended to page one: limit=2 was observed returning 3 rows.
	// Never size a buffer from Limit and never treat a longer page as an
	// error.
	Limit int
	// Cursor continues a walk. Leave it empty for the first page, then pass
	// the previous page's NextCursor. A stale or malformed cursor is answered
	// with 400.
	Cursor string
}

// ---------------------------------------------------------------------------
// Methods.

// SupportedAssets lists every chain and token the bridge accepts, with each
// chain's minimum deposit. It needs no authentication.
//
// GET /supported-assets
func (c *Client) SupportedAssets(ctx context.Context) (SupportedAssetsResponse, error) {
	var out SupportedAssetsResponse
	err := c.session.Get(ctx, epSupportedAssets, nil, &out)
	return out, err
}

// Quote estimates the output, duration and fees of a bridge or swap. It needs
// no authentication, and it moves nothing: the reply is an estimate with no
// executable transaction in it, and the figures may move before a transfer
// lands.
//
// POST /quote
func (c *Client) Quote(ctx context.Context, req QuoteRequest) (QuoteResponse, error) {
	var out QuoteResponse
	err := c.post(ctx, epQuote, req, &out)
	return out, err
}

// Deposit creates the bridge deposit addresses for one Polymarket wallet, one
// per chain family. Funds sent to them are bridged, swapped and credited to
// that wallet. It needs no authentication.
//
// address is the Polymarket wallet to credit, as 0x-hex. The optional
// X-Builder-Code attribution header is not sent — see this file's notes on
// authentication — which the bridge accepts.
//
// POST /deposit
func (c *Client) Deposit(ctx context.Context, address string) (DepositResponse, error) {
	var out DepositResponse
	err := c.post(ctx, epDeposit, depositRequest{Address: address}, &out)
	return out, err
}

// Withdraw creates a withdrawal address bound to one destination chain, token
// and recipient. Send pUSD from the Polymarket wallet to the returned EVM
// address and it is unwrapped and delivered. Polymarket charges no fee of its
// own on a withdrawal.
//
// The address returned is single-purpose: create one when the withdrawal is
// ready, never in advance. It needs no authentication, and the optional
// X-Builder-Code attribution header is not sent.
//
// POST /withdraw
func (c *Client) Withdraw(ctx context.Context, req WithdrawRequest) (DepositResponse, error) {
	var out DepositResponse
	err := c.post(ctx, epWithdraw, req, &out)
	return out, err
}

// Status reports one page of the deposits and withdrawals seen at a bridge
// address, newest first. It needs no authentication.
//
// address is a bridge address returned by Deposit or Withdraw, not a
// Polymarket wallet address. EVM, Solana, Tron and Bitcoin forms are all
// accepted.
//
// Walk a history by passing each StatusPage.NextCursor back as
// StatusParams.Cursor until NextCursor is empty. Do not stop early on a short
// or empty page.
//
// The failure codes are not what they look like. A syntactically invalid
// address gives 400; an address with no history gives 200 and an empty page;
// but a well-formed address the bridge has never seen gives 500. A 500 from
// this endpoint therefore does not mean the server is broken and should not
// be blindly retried.
//
// GET /status/{address}
func (c *Client) Status(ctx context.Context, address string, p StatusParams) (StatusPage, error) {
	q := url.Values{}
	if p.Limit != 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	var out StatusPage
	// A cursor carries +, / and =, which url.Values escapes on the way out.
	// The address is escaped for the same reason: it is caller data landing
	// in a path segment.
	err := c.session.Get(ctx, epStatus+"/"+url.PathEscape(address), q, &out)
	return out, err
}

// post issues one of this package's three public POSTs. It uses session.Do
// rather than a PostL2 helper because these endpoints take a body but no
// authentication — AuthNone, polymarket.Request's zero value, is the level a
// public POST needs.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	req := polymarket.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
		Out:    out,
	}
	return c.session.Do(ctx, req)
}

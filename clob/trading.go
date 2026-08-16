// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package clob

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	polymarket "github.com/ChloePike/go-polymarket"
)

// This file is the trading surface: the API-key lifecycle, order submission,
// cancellation, and the queries that report what an account has resting or
// filled. Everything here moves money or the permission to move it.

// AssetType names which balance an allowance query is about.
type AssetType string

const (
	// Collateral is USDC, the currency side of every market.
	Collateral AssetType = "COLLATERAL"
	// Conditional is an outcome token, the share side.
	Conditional AssetType = "CONDITIONAL"
)

// CreateAPIKey asks the exchange for new level-2 credentials, proving control
// of the wallet with a level-1 signature. It needs a Signer.
//
// The credentials are deterministic per wallet and nonce: calling this twice
// returns the same key, and DeriveAPIKey recovers it without creating
// anything. On success the credentials are stored on the Client.
func (c *Client) CreateAPIKey(ctx context.Context, nonce int64) (polymarket.APICreds, error) {
	return c.apiKeyRequest(ctx, http.MethodPost, epCreateAPIKey, nonce)
}

// DeriveAPIKey recovers the level-2 credentials a wallet already has, without
// creating anything. It needs a Signer. On success the credentials are stored
// on the Client.
func (c *Client) DeriveAPIKey(ctx context.Context, nonce int64) (polymarket.APICreds, error) {
	return c.apiKeyRequest(ctx, http.MethodGet, epDeriveAPIKey, nonce)
}

// CreateOrDeriveAPIKey obtains level-2 credentials by whichever route works:
// it creates a key, and falls back to deriving the existing one. This is the
// call to make at startup. It needs a Signer, and stores the credentials on
// the Client so later requests authenticate themselves.
func (c *Client) CreateOrDeriveAPIKey(ctx context.Context) (polymarket.APICreds, error) {
	creds, createErr := c.CreateAPIKey(ctx, 0)
	if createErr == nil && creds.Key != "" {
		return creds, nil
	}
	creds, deriveErr := c.DeriveAPIKey(ctx, 0)
	if deriveErr == nil && creds.Key != "" {
		return creds, nil
	}
	if createErr != nil {
		return polymarket.APICreds{}, fmt.Errorf("polymarket: creating api key: %w (deriving also failed: %v)",
			createErr, deriveErr)
	}
	return polymarket.APICreds{}, fmt.Errorf("polymarket: deriving api key: %w", deriveErr)
}

// apiKeyRequest performs one level-1 key-management call and, when it yields
// usable credentials, adopts them.
func (c *Client) apiKeyRequest(ctx context.Context, method, path string, nonce int64) (polymarket.APICreds, error) {
	if c.session.Signer() == nil {
		return polymarket.APICreds{}, polymarket.ErrNoSigner
	}
	var creds polymarket.APICreds
	if err := c.session.Do(ctx, polymarket.Request{
		Method: method,
		Path:   path,
		Query:  nonceQuery(nonce),
		Auth:   polymarket.AuthL1,
		Out:    &creds,
	}); err != nil {
		return polymarket.APICreds{}, err
	}
	if creds.Key != "" {
		c.session.SetCredentials(creds)
	}
	return creds, nil
}

// nonceQuery carries a non-default nonce, which selects between several key
// pairs for one wallet. Zero is the default and is left off the wire.
func nonceQuery(nonce int64) url.Values {
	if nonce == 0 {
		return nil
	}
	return url.Values{"nonce": {strconv.FormatInt(nonce, 10)}}
}

// APIKeysResponse lists the keys a wallet holds.
type APIKeysResponse struct {
	APIKeys []string `json:"apiKeys"`
}

// APIKeys lists the level-2 keys the account holds. Level 2.
func (c *Client) APIKeys(ctx context.Context) (APIKeysResponse, error) {
	var out APIKeysResponse
	err := c.session.GetL2(ctx, epGetAPIKeys, nil, &out)
	return out, err
}

// DeleteAPIKey revokes the credentials the Client is using. Level 2. After
// this the stored credentials no longer authenticate anything.
func (c *Client) DeleteAPIKey(ctx context.Context) error {
	return c.session.DeleteL2(ctx, epDeleteAPIKey, nil, nil)
}

// BanStatus reports whether an account is restricted to closing trades.
type BanStatus struct {
	ClosedOnly bool `json:"closed_only"`
}

// ClosedOnly reports whether the account may only reduce positions rather than
// open new ones. Level 2.
func (c *Client) ClosedOnly(ctx context.Context) (BanStatus, error) {
	var out BanStatus
	err := c.session.GetL2(ctx, epClosedOnly, nil, &out)
	return out, err
}

// ReadonlyAPIKey is a credential that can read an account but not trade on it.
type ReadonlyAPIKey struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// CreateReadonlyAPIKey issues a credential that can read the account but not
// trade on it. Level 2.
func (c *Client) CreateReadonlyAPIKey(ctx context.Context) (ReadonlyAPIKey, error) {
	var out ReadonlyAPIKey
	err := c.session.PostL2(ctx, epCreateReadonlyKey, nil, &out)
	return out, err
}

// ReadonlyAPIKeys lists the read-only credentials the account has issued.
// Level 2.
func (c *Client) ReadonlyAPIKeys(ctx context.Context) ([]string, error) {
	var out []string
	err := c.session.GetL2(ctx, epGetReadonlyKeys, nil, &out)
	return out, err
}

// DeleteReadonlyAPIKey revokes one read-only credential. Level 2.
func (c *Client) DeleteReadonlyAPIKey(ctx context.Context, key string) error {
	return c.session.Do(ctx, polymarket.Request{
		Method: http.MethodDelete,
		Path:   epDeleteReadonlyKey,
		Query:  url.Values{"api_key": {key}},
		Auth:   polymarket.AuthL2,
	})
}

// BuilderAPIKey is the credential a builder uses to read its attribution.
type BuilderAPIKey struct {
	Key         string `json:"apiKey"`
	Secret      string `json:"secret"`
	Passphrase  string `json:"passphrase"`
	BuilderCode string `json:"builderCode"`
}

// CreateBuilderAPIKey issues a builder credential for the account. Level 2.
func (c *Client) CreateBuilderAPIKey(ctx context.Context) (BuilderAPIKey, error) {
	var out BuilderAPIKey
	err := c.session.PostL2(ctx, epCreateBuilderKey, nil, &out)
	return out, err
}

// BuilderAPIKeys lists the account's builder credentials. Level 2.
func (c *Client) BuilderAPIKeys(ctx context.Context) ([]BuilderAPIKey, error) {
	var out []BuilderAPIKey
	err := c.session.GetL2(ctx, epGetBuilderKeys, nil, &out)
	return out, err
}

// RevokeBuilderAPIKey revokes the account's builder credential. Level 2.
func (c *Client) RevokeBuilderAPIKey(ctx context.Context) error {
	return c.session.DeleteL2(ctx, epRevokeBuilderKey, nil, nil)
}

// CreateOrder resolves a UserOrder into a signed order, ready for PostOrder.
//
// It fills in whatever OrderOptions left blank by asking the exchange: an
// empty TickSize is fetched for the token, and NegRisk is resolved unless the
// caller set it. Those two decide the rounding and the verifying contract, so
// a wrong value produces an order the exchange refuses. It needs a Signer.
//
// Nothing is submitted here. The returned order is signed and therefore
// authorises a trade, but only PostOrder sends it.
func (c *Client) CreateOrder(ctx context.Context, u polymarket.UserOrder, opts polymarket.OrderOptions) (polymarket.SignedOrder, error) {
	if c.session.Signer() == nil {
		return polymarket.SignedOrder{}, polymarket.ErrNoSigner
	}
	opts, err := c.resolveMarket(ctx, u.TokenID, opts)
	if err != nil {
		return polymarket.SignedOrder{}, err
	}
	order, err := polymarket.BuildOrder(u, c.session.Signer().Address(), opts)
	if err != nil {
		return polymarket.SignedOrder{}, err
	}
	return polymarket.SignOrder(order, c.session.ChainID(), opts, c.session.Signer())
}

// CreateMarketOrder resolves a MarketOrder into a signed order. A market buy
// is sized in USDC and a market sell in shares.
//
// When Price is empty it is filled from the book with MarketPrice, so a caller
// can name an amount and let the client find the marketable price. As with
// CreateOrder, nothing is submitted. It needs a Signer.
func (c *Client) CreateMarketOrder(ctx context.Context, m polymarket.MarketOrder, opts polymarket.OrderOptions) (polymarket.SignedOrder, error) {
	if c.session.Signer() == nil {
		return polymarket.SignedOrder{}, polymarket.ErrNoSigner
	}
	opts, err := c.resolveMarket(ctx, m.TokenID, opts)
	if err != nil {
		return polymarket.SignedOrder{}, err
	}
	if m.Price == "" {
		price, err := c.MarketPrice(ctx, m.TokenID, m.Side, m.Amount, polymarket.FOK)
		if err != nil {
			return polymarket.SignedOrder{}, fmt.Errorf("polymarket: pricing market order: %w", err)
		}
		m.Price = price
	}
	order, err := polymarket.BuildMarketOrder(m, c.session.Signer().Address(), opts)
	if err != nil {
		return polymarket.SignedOrder{}, err
	}
	return polymarket.SignOrder(order, c.session.ChainID(), opts, c.session.Signer())
}

// resolveMarket fills in the market facts an order needs when the caller left
// them unset.
func (c *Client) resolveMarket(ctx context.Context, tokenID string, opts polymarket.OrderOptions) (polymarket.OrderOptions, error) {
	if opts.TickSize == "" {
		tick, err := c.TickSize(ctx, tokenID)
		if err != nil {
			return opts, fmt.Errorf("polymarket: resolving tick size: %w", err)
		}
		opts.TickSize = tick
	}
	if !opts.NegRisk {
		negRisk, err := c.NegRisk(ctx, tokenID)
		if err != nil {
			return opts, fmt.Errorf("polymarket: resolving neg-risk flag: %w", err)
		}
		opts.NegRisk = negRisk
	}
	return opts, nil
}

// wireOrder is the order object inside a submission body.
//
// Salt is a JSON number here while every other numeric field is a string.
// That asymmetry is the wire format, not an oversight, and it is why
// randomSalt keeps the salt below 2^52: a parser reading this number as a
// float64 must recover exactly the value that was signed.
type wireOrder struct {
	Salt          int64                    `json:"salt"`
	Maker         string                   `json:"maker"`
	Signer        string                   `json:"signer"`
	Taker         string                   `json:"taker"`
	TokenID       string                   `json:"tokenId"`
	MakerAmount   string                   `json:"makerAmount"`
	TakerAmount   string                   `json:"takerAmount"`
	Side          polymarket.Side          `json:"side"`
	SignatureType polymarket.SignatureType `json:"signatureType"`
	Timestamp     string                   `json:"timestamp"`
	Expiration    string                   `json:"expiration"`
	Metadata      string                   `json:"metadata"`
	Builder       string                   `json:"builder"`
	Signature     string                   `json:"signature"`
}

// postOrderRequest is the body of a single-order submission.
type postOrderRequest struct {
	DeferExec bool                 `json:"deferExec"`
	PostOnly  bool                 `json:"postOnly"`
	Order     wireOrder            `json:"order"`
	Owner     string               `json:"owner"`
	OrderType polymarket.OrderType `json:"orderType"`
}

// OrderResponse is what the exchange says about a submitted order.
type OrderResponse struct {
	Success  bool   `json:"success"`
	ErrorMsg string `json:"errorMsg,omitempty"`
	OrderID  string `json:"orderID,omitempty"`
	Status   string `json:"status,omitempty"`
	// TakingAmount and MakingAmount report what matched immediately, as
	// integer strings at six decimals.
	TakingAmount string `json:"takingAmount,omitempty"`
	MakingAmount string `json:"makingAmount,omitempty"`
	// TradeIDs identifies the trades an immediate match created.
	TradeIDs []string `json:"tradeIDs,omitempty"`
	// TransactionsHashes are the settlement transactions, supplied on a
	// best-effort basis: a match that has not settled yet reports none.
	TransactionsHashes []string `json:"transactionsHashes,omitempty"`
}

// SubmitOptions modify how an order is submitted rather than what it says.
type SubmitOptions struct {
	// PostOnly rejects the order rather than let it take liquidity, keeping
	// it a maker. The exchange refuses it on a FOK or FAK order, which exist
	// to take.
	PostOnly bool

	// DeferExec asks the exchange to accept the order without waiting for
	// settlement, so the response arrives before the transaction hashes do.
	DeferExec bool
}

// ErrPostOnlyMarketOrder reports a combination the exchange always rejects:
// a fill-or-kill or fill-and-kill order exists to take liquidity, so it
// cannot also be post-only.
var ErrPostOnlyMarketOrder = fmt.Errorf("polymarket: postOnly cannot be used with a FOK or FAK order")

// PostOrder submits one signed order. Level 2, and the account must hold the
// credentials the order will be attributed to.
//
// This spends money. The order is live on the book the moment it is accepted,
// and a marketable one may fill before this call returns.
func (c *Client) PostOrder(ctx context.Context, o polymarket.SignedOrder, orderType polymarket.OrderType, opts SubmitOptions) (OrderResponse, error) {
	body, err := submissionBody(o, orderType, c.owner(), opts)
	if err != nil {
		return OrderResponse{}, err
	}
	var out OrderResponse
	err = c.session.PostL2(ctx, epPostOrder, body, &out)
	return out, err
}

// OrderSubmission pairs a signed order with the time in force it is submitted
// under, for PostOrders.
type OrderSubmission struct {
	Order     polymarket.SignedOrder
	OrderType polymarket.OrderType
}

// PostOrders submits several signed orders in one request. Level 2.
//
// This spends money, once per order. The exchange answers with one response
// per submission, in the same order.
func (c *Client) PostOrders(ctx context.Context, orders []OrderSubmission, opts SubmitOptions) ([]OrderResponse, error) {
	if len(orders) == 0 {
		return nil, fmt.Errorf("polymarket: PostOrders needs at least one order")
	}
	owner := c.owner()
	bodies := make([]postOrderRequest, 0, len(orders))
	for i, s := range orders {
		body, err := submissionBody(s.Order, s.OrderType, owner, opts)
		if err != nil {
			return nil, fmt.Errorf("polymarket: order %d: %w", i, err)
		}
		bodies = append(bodies, body)
	}
	var out []OrderResponse
	err := c.session.PostL2(ctx, epPostOrders, bodies, &out)
	return out, err
}

// submissionBody renders a signed order as the exchange expects it.
func submissionBody(o polymarket.SignedOrder, orderType polymarket.OrderType, owner string, opts SubmitOptions) (postOrderRequest, error) {
	if opts.PostOnly && (orderType == polymarket.FOK || orderType == polymarket.FAK) {
		return postOrderRequest{}, ErrPostOnlyMarketOrder
	}
	if o.Signature == "" {
		return postOrderRequest{}, fmt.Errorf("polymarket: order is not signed")
	}
	salt, err := strconv.ParseInt(o.Salt, 10, 64)
	if err != nil {
		return postOrderRequest{}, fmt.Errorf("polymarket: order salt %q is not an integer: %w", o.Salt, err)
	}
	return postOrderRequest{
		DeferExec: opts.DeferExec,
		PostOnly:  opts.PostOnly,
		Owner:     owner,
		OrderType: orderType,
		Order: wireOrder{
			Salt:          salt,
			Maker:         o.Maker,
			Signer:        o.Signer,
			Taker:         o.Taker,
			TokenID:       o.TokenID,
			MakerAmount:   o.MakerAmount,
			TakerAmount:   o.TakerAmount,
			Side:          o.Side,
			SignatureType: o.SignatureType,
			Timestamp:     o.Timestamp,
			Expiration:    o.Expiration,
			Metadata:      o.Metadata,
			Builder:       o.Builder,
			Signature:     o.Signature,
		},
	}, nil
}

// owner is the API key an order is attributed to.
func (c *Client) owner() string {
	creds := c.session.Credentials()
	if creds == nil {
		return ""
	}
	return creds.Key
}

// CancelResponse reports which cancellations took effect.
type CancelResponse struct {
	// Canceled lists the order ids that are now cancelled.
	Canceled []string `json:"canceled"`
	// NotCanceled maps an order id to why it survived, usually because it
	// had already filled or already been cancelled.
	NotCanceled map[string]string `json:"not_canceled"`
}

// cancelOrderRequest names one order to cancel.
type cancelOrderRequest struct {
	OrderID string `json:"orderID"`
}

// CancelOrder cancels one resting order. Level 2.
func (c *Client) CancelOrder(ctx context.Context, orderID string) (CancelResponse, error) {
	var out CancelResponse
	err := c.session.DeleteL2(ctx, epCancelOrder, cancelOrderRequest{OrderID: orderID}, &out)
	return out, err
}

// CancelOrders cancels several orders by id in one request. Level 2.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) (CancelResponse, error) {
	var out CancelResponse
	// The body is a bare JSON array of ids, not an object wrapping them.
	err := c.session.DeleteL2(ctx, epCancelOrders, orderIDs, &out)
	return out, err
}

// CancelAll cancels every resting order the account has, across all markets.
// Level 2.
func (c *Client) CancelAll(ctx context.Context) (CancelResponse, error) {
	var out CancelResponse
	err := c.session.DeleteL2(ctx, epCancelAll, nil, &out)
	return out, err
}

// MarketCancelParams selects the orders CancelMarketOrders removes. Set one
// of the two: Market is a condition id, AssetID an outcome token id.
type MarketCancelParams struct {
	Market  string `json:"market,omitempty"`
	AssetID string `json:"asset_id,omitempty"`
}

// CancelMarketOrders cancels every resting order the account has in one
// market. Level 2.
func (c *Client) CancelMarketOrders(ctx context.Context, p MarketCancelParams) (CancelResponse, error) {
	if p.Market == "" && p.AssetID == "" {
		return CancelResponse{}, fmt.Errorf("polymarket: CancelMarketOrders needs a market or an asset id")
	}
	var out CancelResponse
	err := c.session.DeleteL2(ctx, epCancelMarketOrders, p, &out)
	return out, err
}

// An OpenOrder is one of the account's orders as the exchange holds it.
type OpenOrder struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	Owner           string          `json:"owner"`
	MakerAddress    string          `json:"maker_address"`
	Market          string          `json:"market"`
	AssetID         string          `json:"asset_id"`
	Side            polymarket.Side `json:"side"`
	OriginalSize    string          `json:"original_size"`
	SizeMatched     string          `json:"size_matched"`
	Price           string          `json:"price"`
	AssociateTrades []string        `json:"associate_trades"`
	Outcome         string          `json:"outcome"`
	CreatedAt       int64           `json:"created_at"`
	Expiration      string          `json:"expiration"`
	OrderType       string          `json:"order_type"`
}

// Order reports one of the account's orders by id. Level 2.
func (c *Client) Order(ctx context.Context, orderID string) (OpenOrder, error) {
	var out OpenOrder
	err := c.session.GetL2(ctx, epGetOrder+orderID, nil, &out)
	return out, err
}

// OpenOrderParams filters a listing of the account's resting orders. An empty
// value lists them all.
type OpenOrderParams struct {
	ID      string
	Market  string
	AssetID string
}

func (p OpenOrderParams) query() url.Values {
	q := url.Values{}
	setIf(q, "id", p.ID)
	setIf(q, "market", p.Market)
	setIf(q, "asset_id", p.AssetID)
	return q
}

// openOrdersPage is the envelope /data/orders serves.
//
// The official SDK types this endpoint as a bare array. Production returns a
// paginated object, verified live, so decoding into a slice fails.
type openOrdersPage struct {
	Data []OpenOrder `json:"data"`
	Pagination
}

// OpenOrders lists one page of the account's resting orders. Level 2. Pass
// CursorStart, or "", for the first page; use Pages to walk them all.
func (c *Client) OpenOrders(ctx context.Context, p OpenOrderParams, cursor string) ([]OpenOrder, Pagination, error) {
	q := p.query()
	q.Set("next_cursor", cursorOrStart(cursor))
	var page openOrdersPage
	if err := c.session.GetL2(ctx, epOpenOrders, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// PreMigrationOrders lists orders left on the book by an earlier exchange
// version. Level 2.
func (c *Client) PreMigrationOrders(ctx context.Context, cursor string) ([]OpenOrder, Pagination, error) {
	q := url.Values{"next_cursor": {cursorOrStart(cursor)}}
	var page openOrdersPage
	if err := c.session.GetL2(ctx, epPreMigrationOrders, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// A MakerOrder is one resting order that a trade matched against.
type MakerOrder struct {
	OrderID      string `json:"order_id"`
	Owner        string `json:"owner"`
	MakerAddress string `json:"maker_address"`
	MatchedSize  string `json:"matched_amount"`
	Price        string `json:"price"`
	FeeRateBps   string `json:"fee_rate_bps"`
	AssetID      string `json:"asset_id"`
	Outcome      string `json:"outcome"`
}

// A Trade is one fill the account took part in.
type Trade struct {
	ID              string          `json:"id"`
	TakerOrderID    string          `json:"taker_order_id"`
	Market          string          `json:"market"`
	AssetID         string          `json:"asset_id"`
	Side            polymarket.Side `json:"side"`
	Size            string          `json:"size"`
	FeeRateBps      string          `json:"fee_rate_bps"`
	Price           string          `json:"price"`
	Status          string          `json:"status"`
	MatchTime       string          `json:"match_time"`
	LastUpdate      string          `json:"last_update"`
	Outcome         string          `json:"outcome"`
	BucketIndex     int             `json:"bucket_index"`
	Owner           string          `json:"owner"`
	MakerAddress    string          `json:"maker_address"`
	MakerOrders     []MakerOrder    `json:"maker_orders"`
	TransactionHash string          `json:"transaction_hash,omitempty"`
	ErrMsg          string          `json:"err_msg,omitempty"`
	// TraderSide is TAKER when the account crossed the spread and MAKER when
	// it was rested on the book.
	TraderSide string `json:"trader_side"`
}

// TradeParams filters a listing of the account's fills.
type TradeParams struct {
	ID           string
	MakerAddress string
	Market       string
	AssetID      string
	// Before and After bound the match time, as unix-seconds strings.
	Before string
	After  string
}

func (p TradeParams) query() url.Values {
	q := url.Values{}
	setIf(q, "id", p.ID)
	setIf(q, "maker_address", p.MakerAddress)
	setIf(q, "market", p.Market)
	setIf(q, "asset_id", p.AssetID)
	setIf(q, "before", p.Before)
	setIf(q, "after", p.After)
	return q
}

// tradesPage is the envelope /data/trades serves.
type tradesPage struct {
	Data []Trade `json:"data"`
	Pagination
}

// Trades lists one page of the account's fills. Level 2. Use Pages to walk
// every page.
func (c *Client) Trades(ctx context.Context, p TradeParams, cursor string) ([]Trade, Pagination, error) {
	q := p.query()
	q.Set("next_cursor", cursorOrStart(cursor))
	var page tradesPage
	if err := c.session.GetL2(ctx, epTrades, q, &page); err != nil {
		return nil, Pagination{}, err
	}
	return page.Data, page.Pagination, nil
}

// OrderScoring reports whether an order counts toward liquidity rewards.
type OrderScoring struct {
	Scoring bool `json:"scoring"`
}

// IsOrderScoring reports whether one order is earning liquidity rewards.
// Level 2.
func (c *Client) IsOrderScoring(ctx context.Context, orderID string) (OrderScoring, error) {
	var out OrderScoring
	err := c.session.GetL2(ctx, epOrderScoring, url.Values{"order_id": {orderID}}, &out)
	return out, err
}

// OrdersScoring reports, per order id, whether it is earning rewards.
type OrdersScoring map[string]bool

// AreOrdersScoring reports which of several orders are earning liquidity
// rewards. Level 2.
func (c *Client) AreOrdersScoring(ctx context.Context, orderIDs []string) (OrdersScoring, error) {
	var out OrdersScoring
	// The body is a bare JSON array of ids.
	err := c.session.PostL2(ctx, epOrdersScoring, orderIDs, &out)
	return out, err
}

// BalanceAllowance reports a balance and what the exchange contracts are
// allowed to move on the account's behalf. An allowance of zero blocks
// trading even when the balance is ample.
type BalanceAllowance struct {
	Balance    string            `json:"balance"`
	Allowances map[string]string `json:"allowances"`
}

// BalanceAllowanceParams selects which balance to report. TokenID is required
// for Conditional and ignored for Collateral.
type BalanceAllowanceParams struct {
	AssetType AssetType
	TokenID   string
}

func (p BalanceAllowanceParams) query(sig polymarket.SignatureType) url.Values {
	q := url.Values{"asset_type": {string(p.AssetType)}}
	setIf(q, "token_id", p.TokenID)
	q.Set("signature_type", strconv.Itoa(int(sig)))
	return q
}

// BalanceAllowance reports the account's balance and allowance for one asset.
// Level 2.
func (c *Client) BalanceAllowance(ctx context.Context, p BalanceAllowanceParams, sig polymarket.SignatureType) (BalanceAllowance, error) {
	var out BalanceAllowance
	err := c.session.GetL2(ctx, epBalanceAllowance, p.query(sig), &out)
	return out, err
}

// UpdateBalanceAllowance asks the exchange to re-read the account's on-chain
// balance and allowance. Level 2.
//
// Despite updating something, this is a GET: the exchange treats it as a
// refresh of its own cache rather than a change to the account.
func (c *Client) UpdateBalanceAllowance(ctx context.Context, p BalanceAllowanceParams, sig polymarket.SignatureType) error {
	return c.session.GetL2(ctx, epUpdateBalance, p.query(sig), nil)
}

// A Notification is one message the exchange has for the account.
type Notification struct {
	Type  int    `json:"type"`
	Owner string `json:"owner"`
	// Payload's shape depends on Type, so it is left as raw JSON rather than
	// guessed at.
	Payload map[string]any `json:"payload"`
}

// Notifications lists the account's undismissed notifications. Level 2.
func (c *Client) Notifications(ctx context.Context, sig polymarket.SignatureType) ([]Notification, error) {
	var out []Notification
	q := url.Values{"signature_type": {strconv.Itoa(int(sig))}}
	err := c.session.GetL2(ctx, epNotifications, q, &out)
	return out, err
}

// DropNotifications dismisses notifications by id. Level 2.
func (c *Client) DropNotifications(ctx context.Context, ids []string) error {
	q := url.Values{}
	for _, id := range ids {
		q.Add("ids", id)
	}
	return c.session.Do(ctx, polymarket.Request{
		Method: http.MethodDelete,
		Path:   epNotifications,
		Query:  q,
		Auth:   polymarket.AuthL2,
	})
}

// FeeCurve is a market's platform fee curve: the two numbers that
// AdjustBuyAmountForFees needs.
type FeeCurve struct {
	// Rate is the base fee rate as a fraction.
	Rate float64
	// Exponent shapes how the rate scales with distance from an even price.
	Exponent float64
}

// FeeCurve reports a token's platform fee curve. No authentication.
//
// The exchange does not serve this per token, so it takes two hops: the token
// resolves to a condition id, and the condition id to the market summary that
// carries the curve. Feed the result to AdjustBuyAmountForFees.
func (c *Client) FeeCurve(ctx context.Context, tokenID string) (FeeCurve, error) {
	byToken, err := c.MarketByToken(ctx, tokenID)
	if err != nil {
		return FeeCurve{}, fmt.Errorf("polymarket: resolving condition id for token %s: %w", tokenID, err)
	}
	if byToken.ConditionID == "" {
		return FeeCurve{}, fmt.Errorf("polymarket: token %s has no condition id", tokenID)
	}
	market, err := c.ClobMarket(ctx, byToken.ConditionID)
	if err != nil {
		return FeeCurve{}, err
	}
	if market.Fee == nil {
		return FeeCurve{}, fmt.Errorf("polymarket: market %s reports no fee curve", byToken.ConditionID)
	}
	return FeeCurve{Rate: market.Fee.Rate, Exponent: market.Fee.Exponent}, nil
}

// APIVersion reports the order version the exchange currently accepts. No
// authentication.
//
// An order signed for a version the exchange has moved off is rejected with
// order_version_mismatch; this is what tells a caller to rebuild and re-sign
// rather than retry the same bytes.
func (c *Client) APIVersion(ctx context.Context) (int, error) {
	var out apiVersionResponse
	if err := c.session.Get(ctx, epVersion, nil, &out); err != nil {
		return 0, err
	}
	return out.Version, nil
}

// apiVersionResponse is the body of GET /version.
type apiVersionResponse struct {
	Version int `json:"version"`
}

// Heartbeat tells the exchange the client is still alive, which keeps orders
// that opted into a heartbeat from being cancelled. Level 2.
func (c *Client) Heartbeat(ctx context.Context, heartbeatID string) error {
	q := url.Values{}
	setIf(q, "heartbeat_id", heartbeatID)
	return c.session.Do(ctx, polymarket.Request{
		Method: http.MethodPost,
		Path:   epHeartbeat,
		Query:  q,
		Auth:   polymarket.AuthL2,
	})
}

// setIf adds a query parameter only when it has a value, so an unset filter
// stays off the wire rather than being sent as an empty string.
func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

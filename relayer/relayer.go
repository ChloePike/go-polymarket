// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package relayer

// This file holds the relayer's read endpoints and every shape they return.
//
// WHICH CALLS NEED CREDENTIALS: Nonce, RelayPayload, Deployed and Transaction
// are public — the relayer answers them with no headers at all, and it never
// checks who is asking, so anyone holding a transaction id can read that
// transaction. Transactions and APIKeys identify the caller entirely by the
// credential headers; neither takes an address parameter, and passing one
// does not stand in for authentication.
//
// EVERYTHING IS A STRING: there is exactly one JSON number or boolean in the
// whole relayer API, DeployedResponse.Deployed. A nonce is the decimal string
// "31", not 31, and Transaction.Value — a wei amount — is a string whose
// documented example is "". They are declared here as the strings the wire
// carries, so no amount ever passes through a float64.
//
// THE TYPE PARAMETER IS THREE VALUES, NOT TWO: the published enum for Nonce
// and RelayPayload is PROXY|SAFE, but WalletTypeWallet — the deposit wallet,
// signature type 3 — is whitelisted server side and answers with a third,
// distinct relayer address. A client that admits only PROXY and SAFE locks
// callers out of deposit wallets. The value is uppercase and case sensitive
// everywhere: "proxy" is a 400, not a synonym.
//
// TWO TRAPS IN THE PUBLIC CALLS, both of which return a confident wrong
// answer rather than an error:
//
//   - Deployed does not validate the type parameter. An unrecognised value
//     silently falls back to SAFE, so a typo is answered as if it were a
//     question about the Safe. Nonce and RelayPayload do validate it and
//     answer 400.
//   - Neither Nonce nor Deployed validates the address beyond requiring one.
//     A garbage address reads back nonce "0" and deployed false, which is
//     also what a fresh, never-used account reads back. Never take nonce "0"
//     as proof the address was understood.
//
// A 404 FROM Transaction IS NOT PROOF OF ABSENCE: a malformed id answers 404
// "transaction not found", the same as a well-formed id nobody has. Check the
// id before concluding the transaction does not exist.
//
// RATE LIMITS: Polymarket publishes a limit for the relayer's submit endpoint
// and none for any read here, so the session paces none of these calls. That
// is the absence of a published figure, not a licence to poll hard.

import (
	"context"
	"net/http"
	"net/url"

	polymarket "github.com/ChloePike/go-polymarket"
)

// Relayer paths. Every one is a bare path on RelayerHost; the API keys
// endpoint is the only one not at the root.
const (
	epNonce        = "/nonce"
	epRelayPayload = "/relay-payload"
	epTransaction  = "/transaction"
	epTransactions = "/transactions"
	epDeployed     = "/deployed"
	epAPIKeys      = "/relayer/api/keys"
)

// A WalletType names which of a user's wallets a call is about. Polymarket
// gives an account up to three, and they have separate nonces, separate
// relayer addresses and separate deployment states — an answer about one says
// nothing about another.
type WalletType string

const (
	// WalletTypeProxy is the legacy proxy wallet.
	WalletTypeProxy WalletType = "PROXY"
	// WalletTypeSafe is the Gnosis Safe, signature type 2. It is what
	// Deployed assumes when no type is given.
	WalletTypeSafe WalletType = "SAFE"
	// WalletTypeWallet is the deposit wallet, signature type 3. The relayer
	// accepts and answers it, but Polymarket's specification does not list
	// it.
	WalletTypeWallet WalletType = "WALLET"
)

// A TransactionState is how far a relayed transaction has got. The relayer
// defines six; treat anything else as unrecognised rather than assuming it
// is terminal.
type TransactionState string

const (
	// TransactionStateNew is accepted by the relayer, not yet broadcast.
	TransactionStateNew TransactionState = "STATE_NEW"
	// TransactionStateExecuted has been broadcast.
	TransactionStateExecuted TransactionState = "STATE_EXECUTED"
	// TransactionStateMined is in a block.
	TransactionStateMined TransactionState = "STATE_MINED"
	// TransactionStateConfirmed is in a block the relayer considers final.
	TransactionStateConfirmed TransactionState = "STATE_CONFIRMED"
	// TransactionStateInvalid was rejected before broadcast.
	TransactionStateInvalid TransactionState = "STATE_INVALID"
	// TransactionStateFailed reverted or was dropped.
	TransactionStateFailed TransactionState = "STATE_FAILED"
)

// A Transaction is one transaction the relayer has been asked to broadcast.
//
// Every field is a string on the wire, including Nonce and Value. Poll for
// one with Client.Transaction and the id the submit endpoint returned:
// TransactionHash is empty until the transaction is broadcast.
type Transaction struct {
	// TransactionID is the relayer's own id, the value Client.Transaction
	// looks a transaction up by. A UUID in practice, though the relayer
	// neither documents nor enforces that.
	TransactionID string `json:"transactionID"`
	// TransactionHash is the on-chain hash, 0x-prefixed hex. It is empty
	// until the relayer broadcasts the transaction.
	TransactionHash string `json:"transactionHash"`
	// From is the signer address.
	From string `json:"from"`
	// To is the contract the transaction calls.
	To string `json:"to"`
	// ProxyAddress is the user's proxy or Safe wallet.
	ProxyAddress string `json:"proxyAddress"`
	// Data is the encoded calldata, 0x-prefixed hex.
	Data string `json:"data"`
	// Nonce is the wallet nonce the transaction was signed at, as a decimal
	// string. It is a uint256, which is why it is not an integer here.
	Nonce string `json:"nonce"`
	// Value is the transaction value in wei, as a decimal string. The
	// relayer's own example is the empty string.
	Value string `json:"value"`
	// Signature is the user's signature over the operation, 0x-prefixed hex.
	Signature string `json:"signature"`
	// State is how far the transaction has got. See TransactionState.
	State TransactionState `json:"state"`
	// Type is the wallet kind the transaction went through. The relayer
	// documents SAFE and PROXY here.
	Type WalletType `json:"type"`
	// Owner is the address that owns the wallet.
	Owner string `json:"owner"`
	// Metadata is free-form text the submitter attached.
	Metadata string `json:"metadata"`
	// CreatedAt is when the relayer accepted the transaction, RFC 3339 with
	// microseconds.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is when the relayer last changed the record, in the same
	// format.
	UpdatedAt string `json:"updatedAt"`
}

// NonceResponse is the wire shape of the nonce endpoint. Client.Nonce
// returns its single field.
type NonceResponse struct {
	// Nonce is the wallet's current nonce as a decimal string. It is a
	// uint256; keep it a string.
	Nonce string `json:"nonce"`
}

// RelayPayloadResponse is what a caller needs before signing a relayed
// transaction: the relayer's address for a wallet type, and that wallet's
// current nonce, read together so the two cannot disagree.
type RelayPayloadResponse struct {
	// Address is the RELAYER's address for this wallet type — not the
	// user's, and not the user's wallet. It differs per WalletType, and
	// Polymarket changes it: the three addresses read live for one account
	// were all different a few months later. Read it for the type in hand
	// and use it; never cache it across types, and never hard-code one.
	Address string `json:"address"`
	// Nonce is the user's current nonce for that wallet, as a decimal
	// string. It is the same value the nonce endpoint returns.
	Nonce string `json:"nonce"`
}

// DeployedResponse is the wire shape of the deployed endpoint. Client.Deployed
// returns its single field.
type DeployedResponse struct {
	// Deployed reports whether the wallet exists on chain. It is the only
	// genuine boolean the relayer returns.
	Deployed bool `json:"deployed"`
}

// An APIKey is one relayer API key belonging to an address, as APIKeys
// reports it. Key is secret material: never log an APIKey.
type APIKey struct {
	// Key is the key itself, a UUID. It is the RELAYER_API_KEY header value.
	Key string `json:"apiKey"`
	// Address is the address the key was issued to. It is the
	// RELAYER_API_KEY_ADDRESS header value, and the relayer rejects the pair
	// if it is any other address.
	Address string `json:"address"`
	// CreatedAt is when the key was issued, RFC 3339 with microseconds.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is when the key last changed, in the same format.
	UpdatedAt string `json:"updatedAt"`
}

// Credentials returns the pair this key authenticates with, ready for
// WithAPIKey.
func (k APIKey) Credentials() APIKeyCredentials {
	return APIKeyCredentials{Key: k.Key, Address: k.Address}
}

// Nonce reports a wallet's current nonce: the value to sign the next relayed
// transaction at. It needs no authentication.
//
// address is the user's SIGNER address, the externally owned account, not the
// wallet's own address. wallet selects which of that user's wallets to read;
// it is required, and an unrecognised value is a 400 rather than a default.
//
// The nonce is returned as the decimal string the relayer sends. It is a
// uint256 and does not reliably fit an int64.
//
// GET /nonce
func (c *Client) Nonce(ctx context.Context, address string, wallet WalletType) (string, error) {
	var out NonceResponse
	if err := c.session.Get(ctx, epNonce, walletQuery(address, wallet), &out); err != nil {
		return "", err
	}
	return out.Nonce, nil
}

// RelayPayload reports the relayer's address for a wallet type together with
// that wallet's current nonce. It needs no authentication.
//
// It takes the same arguments as Nonce and returns the same nonce, plus the
// address the relayed transaction names as its relay. Prefer it over Nonce
// when a transaction is about to be built: one round trip, and the address
// and the nonce cannot drift apart.
//
// GET /relay-payload
func (c *Client) RelayPayload(ctx context.Context, address string, wallet WalletType) (RelayPayloadResponse, error) {
	var out RelayPayloadResponse
	err := c.session.Get(ctx, epRelayPayload, walletQuery(address, wallet), &out)
	return out, err
}

// Deployed reports whether a wallet exists on chain, which decides whether it
// must be deployed before anything can be relayed through it. It needs no
// authentication.
//
// address is the WALLET's address — the Safe or the deposit wallet — not the
// signer. wallet selects which kind to test and may be empty, which the
// relayer reads as WalletTypeSafe. Only WalletTypeSafe and
// WalletTypeWallet are meaningful here.
//
// An address that is not deployed is a false, not an error. An unrecognised
// wallet type is also a false: this endpoint does not reject one, it answers
// about the Safe instead, so pass one of the constants and never a
// hand-written string.
//
// GET /deployed
func (c *Client) Deployed(ctx context.Context, address string, wallet WalletType) (bool, error) {
	q := url.Values{"address": {address}}
	// The relayer defaults an absent type to SAFE. Sending an empty one is
	// not the same thing, so leave the parameter out entirely.
	if wallet != "" {
		q.Set("type", string(wallet))
	}
	var out DeployedResponse
	if err := c.session.Get(ctx, epDeployed, q, &out); err != nil {
		return false, err
	}
	return out.Deployed, nil
}

// Transaction reports the relayer transaction with an id — the id the submit
// endpoint returns. Poll it to pick up TransactionHash once the transaction
// is broadcast. It needs no authentication, and the relayer checks no
// ownership: an id is all it takes to read a transaction.
//
// The relayer answers with an array even though the id identifies one
// transaction, so this returns a slice. An id nobody has is a 404, reported
// as a *polymarket.Error — as is a malformed id, which is why a 404 is not
// proof that a well-formed id does not exist.
//
// GET /transaction
func (c *Client) Transaction(ctx context.Context, id string) ([]Transaction, error) {
	q := url.Values{"id": {id}}
	var out []Transaction
	if err := c.session.Get(ctx, epTransaction, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Transactions reports the caller's most recent relayer transactions, newest
// first.
//
// It is authenticated, and the credentials are the only thing that says whose
// transactions these are: there is no address argument, and the relayer takes
// none. Both credential schemes work here. How many transactions come back is
// the relayer's choice; there is no paging parameter.
//
// A Client without credentials reports ErrNoCredentials without sending
// anything.
//
// GET /transactions
func (c *Client) Transactions(ctx context.Context) ([]Transaction, error) {
	headers, err := c.authHeaders(http.MethodGet, epTransactions, "")
	if err != nil {
		return nil, err
	}
	var out []Transaction
	if err := c.session.Do(ctx, polymarket.Request{
		Method:  http.MethodGet,
		Path:    epTransactions,
		Headers: headers,
		Out:     &out,
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// APIKeys lists the relayer API keys belonging to the caller's address, or an
// empty slice when it has none.
//
// It is authenticated by the same credentials, but the relayer accepts a
// narrower set of schemes here than on Transactions: an API key works, and
// builder credentials are not listed for this endpoint. A refused scheme is a
// 403 where a bad credential is a 401, though the relayer has been seen to
// answer 401 for both, so neither status identifies which went wrong.
//
// This endpoint cannot bootstrap itself. A key is needed to list keys, and a
// key can only be created under Polymarket's own account authentication, so a
// caller holding nothing but a wallet cannot reach it.
//
// GET /relayer/api/keys
func (c *Client) APIKeys(ctx context.Context) ([]APIKey, error) {
	headers, err := c.authHeaders(http.MethodGet, epAPIKeys, "")
	if err != nil {
		return nil, err
	}
	var out []APIKey
	if err := c.session.Do(ctx, polymarket.Request{
		Method:  http.MethodGet,
		Path:    epAPIKeys,
		Headers: headers,
		Out:     &out,
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// walletQuery builds the address-and-type query the nonce and relay-payload
// endpoints share. Both parameters are required there, so an empty wallet
// type is sent as an empty value and refused by the relayer rather than
// quietly omitted.
func walletQuery(address string, wallet WalletType) url.Values {
	return url.Values{
		"address": {address},
		"type":    {string(wallet)},
	}
}

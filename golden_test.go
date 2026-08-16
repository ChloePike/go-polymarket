// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/json"
	"os"
	"testing"
)

// The types below mirror testdata/vectors.json, the observable output of the
// official TypeScript SDK captured by testdata/gen-vectors.mjs. Byte equality
// against these values is what proves this client signs what the exchange
// expects.

// goldenFile is the whole vector file.
type goldenFile struct {
	Source     string           `json:"source"`
	ChainID    int64            `json:"chainId"`
	Accounts   []goldenAccount  `json:"accounts"`
	Contracts  goldenContracts  `json:"contracts"`
	TypeHashes goldenTypeHashes `json:"typeHashes"`
	Orders     []goldenOrder    `json:"orders"`
	ClobAuth   []goldenClobAuth `json:"clobAuth"`
	HMAC       []goldenHMAC     `json:"hmac"`

	// WalletOrders are orders made by a smart-contract wallet, whose
	// signature wraps the order instead of covering it. They carry no digest:
	// the exchange never hashes the order alone for these.
	WalletOrders []goldenWalletOrder `json:"walletOrders"`
}

// goldenWalletOrder is one order signed on behalf of a contract wallet.
type goldenWalletOrder struct {
	Name      string            `json:"name"`
	Input     goldenOrderInput  `json:"input"`
	Order     goldenOrderFields `json:"order"`
	Signature string            `json:"signature"`
}

// goldenAccount is a key and the address derived from it.
type goldenAccount struct {
	PrivateKey string `json:"privateKey"`
	Address    string `json:"address"`
}

// goldenContracts holds the on-chain addresses the SDK resolves per chain.
type goldenContracts struct {
	Exchange          string `json:"exchange"`
	NegRiskExchange   string `json:"negRiskExchange"`
	ExchangeV2        string `json:"exchangeV2"`
	NegRiskExchangeV2 string `json:"negRiskExchangeV2"`
	ExchangeV3        string `json:"exchangeV3"`
	Collateral        string `json:"collateral"`
	ConditionalTokens string `json:"conditionalTokens"`
	NegRiskAdapter    string `json:"negRiskAdapter"`
}

// goldenTypeHashes are the two EIP-712 type hashes.
type goldenTypeHashes struct {
	Order  string `json:"order"`
	Domain string `json:"domain"`
}

// goldenOrder is one signed order: the inputs it was built from, the resolved
// order, its digest and its signature.
type goldenOrder struct {
	Name      string            `json:"name"`
	Input     goldenOrderInput  `json:"input"`
	Domain    goldenDomain      `json:"domain"`
	Order     goldenOrderFields `json:"order"`
	Digest    string            `json:"digest"`
	Signature string            `json:"signature"`
}

// goldenOrderInput is what a caller supplied.
type goldenOrderInput struct {
	Version     int    `json:"version"`
	NegRisk     bool   `json:"negRisk"`
	Side        string `json:"side"`
	Price       string `json:"price"`
	Size        string `json:"size"`
	TickSize    string `json:"tickSize"`
	TokenID     string `json:"tokenId"`
	BuilderCode string `json:"builderCode"`
	Expiration  string `json:"expiration"`

	// Wallet is the contract wallet an order was made by, empty for the
	// ordinary orders where the key is the account.
	Wallet string `json:"wallet"`
}

// goldenDomain is the EIP-712 domain an order was signed under.
type goldenDomain struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	ChainID           int64  `json:"chainId"`
	VerifyingContract string `json:"verifyingContract"`
}

// goldenOrderFields is the resolved order as the SDK produced it.
type goldenOrderFields struct {
	Salt          string `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Side          string `json:"side"`
	SignatureType uint8  `json:"signatureType"`
	Timestamp     string `json:"timestamp"`
	Metadata      string `json:"metadata"`
	Builder       string `json:"builder"`
	Expiration    string `json:"expiration"`
	Signature     string `json:"signature"`
}

// goldenClobAuth is one level-1 authentication payload.
type goldenClobAuth struct {
	ChainID   int64  `json:"chainId"`
	Address   string `json:"address"`
	Timestamp string `json:"timestamp"`
	Nonce     int64  `json:"nonce"`
	Message   string `json:"message"`
	Digest    string `json:"digest"`
	Signature string `json:"signature"`
}

// goldenHMAC is one level-2 request signature.
type goldenHMAC struct {
	Secret      string `json:"secret"`
	Timestamp   string `json:"timestamp"`
	Method      string `json:"method"`
	RequestPath string `json:"requestPath"`
	Body        string `json:"body"`
	Signature   string `json:"signature"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return g
}

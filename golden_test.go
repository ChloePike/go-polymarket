// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/json"
	"os"
	"testing"
)

// golden is testdata/vectors.json, the observable output of the official
// TypeScript SDK captured by testdata/gen-vectors.mjs. Byte equality against
// these values is what proves this client signs what the exchange expects.
type golden struct {
	Source   string `json:"source"`
	ChainID  int64  `json:"chainId"`
	Accounts []struct {
		PrivateKey string `json:"privateKey"`
		Address    string `json:"address"`
	} `json:"accounts"`
	Contracts struct {
		Exchange          string `json:"exchange"`
		NegRiskExchange   string `json:"negRiskExchange"`
		ExchangeV2        string `json:"exchangeV2"`
		NegRiskExchangeV2 string `json:"negRiskExchangeV2"`
		ExchangeV3        string `json:"exchangeV3"`
		Collateral        string `json:"collateral"`
		ConditionalTokens string `json:"conditionalTokens"`
		NegRiskAdapter    string `json:"negRiskAdapter"`
	} `json:"contracts"`
	TypeHashes struct {
		Order  string `json:"order"`
		Domain string `json:"domain"`
	} `json:"typeHashes"`
	Orders   []goldenOrder `json:"orders"`
	ClobAuth []struct {
		ChainID   int64  `json:"chainId"`
		Address   string `json:"address"`
		Timestamp string `json:"timestamp"`
		Nonce     int64  `json:"nonce"`
		Message   string `json:"message"`
		Digest    string `json:"digest"`
		Signature string `json:"signature"`
	} `json:"clobAuth"`
	HMAC []struct {
		Secret      string `json:"secret"`
		Timestamp   string `json:"timestamp"`
		Method      string `json:"method"`
		RequestPath string `json:"requestPath"`
		Body        string `json:"body"`
		Signature   string `json:"signature"`
	} `json:"hmac"`
}

type goldenOrder struct {
	Name  string `json:"name"`
	Input struct {
		Version     int    `json:"version"`
		NegRisk     bool   `json:"negRisk"`
		Side        string `json:"side"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		TickSize    string `json:"tickSize"`
		TokenID     string `json:"tokenId"`
		BuilderCode string `json:"builderCode"`
		Expiration  string `json:"expiration"`
	} `json:"input"`
	Domain struct {
		Name              string `json:"name"`
		Version           string `json:"version"`
		ChainID           int64  `json:"chainId"`
		VerifyingContract string `json:"verifyingContract"`
	} `json:"domain"`
	Order struct {
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
	} `json:"order"`
	Digest    string `json:"digest"`
	Signature string `json:"signature"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("golden vectors: %v", err)
	}
	return g
}

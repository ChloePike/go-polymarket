// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// TestWalletReads covers the reads that replace the relayer's own: the
// wallet's batch counter, its owner, and whether it exists yet.
func TestWalletReads(t *testing.T) {
	wallet := "0xD71776A8d4FdDeb3c150C4607B3f8bec31213B85"
	owner := "0x3b1A46790B22E7A48f1BbCBD0a629A253C7b2090"

	node := &fakeNode{t: t, results: map[string][]any{
		"eth_call":    {"0x" + strings.Repeat("0", 60) + "dc15"},
		"eth_getCode": {"0x363d3d37"},
	}}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))
	ctx := context.Background()

	nonce, err := client.WalletNonce(ctx, wallet)
	if err != nil {
		t.Fatalf("WalletNonce: %v", err)
	}
	if nonce.Int64() != 0xdc15 {
		t.Errorf("wallet nonce = %s, want %d", nonce, 0xdc15)
	}

	deployed, err := client.Deployed(ctx, wallet)
	if err != nil {
		t.Fatalf("Deployed: %v", err)
	}
	if !deployed {
		t.Error("an address holding code reported as undeployed")
	}

	node.results["eth_getCode"] = []any{"0x"}
	deployed, err = client.Deployed(ctx, wallet)
	if err != nil {
		t.Fatalf("Deployed: %v", err)
	}
	if deployed {
		t.Error("an address holding no code reported as deployed")
	}

	node.results["eth_call"] = []any{"0x" + strings.Repeat("0", 24) + strings.ToLower(owner[2:])}
	got, err := client.WalletOwner(ctx, wallet)
	if err != nil {
		t.Fatalf("WalletOwner: %v", err)
	}
	if got != owner {
		t.Errorf("owner = %s, want %s", got, owner)
	}
}

// TestPredictedWalletMatchesTheDerivation covers the cross-check this package
// exists to make possible: the factory's own prediction against the offline
// CREATE2 derivation. The node here answers with the derived address, so what
// the test pins is that the two are compared at all — the live agreement is
// recorded in DESIGN.md and rerun by examples/onchaincheck.
func TestPredictedWalletMatchesTheDerivation(t *testing.T) {
	owner := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)
	derived, err := polymarket.DeriveDepositWallet(owner,
		contracts.DepositWalletFactory, contracts.DepositWalletBeacon)
	if err != nil {
		t.Fatalf("DeriveDepositWallet: %v", err)
	}

	node := &fakeNode{t: t, result: "0x" + strings.Repeat("0", 24) + strings.ToLower(derived[2:])}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))

	predicted, err := client.PredictDepositWallet(context.Background(), owner)
	if err != nil {
		t.Fatalf("PredictDepositWallet: %v", err)
	}
	if predicted != derived {
		t.Errorf("factory predicts %s, derivation gives %s", predicted, derived)
	}

	call := node.last("eth_call")
	var p callParams
	if err := json.Unmarshal(call.Params[0], &p); err != nil {
		t.Fatalf("decoding eth_call params: %v", err)
	}
	if !strings.EqualFold(p.To, contracts.DepositWalletFactory) {
		t.Errorf("prediction asked %s, want the factory %s", p.To, contracts.DepositWalletFactory)
	}
}

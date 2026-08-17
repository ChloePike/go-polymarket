// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// The arguments the position vectors were generated with.
const (
	vectorCondition = "0x1763261a2bf8884e1cfce3c83522810db637064a17cf0695846762e9b2600aa1"
	vectorMarket    = "0x9a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f7081"
	vectorOracle    = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
	vectorUSDC      = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
)

// positionVectors indexes the calldata vectors by name.
func positionVectors(t *testing.T) map[string]callVectorCase {
	t.Helper()
	v := loadTxVectors(t)
	if len(v.Positions) == 0 {
		t.Fatal("no position vectors")
	}
	byName := make(map[string]callVectorCase, len(v.Positions))
	for _, c := range v.Positions {
		byName[c.Name] = c
	}
	return byName
}

// TestPositionCalldataMatchesTheReferenceClient pins the conditional-token
// calls. Their arguments are dynamic, so the encoding carries offsets, and an
// offset computed wrongly does not fail to decode — it decodes to a different
// amount of a different position.
func TestPositionCalldataMatchesTheReferenceClient(t *testing.T) {
	vectors := positionVectors(t)
	million := big.NewInt(1_000_000)

	split, err := SplitPositionData(vectorUSDC, vectorCondition, BinaryPartition(), million)
	if err != nil {
		t.Fatalf("SplitPositionData: %v", err)
	}
	assertCalldata(t, vectors, "split", split)

	merge, err := MergePositionsData(vectorUSDC, vectorCondition, BinaryPartition(), big.NewInt(250_000))
	if err != nil {
		t.Fatalf("MergePositionsData: %v", err)
	}
	assertCalldata(t, vectors, "merge", merge)

	redeem, err := RedeemPositionsData(vectorUSDC, vectorCondition, BinaryPartition())
	if err != nil {
		t.Fatalf("RedeemPositionsData: %v", err)
	}
	assertCalldata(t, vectors, "redeem", redeem)

	one, err := RedeemPositionsData(vectorUSDC, vectorCondition, []*big.Int{big.NewInt(1)})
	if err != nil {
		t.Fatalf("RedeemPositionsData: %v", err)
	}
	assertCalldata(t, vectors, "redeem one set", one)

	five := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(4), big.NewInt(8), big.NewInt(16)}
	wide, err := SplitPositionData(vectorUSDC, vectorCondition, five, big.NewInt(7))
	if err != nil {
		t.Fatalf("SplitPositionData: %v", err)
	}
	assertCalldata(t, vectors, "split five outcomes", wide)
}

// TestNegRiskCalldataMatchesTheReferenceClient pins the adapter's own calls,
// which take shorter arguments than the framework's under the same names.
func TestNegRiskCalldataMatchesTheReferenceClient(t *testing.T) {
	vectors := positionVectors(t)
	million := big.NewInt(1_000_000)

	split, err := NegRiskSplitPositionData(vectorCondition, million)
	if err != nil {
		t.Fatalf("NegRiskSplitPositionData: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk split", split)

	merge, err := NegRiskMergePositionsData(vectorCondition, million)
	if err != nil {
		t.Fatalf("NegRiskMergePositionsData: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk merge", merge)

	redeem, err := NegRiskRedeemPositionsData(vectorCondition,
		[]*big.Int{big.NewInt(1_500_000), new(big.Int)})
	if err != nil {
		t.Fatalf("NegRiskRedeemPositionsData: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk redeem", redeem)

	convert, err := NegRiskConvertPositionsData(vectorMarket, big.NewInt(6), big.NewInt(2_500_000))
	if err != nil {
		t.Fatalf("NegRiskConvertPositionsData: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk convert", convert)
}

// TestTheTwoRedemptionsDiffer pins the trap between them: the framework's
// redemption takes index sets and the adapter's takes amounts, so the same
// argument list means two different things and must not encode alike.
func TestTheTwoRedemptionsDiffer(t *testing.T) {
	sets := BinaryPartition()
	framework, err := RedeemPositionsData(vectorUSDC, vectorCondition, sets)
	if err != nil {
		t.Fatalf("RedeemPositionsData: %v", err)
	}
	adapter, err := NegRiskRedeemPositionsData(vectorCondition, sets)
	if err != nil {
		t.Fatalf("NegRiskRedeemPositionsData: %v", err)
	}
	if strings.EqualFold(hexData(framework), hexData(adapter)) {
		t.Error("the two redemptions encode alike")
	}
	if strings.EqualFold(hexData(framework[:4]), hexData(adapter[:4])) {
		t.Errorf("the two redemptions share a selector: %s", hexData(framework[:4]))
	}
}

// TestPositionCallsRejectNonsense covers the arguments that cannot mean
// anything: no outcomes, no amount, a malformed condition.
func TestPositionCallsRejectNonsense(t *testing.T) {
	million := big.NewInt(1_000_000)

	if _, err := SplitPositionData(vectorUSDC, vectorCondition, nil, million); err == nil {
		t.Error("split with no partition")
	}
	if _, err := SplitPositionData(vectorUSDC, vectorCondition, BinaryPartition(), nil); err == nil {
		t.Error("split with no amount")
	}
	if _, err := SplitPositionData(vectorUSDC, vectorCondition, BinaryPartition(), new(big.Int)); err == nil {
		t.Error("split of nothing")
	}
	if _, err := SplitPositionData(vectorUSDC, "0x1234", BinaryPartition(), million); err == nil {
		t.Error("split with a malformed condition id")
	}
	if _, err := RedeemPositionsData(vectorUSDC, vectorCondition, nil); err == nil {
		t.Error("redemption of no index sets")
	}
	if _, err := NegRiskRedeemPositionsData(vectorCondition, nil); err == nil {
		t.Error("neg-risk redemption of no amounts")
	}
	if _, err := NegRiskConvertPositionsData(vectorMarket, new(big.Int), million); err == nil {
		t.Error("conversion of an empty index set")
	}
}

// TestPositionReadsMatchTheReferenceClient covers the calldata of the reads,
// and that each is addressed to the contract that answers it.
func TestPositionReadsMatchTheReferenceClient(t *testing.T) {
	vectors := positionVectors(t)
	node := &fakeNode{t: t, result: word(1)}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)
	ctx := context.Background()

	if _, err := client.ConditionID(ctx, vectorOracle, vectorCondition, big.NewInt(2)); err != nil {
		t.Fatalf("ConditionID: %v", err)
	}
	assertCalldata(t, vectors, "condition id", node.lastData(t))
	assertTarget(t, node, contracts.ConditionalTokens)

	if _, err := client.PayoutDenominator(ctx, vectorCondition); err != nil {
		t.Fatalf("PayoutDenominator: %v", err)
	}
	assertCalldata(t, vectors, "payout denominator", node.lastData(t))

	if _, err := client.NegRiskConditionID(ctx, vectorCondition); err != nil {
		t.Fatalf("NegRiskConditionID: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk condition id", node.lastData(t))
	assertTarget(t, node, contracts.NegRiskAdapter)

	if _, err := client.NegRiskPositionID(ctx, vectorCondition, true); err != nil {
		t.Fatalf("NegRiskPositionID: %v", err)
	}
	assertCalldata(t, vectors, "neg-risk position id", node.lastData(t))
}

// TestPositionIDIsTwoCalls covers the derivation that needs the collection
// first: the collection id from the index set, then the token id from the
// collection.
func TestPositionIDIsTwoCalls(t *testing.T) {
	vectors := positionVectors(t)
	// The collection call answers with the condition id, so the second call
	// is made with a known argument and can be compared to a vector.
	node := &fakeNode{t: t, result: vectorCondition + "00"}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))

	if _, err := client.PositionID(context.Background(), vectorCondition, big.NewInt(1)); err != nil {
		t.Fatalf("PositionID: %v", err)
	}
	calls := node.received()
	if len(calls) != 2 {
		t.Fatalf("PositionID made %d calls, want 2", len(calls))
	}
	assertCalldata(t, vectors, "position id", node.lastData(t))
}

// assertTarget checks which contract the last eth_call was addressed to.
func assertTarget(t *testing.T, node *fakeNode, want string) {
	t.Helper()
	call := node.last("eth_call")
	var p callParams
	if err := json.Unmarshal(call.Params[0], &p); err != nil {
		t.Fatalf("decoding eth_call params: %v", err)
	}
	if !strings.EqualFold(p.To, want) {
		t.Errorf("call addressed to %s, want %s", p.To, want)
	}
}

// TestPositionCallsNeedAKnownChain covers a client pointed at a chain this
// module has no addresses for.
func TestPositionCallsNeedAKnownChain(t *testing.T) {
	node := &fakeNode{t: t, result: word(1)}
	client := New(node.start(), WithChainID(1))
	if _, err := client.CTFTransaction([]byte{1}); err == nil {
		t.Error("built a conditional-token transaction for an unknown chain")
	}
	if _, err := client.NegRiskTransaction([]byte{1}); err == nil {
		t.Error("built a neg-risk transaction for an unknown chain")
	}
	if _, err := client.PayoutDenominator(context.Background(), vectorCondition); err == nil {
		t.Error("read a payout denominator on an unknown chain")
	}
}

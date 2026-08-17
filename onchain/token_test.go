// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"math/big"
	"strings"
	"testing"

	polymarket "github.com/ChloePike/go-polymarket"
)

// The addresses the calldata vectors were generated against. They are the
// Polygon contracts, repeated here so that a change to this repository's
// contract table shows up as a failing encoding rather than as calldata that
// silently approves something else.
const (
	vectorSpender  = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	vectorSpender2 = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
	vectorOwner    = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	vectorTokenID  = "71321045679252212594626385532706912750332728571942532289631379312455583992563"
)

// TestApprovalCalldataMatchesTheReferenceClient pins the calldata an approval
// carries. A wrong selector calls a function that does not exist and reverts,
// which is survivable; a right selector with a wrong argument approves the
// wrong contract, which is not.
func TestApprovalCalldataMatchesTheReferenceClient(t *testing.T) {
	v := loadTxVectors(t)
	byName := make(map[string]callVectorCase, len(v.Calls))
	for _, c := range v.Calls {
		byName[c.Name] = c
	}
	if len(byName) == 0 {
		t.Fatal("no calldata vectors")
	}

	approveMax, err := ApproveData(vectorSpender, MaxUint256())
	if err != nil {
		t.Fatalf("ApproveData: %v", err)
	}
	assertCalldata(t, byName, "approve max", approveMax)

	approveZero, err := ApproveData(vectorSpender2, new(big.Int))
	if err != nil {
		t.Fatalf("ApproveData: %v", err)
	}
	assertCalldata(t, byName, "approve zero", approveZero)

	on, err := SetApprovalForAllData(vectorSpender, true)
	if err != nil {
		t.Fatalf("SetApprovalForAllData: %v", err)
	}
	assertCalldata(t, byName, "setApprovalForAll true", on)

	off, err := SetApprovalForAllData(vectorSpender, false)
	if err != nil {
		t.Fatalf("SetApprovalForAllData: %v", err)
	}
	assertCalldata(t, byName, "setApprovalForAll false", off)
}

// assertCalldata compares built calldata against the named vector.
func assertCalldata(t *testing.T, vectors map[string]callVectorCase, name string, got []byte) {
	t.Helper()
	want, ok := vectors[name]
	if !ok {
		t.Fatalf("no vector named %q", name)
	}
	if !strings.EqualFold(hexData(got), want.Data) {
		t.Errorf("%s = %s, want %s", name, hexData(got), want.Data)
	}
}

// TestReadCalldataMatchesTheReferenceClient pins the calls this package makes
// through eth_call. They spend nothing, but a wrong argument order reads one
// address's allowance and reports it as another's.
func TestReadCalldataMatchesTheReferenceClient(t *testing.T) {
	v := loadTxVectors(t)
	byName := make(map[string]callVectorCase, len(v.Calls))
	for _, c := range v.Calls {
		byName[c.Name] = c
	}

	node := &fakeNode{t: t}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)

	node.result = "0x" + strings.Repeat("0", 63) + "1"
	if _, err := client.Allowance(context.Background(), contracts.Collateral, vectorOwner, vectorSpender); err != nil {
		t.Fatalf("Allowance: %v", err)
	}
	assertCalldata(t, byName, "allowance", node.lastData(t))

	if _, err := client.TokenBalance(context.Background(), contracts.Collateral, vectorOwner); err != nil {
		t.Fatalf("TokenBalance: %v", err)
	}
	assertCalldata(t, byName, "erc20 balanceOf", node.lastData(t))

	if _, err := client.IsApprovedForAll(context.Background(), contracts.ConditionalTokens, vectorOwner, vectorSpender); err != nil {
		t.Fatalf("IsApprovedForAll: %v", err)
	}
	assertCalldata(t, byName, "isApprovedForAll", node.lastData(t))

	if _, err := client.OutcomeBalance(context.Background(), contracts.ConditionalTokens, vectorOwner, vectorTokenID); err != nil {
		t.Fatalf("OutcomeBalance: %v", err)
	}
	assertCalldata(t, byName, "erc1155 balanceOf", node.lastData(t))
}

// TestApproveRejectsANegativeAmount covers the one amount that has no
// encoding.
func TestApproveRejectsANegativeAmount(t *testing.T) {
	if _, err := ApproveData(vectorSpender, big.NewInt(-1)); err == nil {
		t.Error("encoded a negative allowance")
	}
	if _, err := ApproveData(vectorSpender, nil); err == nil {
		t.Error("encoded a nil allowance")
	}
	if _, err := ApproveData("0x1234", MaxUint256()); err == nil {
		t.Error("encoded a malformed spender")
	}
}

// TestRequiredApprovalsCoverBothStandards checks the list a caller works
// through before trading: every exchange the chain has, under both token
// standards, and nothing with an empty address.
func TestRequiredApprovalsCoverBothStandards(t *testing.T) {
	contracts, ok := polymarket.ContractsFor(polymarket.ChainPolygon)
	if !ok {
		t.Fatal("no Polygon contracts")
	}
	approvals := RequiredApprovals(contracts)
	if len(approvals) == 0 {
		t.Fatal("no approvals")
	}

	var erc20, erc1155 int
	for _, a := range approvals {
		if a.Spender == "" || a.Token == "" {
			t.Errorf("approval %q has an empty address", a.Name)
		}
		switch a.Standard {
		case ERC20:
			erc20++
			if a.Token != contracts.Collateral {
				t.Errorf("ERC-20 approval %q is sent to %s, want the collateral", a.Name, a.Token)
			}
		case ERC1155:
			erc1155++
			if a.Token != contracts.ConditionalTokens {
				t.Errorf("ERC-1155 approval %q is sent to %s, want the conditional tokens", a.Name, a.Token)
			}
		}
	}
	if erc20 == 0 || erc1155 == 0 {
		t.Errorf("approvals cover %d ERC-20 and %d ERC-1155, want both", erc20, erc1155)
	}
}

// TestApprovalTransactionTargetsTheToken pins which contract an approval is
// sent to. It is the token, not the spender: sending it to the spender calls
// a function on the wrong contract and approves nothing.
func TestApprovalTransactionTargetsTheToken(t *testing.T) {
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)
	for _, a := range RequiredApprovals(contracts) {
		tx, err := a.Transaction(MaxUint256())
		if err != nil {
			t.Fatalf("%s: %v", a.Name, err)
		}
		if tx.To != a.Token {
			t.Errorf("%s: transaction targets %s, want the token %s", a.Name, tx.To, a.Token)
		}
		revoke, err := a.RevokeData()
		if err != nil {
			t.Fatalf("%s: %v", a.Name, err)
		}
		if len(revoke) == 0 || strings.EqualFold(hexData(revoke), hexData(tx.Data)) {
			t.Errorf("%s: revoking encodes the same calldata as granting", a.Name)
		}
	}
}

// TestMissingApprovals covers the read that tells a caller what is left to do,
// including the difference between an allowance of zero and one that is merely
// smaller than what the caller wants.
func TestMissingApprovals(t *testing.T) {
	node := &fakeNode{t: t}
	client := New(node.start(), WithChainID(polymarket.ChainPolygon))

	node.result = word(0)
	missing, err := client.MissingApprovals(context.Background(), vectorOwner, nil)
	if err != nil {
		t.Fatalf("MissingApprovals: %v", err)
	}
	contracts, _ := polymarket.ContractsFor(polymarket.ChainPolygon)
	if len(missing) != len(RequiredApprovals(contracts)) {
		t.Errorf("with nothing approved, %d approvals are missing, want %d",
			len(missing), len(RequiredApprovals(contracts)))
	}

	node.result = word(1)
	missing, err = client.MissingApprovals(context.Background(), vectorOwner, nil)
	if err != nil {
		t.Fatalf("MissingApprovals: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("with everything approved, %d approvals are missing, want none", len(missing))
	}

	// An allowance of one is not enough for a caller that wants two.
	missing, err = client.MissingApprovals(context.Background(), vectorOwner, big.NewInt(2))
	if err != nil {
		t.Fatalf("MissingApprovals: %v", err)
	}
	if len(missing) == 0 {
		t.Error("an allowance below the wanted amount counted as granted")
	}
	for _, a := range missing {
		if a.Standard != ERC20 {
			t.Errorf("%s: an ERC-1155 approval was measured against an amount", a.Name)
		}
	}
}

// word renders a small integer as one 32-byte return value.
func word(v byte) string {
	return "0x" + strings.Repeat("00", 31) + string([]byte{hexDigit(v >> 4), hexDigit(v & 0xf)})
}

func hexDigit(v byte) byte {
	if v < 10 {
		return '0' + v
	}
	return 'a' + v - 10
}

// TestUnknownChainHasNoApprovals covers the client configured for a chain this
// module has no addresses for: it must refuse rather than approve nothing.
func TestUnknownChainHasNoApprovals(t *testing.T) {
	node := &fakeNode{t: t}
	client := New(node.start(), WithChainID(1))
	if _, err := client.MissingApprovals(context.Background(), vectorOwner, nil); err == nil {
		t.Error("listed approvals for a chain with no known contracts")
	}
}

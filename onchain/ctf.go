// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ChloePike/go-polymarket/internal/abi"
	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// The conditional-token functions. The first group is Gnosis's
// ConditionalTokens, which every ordinary market settles through; the second
// is Polymarket's neg-risk adapter, which wraps it for multi-outcome events
// and takes shorter arguments because it fills in the rest itself.
const (
	splitSig             = "splitPosition(address,bytes32,bytes32,uint256[],uint256)"
	mergeSig             = "mergePositions(address,bytes32,bytes32,uint256[],uint256)"
	redeemSig            = "redeemPositions(address,bytes32,bytes32,uint256[])"
	conditionIDSig       = "getConditionId(address,bytes32,uint256)"
	collectionIDSig      = "getCollectionId(bytes32,bytes32,uint256)"
	positionIDSig        = "getPositionId(address,bytes32)"
	payoutDenominatorSig = "payoutDenominator(bytes32)"

	negRiskSplitSig       = "splitPosition(bytes32,uint256)"
	negRiskMergeSig       = "mergePositions(bytes32,uint256)"
	negRiskRedeemSig      = "redeemPositions(bytes32,uint256[])"
	negRiskConvertSig     = "convertPositions(bytes32,uint256,uint256)"
	negRiskConditionIDSig = "getConditionId(bytes32)"
	negRiskPositionIDSig  = "getPositionId(bytes32,bool)"
)

// topCollection is the parent collection id every Polymarket position sits
// under. The conditional-token framework allows a position to be split again
// under another condition, and the parent names that outer condition; nothing
// on Polymarket does, so it is always zero here.
var topCollection eip712.Word

// BinaryPartition returns the partition of a two-outcome market: index set 1
// is the first outcome, index set 2 the second.
//
// An index set is a bitmask over the outcome slots, so the partition says how
// to divide the whole into the positions being minted. For a binary market
// that is {0b01, 0b10} and there is nothing else it can be.
func BinaryPartition() []*big.Int { return []*big.Int{big.NewInt(1), big.NewInt(2)} }

// SplitPositionData builds the calldata that turns collateral into a complete
// set of outcome tokens: amount of collateral in, amount of every outcome
// token out. It is the mint, and it is how a market maker gets both sides.
//
// The amount is in the collateral's own units — six-decimal fixed point for
// USDC, so one dollar is 1000000.
func SplitPositionData(collateral, conditionID string, partition []*big.Int, amount *big.Int) ([]byte, error) {
	args, err := ctfPositionArgs(collateral, conditionID, partition)
	if err != nil {
		return nil, err
	}
	value, err := amountWord(amount, "split")
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(splitSig, append(args, abi.Word32(value))...), nil
}

// MergePositionsData builds the calldata for the reverse of a split: a
// complete set of outcome tokens in, collateral out. It needs every outcome of
// the set, which is what makes it the only way out of a market that has not
// resolved.
func MergePositionsData(collateral, conditionID string, partition []*big.Int, amount *big.Int) ([]byte, error) {
	args, err := ctfPositionArgs(collateral, conditionID, partition)
	if err != nil {
		return nil, err
	}
	value, err := amountWord(amount, "merge")
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(mergeSig, append(args, abi.Word32(value))...), nil
}

// RedeemPositionsData builds the calldata that claims what a resolved market
// owes: the caller's whole balance of each index set given, paid out in
// collateral at the reported ratio. It takes no amount — the contract pays out
// everything the caller holds — and it reverts while the condition is
// unresolved.
func RedeemPositionsData(collateral, conditionID string, indexSets []*big.Int) ([]byte, error) {
	args, err := ctfPositionArgs(collateral, conditionID, indexSets)
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(redeemSig, args...), nil
}

// ctfPositionArgs builds the four arguments the three conditional-token calls
// share: the collateral, the parent collection, the condition and the sets.
func ctfPositionArgs(collateral, conditionID string, sets []*big.Int) ([]abi.Arg, error) {
	token, err := eip712.Address(collateral)
	if err != nil {
		return nil, fmt.Errorf("onchain: collateral: %w", err)
	}
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	if len(sets) == 0 {
		return nil, fmt.Errorf("onchain: an index set list cannot be empty")
	}
	list, err := abi.UintArray(sets)
	if err != nil {
		return nil, fmt.Errorf("onchain: index sets: %w", err)
	}
	return []abi.Arg{
		abi.Word32(token),
		abi.Word32(topCollection),
		abi.Word32(condition),
		list,
	}, nil
}

// NegRiskSplitPositionData builds the calldata for a split through the
// neg-risk adapter. The adapter knows the collateral and the partition, so it
// takes the condition and the amount and nothing else.
func NegRiskSplitPositionData(conditionID string, amount *big.Int) ([]byte, error) {
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	value, err := amountWord(amount, "split")
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(negRiskSplitSig, abi.Word32(condition), abi.Word32(value)), nil
}

// NegRiskMergePositionsData builds the calldata for a merge through the
// neg-risk adapter.
func NegRiskMergePositionsData(conditionID string, amount *big.Int) ([]byte, error) {
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	value, err := amountWord(amount, "merge")
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(negRiskMergeSig, abi.Word32(condition), abi.Word32(value)), nil
}

// NegRiskRedeemPositionsData builds the calldata for a redemption through the
// neg-risk adapter.
//
// The array is the trap. The adapter's redemption takes AMOUNTS — how much of
// the yes position and how much of the no position to redeem, in that order —
// where the conditional-token framework's takes index sets. The two calls look
// alike and mean different things, so a list of index sets passed here redeems
// one unit and two units of a position rather than all of it.
func NegRiskRedeemPositionsData(conditionID string, amounts []*big.Int) ([]byte, error) {
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	if len(amounts) == 0 {
		return nil, fmt.Errorf("onchain: a redemption needs an amount for each outcome")
	}
	list, err := abi.UintArray(amounts)
	if err != nil {
		return nil, fmt.Errorf("onchain: redemption amounts: %w", err)
	}
	return abi.EncodeArgsCall(negRiskRedeemSig, abi.Word32(condition), list), nil
}

// NegRiskConvertPositionsData builds the calldata that converts the no
// positions of one neg-risk market into the yes positions of every other
// question in it, plus collateral. It is what makes a multi-outcome event
// capital-efficient, and it exists only on the adapter.
//
// The index set is a bitmask over the event's questions: bit n is question n.
func NegRiskConvertPositionsData(marketID string, indexSet, amount *big.Int) ([]byte, error) {
	market, err := eip712.Bytes32(marketID)
	if err != nil {
		return nil, fmt.Errorf("onchain: market id: %w", err)
	}
	set, err := amountWord(indexSet, "convert")
	if err != nil {
		return nil, err
	}
	if set == (eip712.Word{}) {
		return nil, fmt.Errorf("onchain: an index set of zero converts nothing")
	}
	value, err := amountWord(amount, "convert")
	if err != nil {
		return nil, err
	}
	return abi.EncodeArgsCall(negRiskConvertSig, abi.Word32(market), abi.Word32(set), abi.Word32(value)), nil
}

// amountWord encodes an amount, refusing the values that have no meaning.
func amountWord(amount *big.Int, what string) (eip712.Word, error) {
	if amount == nil || amount.Sign() <= 0 {
		return eip712.Word{}, fmt.Errorf("onchain: a %s needs a positive amount", what)
	}
	w, err := eip712.Uint(amount)
	if err != nil {
		return eip712.Word{}, fmt.Errorf("onchain: %s amount: %w", what, err)
	}
	return w, nil
}

// CTFTransaction wraps calldata for the conditional-tokens contract in an
// unsigned transaction, leaving the fields a node fills to Fill or Send.
func (c *Client) CTFTransaction(data []byte) (Transaction, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return Transaction{}, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	return Transaction{To: contracts.ConditionalTokens, Data: data}, nil
}

// NegRiskTransaction wraps calldata for the neg-risk adapter in an unsigned
// transaction. Sending a neg-risk call to the conditional-tokens contract
// instead reverts, because the two take different arguments under the same
// names.
func (c *Client) NegRiskTransaction(data []byte) (Transaction, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return Transaction{}, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	if contracts.NegRiskAdapter == "" {
		return Transaction{}, fmt.Errorf("onchain: no neg-risk adapter on chain %d", c.ChainID())
	}
	return Transaction{To: contracts.NegRiskAdapter, Data: data}, nil
}

// ConditionID derives the id of a condition from the oracle that will resolve
// it, its question id and how many outcomes it has. It is a pure function of
// its arguments, read from the contract rather than reimplemented here.
func (c *Client) ConditionID(ctx context.Context, oracle, questionID string, outcomeSlots *big.Int) (string, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return "", fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	oracleWord, err := eip712.Address(oracle)
	if err != nil {
		return "", fmt.Errorf("onchain: oracle: %w", err)
	}
	question, err := eip712.Bytes32(questionID)
	if err != nil {
		return "", fmt.Errorf("onchain: question id: %w", err)
	}
	slots, err := amountWord(outcomeSlots, "condition")
	if err != nil {
		return "", err
	}
	out, err := c.Call(ctx, CallMsg{
		To:   contracts.ConditionalTokens,
		Data: abi.EncodeCall(conditionIDSig, oracleWord, question, slots),
	})
	if err != nil {
		return "", err
	}
	if len(out) < 32 {
		return "", fmt.Errorf("onchain: condition id: call returned %d bytes, want 32", len(out))
	}
	return hexData(out[:32]), nil
}

// PositionID returns the ERC-1155 token id of one outcome: the id an order
// names and a balance is held under.
//
// It is two calls, because a position is identified by its collection and the
// collection by its index set.
func (c *Client) PositionID(ctx context.Context, conditionID string, indexSet *big.Int) (*big.Int, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return nil, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	set, err := amountWord(indexSet, "position")
	if err != nil {
		return nil, err
	}
	collection, err := c.Call(ctx, CallMsg{
		To:   contracts.ConditionalTokens,
		Data: abi.EncodeCall(collectionIDSig, topCollection, condition, set),
	})
	if err != nil {
		return nil, err
	}
	if len(collection) < 32 {
		return nil, fmt.Errorf("onchain: collection id: call returned %d bytes, want 32", len(collection))
	}
	token, err := eip712.Address(contracts.Collateral)
	if err != nil {
		return nil, err
	}
	var collectionWord eip712.Word
	copy(collectionWord[:], collection[:32])
	return c.callUint(ctx, contracts.ConditionalTokens,
		abi.EncodeCall(positionIDSig, token, collectionWord))
}

// PayoutDenominator reports whether a condition has resolved and what its
// payouts are scaled by: zero until the oracle reports, non-zero after. A
// redemption before that reverts.
func (c *Client) PayoutDenominator(ctx context.Context, conditionID string) (*big.Int, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return nil, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	condition, err := eip712.Bytes32(conditionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: condition id: %w", err)
	}
	return c.callUint(ctx, contracts.ConditionalTokens,
		abi.EncodeCall(payoutDenominatorSig, condition))
}

// Resolved reports whether a condition has been resolved, which is the check
// to make before redeeming.
func (c *Client) Resolved(ctx context.Context, conditionID string) (bool, error) {
	d, err := c.PayoutDenominator(ctx, conditionID)
	if err != nil {
		return false, err
	}
	return d.Sign() != 0, nil
}

// NegRiskConditionID returns the condition id the adapter uses for one
// question of a neg-risk event.
func (c *Client) NegRiskConditionID(ctx context.Context, questionID string) (string, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return "", fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	question, err := eip712.Bytes32(questionID)
	if err != nil {
		return "", fmt.Errorf("onchain: question id: %w", err)
	}
	out, err := c.Call(ctx, CallMsg{
		To:   contracts.NegRiskAdapter,
		Data: abi.EncodeCall(negRiskConditionIDSig, question),
	})
	if err != nil {
		return "", err
	}
	if len(out) < 32 {
		return "", fmt.Errorf("onchain: neg-risk condition id: call returned %d bytes, want 32", len(out))
	}
	return hexData(out[:32]), nil
}

// NegRiskPositionID returns the token id of one side of a neg-risk question.
func (c *Client) NegRiskPositionID(ctx context.Context, questionID string, yes bool) (*big.Int, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return nil, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	question, err := eip712.Bytes32(questionID)
	if err != nil {
		return nil, fmt.Errorf("onchain: question id: %w", err)
	}
	var flag uint8
	if yes {
		flag = 1
	}
	return c.callUint(ctx, contracts.NegRiskAdapter,
		abi.EncodeCall(negRiskPositionIDSig, question, eip712.Uint8(flag)))
}

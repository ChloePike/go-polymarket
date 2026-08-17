// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"fmt"
	"math/big"

	polymarket "github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/internal/abi"
	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// The token functions this package calls. Each string is the canonical
// signature the four-byte selector is taken from: no argument names, no
// spaces. A character out of place names a function that does not exist and
// the call reverts.
const (
	approveSig           = "approve(address,uint256)"
	allowanceSig         = "allowance(address,address)"
	erc20BalanceSig      = "balanceOf(address)"
	setApprovalForAllSig = "setApprovalForAll(address,bool)"
	isApprovedForAllSig  = "isApprovedForAll(address,address)"
	erc1155BalanceSig    = "balanceOf(address,uint256)"
)

// MaxUint256 returns the unlimited allowance, which is what Polymarket's own
// interface sets. It is a standing permission for the spender to move that
// token out of the account until it is revoked, so it is returned by a
// function rather than assumed by a default.
func MaxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

// ApproveData builds the calldata for an ERC-20 approval, letting spender move
// up to amount of the token the call is sent to.
//
// Some tokens refuse to change a non-zero allowance to another non-zero one
// and require a reset to zero first. The USDC Polymarket settles in is not one
// of them, but a caller pointing this at another token should know the rule
// exists.
func ApproveData(spender string, amount *big.Int) ([]byte, error) {
	if amount == nil || amount.Sign() < 0 {
		return nil, fmt.Errorf("onchain: approval amount must be zero or more")
	}
	to, err := eip712.Address(spender)
	if err != nil {
		return nil, fmt.Errorf("onchain: approve spender: %w", err)
	}
	value, err := eip712.Uint(amount)
	if err != nil {
		return nil, fmt.Errorf("onchain: approve amount: %w", err)
	}
	return abi.EncodeCall(approveSig, to, value), nil
}

// SetApprovalForAllData builds the calldata for an ERC-1155 approval. It is
// all or nothing: an operator approved this way can move every outcome token
// the account holds, of every market, until the approval is revoked with
// approved false.
func SetApprovalForAllData(operator string, approved bool) ([]byte, error) {
	to, err := eip712.Address(operator)
	if err != nil {
		return nil, fmt.Errorf("onchain: approval operator: %w", err)
	}
	var flag uint8
	if approved {
		flag = 1
	}
	return abi.EncodeCall(setApprovalForAllSig, to, eip712.Uint8(flag)), nil
}

// Allowance reads how much of an ERC-20 token a spender may move on an owner's
// behalf.
func (c *Client) Allowance(ctx context.Context, token, owner, spender string) (*big.Int, error) {
	ownerWord, err := eip712.Address(owner)
	if err != nil {
		return nil, fmt.Errorf("onchain: allowance owner: %w", err)
	}
	spenderWord, err := eip712.Address(spender)
	if err != nil {
		return nil, fmt.Errorf("onchain: allowance spender: %w", err)
	}
	return c.callUint(ctx, token, abi.EncodeCall(allowanceSig, ownerWord, spenderWord))
}

// TokenBalance reads an ERC-20 balance. For the collateral contract the result
// is USDC in its own six-decimal fixed point, which polymarket.FormatAmount
// turns into a decimal string.
func (c *Client) TokenBalance(ctx context.Context, token, owner string) (*big.Int, error) {
	ownerWord, err := eip712.Address(owner)
	if err != nil {
		return nil, fmt.Errorf("onchain: balance owner: %w", err)
	}
	return c.callUint(ctx, token, abi.EncodeCall(erc20BalanceSig, ownerWord))
}

// IsApprovedForAll reads whether an operator may move all of an owner's
// ERC-1155 tokens.
func (c *Client) IsApprovedForAll(ctx context.Context, token, owner, operator string) (bool, error) {
	ownerWord, err := eip712.Address(owner)
	if err != nil {
		return false, fmt.Errorf("onchain: approval owner: %w", err)
	}
	operatorWord, err := eip712.Address(operator)
	if err != nil {
		return false, fmt.Errorf("onchain: approval operator: %w", err)
	}
	v, err := c.callUint(ctx, token, abi.EncodeCall(isApprovedForAllSig, ownerWord, operatorWord))
	if err != nil {
		return false, err
	}
	return v.Sign() != 0, nil
}

// OutcomeBalance reads how many units of one outcome token an address holds.
// The token id is the decimal string the CLOB uses, the same value an order
// carries, and it is a 256-bit number rather than an index.
func (c *Client) OutcomeBalance(ctx context.Context, token, owner, tokenID string) (*big.Int, error) {
	ownerWord, err := eip712.Address(owner)
	if err != nil {
		return nil, fmt.Errorf("onchain: outcome balance owner: %w", err)
	}
	idWord, err := eip712.UintString(tokenID)
	if err != nil {
		return nil, fmt.Errorf("onchain: outcome token id: %w", err)
	}
	return c.callUint(ctx, token, abi.EncodeCall(erc1155BalanceSig, ownerWord, idWord))
}

// callUint makes a read call that returns one 32-byte word.
func (c *Client) callUint(ctx context.Context, to string, data []byte) (*big.Int, error) {
	out, err := c.Call(ctx, CallMsg{To: to, Data: data})
	if err != nil {
		return nil, err
	}
	if len(out) < 32 {
		return nil, fmt.Errorf("onchain: call returned %d bytes, want at least 32", len(out))
	}
	return new(big.Int).SetBytes(out[:32]), nil
}

// A TokenStandard names which of the two approval mechanisms an approval uses.
type TokenStandard int

const (
	// ERC20 is the collateral: an allowance denominated in an amount.
	ERC20 TokenStandard = iota
	// ERC1155 is the outcome tokens: an all-or-nothing operator flag.
	ERC1155
)

// An Approval is one permission a Polymarket account grants an exchange
// contract. Trading needs both standards approved for every exchange the
// account's orders may be matched by, which is why this is a list and not a
// single call.
type Approval struct {
	// Standard selects the approval mechanism.
	Standard TokenStandard
	// Token is the token contract the approval is sent to: the collateral
	// for ERC20, the conditional tokens for ERC1155.
	Token string
	// Spender is the contract being approved.
	Spender string
	// Name identifies the spender for a human reading a plan.
	Name string
}

// Data builds the calldata that grants this approval. The amount applies only
// to an ERC-20 approval and is ignored for ERC-1155, which has no amount.
func (a Approval) Data(amount *big.Int) ([]byte, error) {
	switch a.Standard {
	case ERC20:
		return ApproveData(a.Spender, amount)
	case ERC1155:
		return SetApprovalForAllData(a.Spender, true)
	}
	return nil, fmt.Errorf("onchain: unknown token standard %d", a.Standard)
}

// RevokeData builds the calldata that takes this approval back: an allowance
// of zero, or an operator flag of false.
func (a Approval) RevokeData() ([]byte, error) {
	switch a.Standard {
	case ERC20:
		return ApproveData(a.Spender, new(big.Int))
	case ERC1155:
		return SetApprovalForAllData(a.Spender, false)
	}
	return nil, fmt.Errorf("onchain: unknown token standard %d", a.Standard)
}

// Transaction returns the unsigned transaction that grants this approval, with
// the fields a node fills left empty for Fill or Send.
func (a Approval) Transaction(amount *big.Int) (Transaction, error) {
	data, err := a.Data(amount)
	if err != nil {
		return Transaction{}, err
	}
	return Transaction{To: a.Token, Data: data}, nil
}

// RequiredApprovals lists the approvals an address must grant before it can
// trade on Polymarket with its own key.
//
// Every exchange version the chain has deployed is included, because an order
// is matched by the exchange its signature names and a caller trading on more
// than one needs all of them. An exchange the chain does not have is skipped
// rather than listed with an empty address.
//
// A smart wallet does not need these: the relayer package grants the same
// approvals as gasless calls made by the wallet itself.
func RequiredApprovals(c polymarket.Contracts) []Approval {
	spenders := []approvalSpender{
		{"exchange", c.Exchange},
		{"neg-risk exchange", c.NegRiskExchange},
		{"exchange v2", c.ExchangeV2},
		{"neg-risk exchange v2", c.NegRiskExchangeV2},
		{"exchange v3", c.ExchangeV3},
		{"neg-risk adapter", c.NegRiskAdapter},
	}
	var out []Approval
	for _, s := range spenders {
		if s.address == "" || c.Collateral == "" {
			continue
		}
		out = append(out, Approval{
			Standard: ERC20,
			Token:    c.Collateral,
			Spender:  s.address,
			Name:     s.name,
		})
	}
	for _, s := range spenders {
		if s.address == "" || c.ConditionalTokens == "" {
			continue
		}
		out = append(out, Approval{
			Standard: ERC1155,
			Token:    c.ConditionalTokens,
			Spender:  s.address,
			Name:     s.name,
		})
	}
	return out
}

// An approvalSpender pairs a contract with the name a human reads it by.
type approvalSpender struct {
	name    string
	address string
}

// MissingApprovals reads the current state and returns the approvals an owner
// has not granted yet. An ERC-20 allowance counts as granted when it is at
// least want; pass nil to accept any non-zero allowance.
//
// It reads only: it issues one eth_call per approval and sends nothing.
func (c *Client) MissingApprovals(ctx context.Context, owner string, want *big.Int) ([]Approval, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return nil, fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	var missing []Approval
	for _, a := range RequiredApprovals(contracts) {
		switch a.Standard {
		case ERC20:
			have, err := c.Allowance(ctx, a.Token, owner, a.Spender)
			if err != nil {
				return nil, err
			}
			if want == nil {
				if have.Sign() == 0 {
					missing = append(missing, a)
				}
			} else if have.Cmp(want) < 0 {
				missing = append(missing, a)
			}
		case ERC1155:
			approved, err := c.IsApprovedForAll(ctx, a.Token, owner, a.Spender)
			if err != nil {
				return nil, err
			}
			if !approved {
				missing = append(missing, a)
			}
		}
	}
	return missing, nil
}

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

// The deposit wallet's own read functions, and the factory's predictions.
//
// There is deliberately no builder here for the wallet's execute: see the
// package doc. A deposit wallet performs a batch only when the factory asks
// it to, and the factory takes that request only from a Polymarket operator,
// so calldata built for it here would revert whoever sent it.
const (
	walletNonceSig   = "nonce()"
	walletOwnerSig   = "owner()"
	predictWalletSig = "predictWalletAddress(bytes32)"
	predictLegacySig = "predictLegacyWalletAddress(bytes32)"
)

// WalletNonce reads a deposit wallet's batch counter, the value the next batch
// must be signed with.
//
// It is the wallet's own counter and not the sending key's transaction count,
// which is what Nonce reports. The relayer answers the same question; reading
// it from the chain needs no Polymarket host and no credentials.
func (c *Client) WalletNonce(ctx context.Context, wallet string) (*big.Int, error) {
	return c.callUint(ctx, wallet, abi.Selector(walletNonceSig))
}

// WalletOwner reads which key controls a deposit wallet.
func (c *Client) WalletOwner(ctx context.Context, wallet string) (string, error) {
	out, err := c.Call(ctx, CallMsg{To: wallet, Data: abi.Selector(walletOwnerSig)})
	if err != nil {
		return "", err
	}
	if len(out) < 32 {
		return "", fmt.Errorf("onchain: owner: call returned %d bytes, want 32", len(out))
	}
	return polymarket.ChecksumAddress(hexData(out[12:32])), nil
}

// Deployed reports whether an address holds contract code. A smart wallet's
// address is fixed by its owner and exists as a derivation long before it
// exists on chain; until it is deployed, a call to it reverts with nothing to
// explain why.
func (c *Client) Deployed(ctx context.Context, address string) (bool, error) {
	if _, err := normalizeAddress(address); err != nil {
		return false, err
	}
	var out string
	if err := c.call(ctx, "eth_getCode", []any{address, BlockLatest}, &out); err != nil {
		return false, err
	}
	code, err := parseData(out)
	if err != nil {
		return false, err
	}
	return len(code) > 0, nil
}

// PredictDepositWallet asks the factory which address an owner's deposit
// wallet has, under the current beacon layout.
//
// The same address comes out of polymarket.DeriveDepositWallet with no network
// call at all. This is here to check that derivation against the contract that
// decides it — the two agreeing is what makes the offline one safe to trust.
func (c *Client) PredictDepositWallet(ctx context.Context, owner string) (string, error) {
	return c.predict(ctx, predictWalletSig, owner)
}

// PredictLegacyDepositWallet asks the factory for the pre-upgrade address of
// an owner's deposit wallet, which is where an account opened before the June
// 2026 beacon upgrade lives.
func (c *Client) PredictLegacyDepositWallet(ctx context.Context, owner string) (string, error) {
	return c.predict(ctx, predictLegacySig, owner)
}

// predict makes one of the factory's address predictions. The wallet id is the
// owner's address widened to a word, which is also what the CREATE2 salt is
// derived from.
func (c *Client) predict(ctx context.Context, signature, owner string) (string, error) {
	contracts, ok := c.Contracts()
	if !ok {
		return "", fmt.Errorf("onchain: no known contracts for chain %d", c.ChainID())
	}
	if contracts.DepositWalletFactory == "" {
		return "", fmt.Errorf("onchain: no deposit wallet factory on chain %d", c.ChainID())
	}
	walletID, err := eip712.Address(owner)
	if err != nil {
		return "", fmt.Errorf("onchain: deposit wallet owner: %w", err)
	}
	out, err := c.Call(ctx, CallMsg{
		To:   contracts.DepositWalletFactory,
		Data: abi.EncodeCall(signature, walletID),
	})
	if err != nil {
		return "", err
	}
	if len(out) < 32 {
		return "", fmt.Errorf("onchain: predicted address: call returned %d bytes, want 32", len(out))
	}
	return polymarket.ChecksumAddress(hexData(out[12:32])), nil
}

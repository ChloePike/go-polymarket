// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"fmt"
	"math/big"

	"github.com/ChloePike/go-polymarket/internal/abi"
	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// Most Polymarket accounts are not the key that signs for them.
//
// Signing up through Google or an email link, or connecting MetaMask, gives
// the account a smart contract that holds the funds; the key only authorises
// it. That contract is the order's maker, the key is its signer, and the
// address a caller sees on polymarket.com is the contract's, not the key's.
// Sending an order with the key as maker asks the exchange to spend a balance
// that lives somewhere else.
//
// Every one of these wallets is deployed with CREATE2 at an address fixed by
// its owner, so the address can be derived offline — before the wallet is
// deployed, and without asking anything. That is what this file does.

// Init code hashes for the two legacy factories, each the keccak256 of the
// creation code its factory deploys. They are constants of the deployment, not
// of this client: the factory bytecode fixes them, and the addresses below
// were derived from the deployed factories.
const (
	proxyInitCodeHash = "0xd21df8dc65880a8606f09fe0ce3df9b8869287ab0b058be05aa9e8af6330a00b"
	safeInitCodeHash  = "0x2bce2127ff07fb632d16c8347c4ebf501f4841168bed00d9e6ef715ddb6fcecf"
)

// Byte constants of the minimal ERC-1967 proxy that the deposit-wallet factory
// deploys, as Solady's LibClone assembles it. Between them they spell out the
// whole creation code except the implementation or beacon address and the
// trailing constructor arguments, which is what makes the init code hash — and
// so the wallet address — computable without holding the bytecode.
//
// The prefixes begin with PUSH2 over the creation code's length, which is why
// the length of the arguments is added into them rather than appended.
const (
	erc1967Const1 = "0xcc3735a920a3ca505d382bbc545af43d6000803e6038573d6000fd5b3d6000f3"
	erc1967Const2 = "0x5155f3363d3d373d3d363d7f360894a13ba1a3210667c828492db98dca3e2076"
	erc1967Prefix = "0x61003d3d8160233d3973"

	erc1967BeaconConst1 = "0xb3582b35133d50545afa5036515af43d6000803e604d573d6000fd5b3d6000f3"
	erc1967BeaconConst2 = "0x1b60e01b36527fa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6c"
	erc1967BeaconConst3 = "0x60195155f3363d3d373d3d363d602036600436635c60da"
	erc1967BeaconPrefix = "0x6100523d8160233d3973"
)

// A Wallet is an account form together with the address it trades from.
//
// Build one with NewWallet and pass its OrderOptions to every order: it sets
// both the signature type and the funder, which are the two fields that have
// to agree for a smart-wallet order to be accepted.
type Wallet struct {
	// SignatureType is the verification path the exchange takes for this
	// account form.
	SignatureType SignatureType

	// Owner is the key that signs. For an EOA it is also the address holding
	// the funds.
	Owner string

	// Address is the wallet that holds the funds and makes the order. For an
	// EOA it equals Owner.
	Address string
}

// NewWallet derives the wallet an owner key controls under one signature type.
//
// The owner is the address of the signing key — Signer.Address, or what a
// wallet extension reports — and never the account address shown on
// polymarket.com, which is the result.
//
// SigEIP1271 derives the current deposit wallet, the one every account created
// since May 2026 uses. An account created before the June 2026 upgrade sits at
// a different address; derive that one with DeriveDepositWalletUUPS.
func NewWallet(sigType SignatureType, owner string, chainID int64) (Wallet, error) {
	contracts, ok := ContractsFor(chainID)
	if !ok {
		return Wallet{}, fmt.Errorf("polymarket: unknown chain %d", chainID)
	}
	address, err := DeriveWallet(sigType, owner, contracts)
	if err != nil {
		return Wallet{}, err
	}
	return Wallet{SignatureType: sigType, Owner: ChecksumAddress(owner), Address: address}, nil
}

// OrderOptions returns the two order fields this wallet decides. Merge them
// into the options an order is built with:
//
//	opts := wallet.OrderOptions()
//	opts.TickSize = tickSize
//	order, err := polymarket.BuildOrder(signer, user, chainID, opts)
func (w Wallet) OrderOptions() OrderOptions {
	opts := OrderOptions{SignatureType: w.SignatureType}
	if w.SignatureType != SigEOA {
		opts.Funder = w.Address
	}
	return opts
}

// DeriveWallet returns the address a signature type's wallet deploys to for
// one owner. An EOA has no wallet, so it derives to itself.
func DeriveWallet(sigType SignatureType, owner string, c Contracts) (string, error) {
	switch sigType {
	case SigEOA:
		if _, err := eip712.Address(owner); err != nil {
			return "", fmt.Errorf("polymarket: wallet owner: %w", err)
		}
		return ChecksumAddress(owner), nil
	case SigPolyProxy:
		return DeriveProxyWallet(owner, c.ProxyFactory)
	case SigPolyGnosisSafe:
		return DeriveSafeWallet(owner, c.SafeFactory)
	case SigEIP1271:
		return DeriveDepositWallet(owner, c.DepositWalletFactory, c.DepositWalletBeacon)
	}
	return "", fmt.Errorf("polymarket: unknown signature type %d", sigType)
}

// DeriveProxyWallet returns the Polymarket proxy wallet an owner controls, the
// account form behind a Magic Link or Google sign-in.
//
// The factory salts CREATE2 with keccak256 over the owner's twenty raw bytes.
// The Safe factory below salts it with the address padded to a full word
// instead, so the two produce different wallets for the same owner and neither
// salt is interchangeable with the other.
func DeriveProxyWallet(owner, factory string) (string, error) {
	if factory == "" {
		return "", fmt.Errorf("polymarket: no proxy factory on this chain")
	}
	packed, err := abi.PackedAddress(owner)
	if err != nil {
		return "", fmt.Errorf("polymarket: proxy wallet owner: %w", err)
	}
	initCodeHash, err := eip712.Bytes32(proxyInitCodeHash)
	if err != nil {
		return "", err
	}
	address, err := abi.Create2(factory, eip712.Keccak256(packed), initCodeHash)
	if err != nil {
		return "", err
	}
	return ChecksumAddress(address), nil
}

// DeriveSafeWallet returns the Polymarket Gnosis Safe an owner controls, the
// account form behind connecting an external signer such as MetaMask.
func DeriveSafeWallet(owner, factory string) (string, error) {
	if factory == "" {
		return "", fmt.Errorf("polymarket: no Safe factory on this chain")
	}
	word, err := eip712.Address(owner)
	if err != nil {
		return "", fmt.Errorf("polymarket: Safe owner: %w", err)
	}
	initCodeHash, err := eip712.Bytes32(safeInitCodeHash)
	if err != nil {
		return "", err
	}
	address, err := abi.Create2(factory, eip712.Keccak256(word[:]), initCodeHash)
	if err != nil {
		return "", err
	}
	return ChecksumAddress(address), nil
}

// DeriveDepositWallet returns the deposit wallet an owner controls: the
// current account form, used by everything created since May 2026.
//
// This derives the beacon-proxy wallet of the June 2026 upgrade. A wallet
// created before it points straight at an implementation and lives at a
// different address; DeriveDepositWalletUUPS derives that one.
func DeriveDepositWallet(owner, factory, beacon string) (string, error) {
	args, salt, err := depositWalletArgs(owner, factory)
	if err != nil {
		return "", err
	}
	initCodeHash, err := beaconInitCodeHash(beacon, args)
	if err != nil {
		return "", err
	}
	address, err := abi.Create2(factory, salt, initCodeHash)
	if err != nil {
		return "", err
	}
	return ChecksumAddress(address), nil
}

// DeriveDepositWalletUUPS returns the deposit wallet an owner controls under
// the pre-upgrade layout, where the proxy names its implementation directly.
// Use it only for an account created before the June 2026 beacon upgrade.
func DeriveDepositWalletUUPS(owner, factory, implementation string) (string, error) {
	args, salt, err := depositWalletArgs(owner, factory)
	if err != nil {
		return "", err
	}
	initCodeHash, err := uupsInitCodeHash(implementation, args)
	if err != nil {
		return "", err
	}
	address, err := abi.Create2(factory, salt, initCodeHash)
	if err != nil {
		return "", err
	}
	return ChecksumAddress(address), nil
}

// depositWalletArgs builds the constructor arguments a deposit wallet is
// deployed with, and the CREATE2 salt, which is their hash. The wallet id is
// the owner's address widened to a word rather than the address itself.
func depositWalletArgs(owner, factory string) ([]byte, eip712.Word, error) {
	if factory == "" {
		return nil, eip712.Word{}, fmt.Errorf("polymarket: no deposit wallet factory on this chain")
	}
	factoryWord, err := eip712.Address(factory)
	if err != nil {
		return nil, eip712.Word{}, fmt.Errorf("polymarket: deposit wallet factory: %w", err)
	}
	walletID, err := eip712.Address(owner)
	if err != nil {
		return nil, eip712.Word{}, fmt.Errorf("polymarket: deposit wallet owner: %w", err)
	}
	args := abi.Encode(factoryWord, walletID)
	return args, eip712.Keccak256(args), nil
}

// uupsInitCodeHash reproduces Solady LibClone.initCodeHashERC1967.
func uupsInitCodeHash(implementation string, args []byte) (eip712.Word, error) {
	prefix, err := soladyPrefix(erc1967Prefix, len(args))
	if err != nil {
		return eip712.Word{}, err
	}
	target, err := abi.PackedAddress(implementation)
	if err != nil {
		return eip712.Word{}, fmt.Errorf("polymarket: deposit wallet implementation: %w", err)
	}
	const1, err := eip712.HexBytes(erc1967Const1)
	if err != nil {
		return eip712.Word{}, err
	}
	const2, err := eip712.HexBytes(erc1967Const2)
	if err != nil {
		return eip712.Word{}, err
	}
	return eip712.Keccak256(prefix, target, []byte{0x60, 0x09}, const2, const1, args), nil
}

// beaconInitCodeHash reproduces Solady LibClone.initCodeHashERC1967Beacon.
func beaconInitCodeHash(beacon string, args []byte) (eip712.Word, error) {
	prefix, err := soladyPrefix(erc1967BeaconPrefix, len(args))
	if err != nil {
		return eip712.Word{}, err
	}
	target, err := abi.PackedAddress(beacon)
	if err != nil {
		return eip712.Word{}, fmt.Errorf("polymarket: deposit wallet beacon: %w", err)
	}
	const1, err := eip712.HexBytes(erc1967BeaconConst1)
	if err != nil {
		return eip712.Word{}, err
	}
	const2, err := eip712.HexBytes(erc1967BeaconConst2)
	if err != nil {
		return eip712.Word{}, err
	}
	const3, err := eip712.HexBytes(erc1967BeaconConst3)
	if err != nil {
		return eip712.Word{}, err
	}
	return eip712.Keccak256(prefix, target, const3, const2, const1, args), nil
}

// soladyPrefix adds the argument length into a creation-code prefix.
//
// The prefix opens with PUSH2 over the total length of the code being
// deployed, and the arguments are part of that total. Bit 56 is the low half
// of that operand in a ten-byte prefix, so the length is added there rather
// than concatenated anywhere.
func soladyPrefix(prefix string, argLen int) ([]byte, error) {
	base, ok := new(big.Int).SetString(prefix[2:], 16)
	if !ok {
		return nil, fmt.Errorf("polymarket: bad creation-code prefix %q", prefix)
	}
	base.Add(base, new(big.Int).Lsh(big.NewInt(int64(argLen)), 56))
	return abi.PackedUint(base, 10)
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"fmt"
)

// A smart-contract wallet has no private key, so it cannot produce the
// signature the exchange checks. It verifies one instead, through EIP-1271:
// the exchange hands the wallet a hash and a blob of bytes, and the wallet
// answers whether it considers them authorised.
//
// Polymarket's deposit wallet answers using ERC-7739, which exists to stop one
// signature being replayed into a different contract. Rather than signing the
// order's own digest, the owner signs a TypedDataSign struct that carries the
// order inside it along with the domain of the wallet being asked. A signature
// made for one wallet therefore names that wallet, and presenting it to
// another fails.
//
// The wrapper the exchange receives is not 65 bytes. It carries everything the
// wallet needs to recompute the digest for itself:
//
//	innerSig(65) ‖ appDomainSeparator(32) ‖ contentsHash(32) ‖ typeString ‖ len(typeString)(2)
//
// One detail is worth stating because it looks like a mistake. The inner
// signature is made against the *exchange's* domain, while the wallet's own
// domain — name, version, verifying contract — travels as fields of the
// message. That is the opposite of the arrangement the ERC's own text
// describes, and it is what Polymarket's contracts verify; an implementation
// corrected toward the specification produces signatures the exchange refuses.

// The wallet domain named inside the signed message. It describes the wallet
// being asked to authorise, not the contract the signature is bound to.
const (
	depositWalletDomainName    = "DepositWallet"
	depositWalletDomainVersion = "1"
)

// typedDataSignFields is the ERC-7739 wrapper struct. contents is the order
// itself, and the four fields after it are the wallet's EIP-712 domain spread
// out as ordinary message fields.
var typedDataSignFields = []TypedDataField{
	{Name: "contents", Type: "Order"},
	{Name: "name", Type: "string"},
	{Name: "version", Type: "string"},
	{Name: "chainId", Type: "uint256"},
	{Name: "verifyingContract", Type: "address"},
	{Name: "salt", Type: "bytes32"},
}

// WrappedOrderTypedData returns the EIP-712 payload a smart-contract wallet's
// owner actually signs, which is not the order payload.
//
// Use it wherever OrderTypedData would be used for an ordinary account: an
// audit log, a policy engine, or a hardware wallet's display. The order is
// still all there, nested under "contents", and an auditor reading it should
// expect to find the wallet's own domain repeated as message fields.
//
// The wallet's address is taken from the order's Signer field, which for this
// signature type names the wallet rather than the key.
func WrappedOrderTypedData(o Order, chainID int64, opts OrderOptions) (TypedData, error) {
	inner, err := OrderTypedData(o, chainID, opts)
	if err != nil {
		return TypedData{}, err
	}
	return TypedData{
		Types: map[string][]TypedDataField{
			"EIP712Domain":  inner.Domain.domainType(),
			"TypedDataSign": cloneFields(typedDataSignFields),
			"Order":         cloneFields(orderStructFields),
		},
		PrimaryType: "TypedDataSign",
		Domain:      inner.Domain,
		Message: map[string]any{
			"contents":          inner.Message,
			"name":              depositWalletDomainName,
			"version":           depositWalletDomainVersion,
			"chainId":           chainID,
			"verifyingContract": o.Signer,
			"salt":              ZeroBytes32,
		},
	}, nil
}

// WrapOrderSignature assembles the bytes the exchange passes to a
// smart-contract wallet, given a signature over WrappedOrderTypedData.
//
// It is exported for the caller whose wallet is not Polymarket's deposit
// wallet: sign whatever that contract expects, and pass the result here to get
// the surrounding layout, or ignore this entirely and build SignedOrder
// yourself. SignOrder calls it for every SigEIP1271 order.
func WrapOrderSignature(inner []byte, o Order, chainID int64, opts OrderOptions) (string, error) {
	domain, err := orderDomain(chainID, opts.version(), opts.NegRisk)
	if err != nil {
		return "", err
	}
	separator, err := domain.Separator()
	if err != nil {
		return "", err
	}
	td, err := OrderTypedData(o, chainID, opts)
	if err != nil {
		return "", err
	}
	contents, err := td.StructHash()
	if err != nil {
		return "", fmt.Errorf("polymarket: order: %w", err)
	}

	// The type string travels in full so the wallet can check that the
	// contents hash it is being shown is a hash of the struct it expects, and
	// its length trails the whole blob because the wallet reads backwards
	// from the end. Two bytes big-endian, so a type string is bounded.
	if len(orderTypeString) > 0xffff {
		return "", fmt.Errorf("polymarket: order type string is %d bytes, too long to describe in two", len(orderTypeString))
	}
	out := make([]byte, 0, len(inner)+64+len(orderTypeString)+2)
	out = append(out, inner...)
	out = append(out, separator[:]...)
	out = append(out, contents[:]...)
	out = append(out, orderTypeString...)
	out = append(out, byte(len(orderTypeString)>>8), byte(len(orderTypeString)))
	return "0x" + hex.EncodeToString(out), nil
}

// signWrappedOrder signs an order on behalf of a smart-contract wallet.
func signWrappedOrder(o Order, chainID int64, opts OrderOptions, s Signer) (SignedOrder, error) {
	td, err := WrappedOrderTypedData(o, chainID, opts)
	if err != nil {
		return SignedOrder{}, err
	}
	inner, err := signTypedData(s, td)
	if err != nil {
		return SignedOrder{}, fmt.Errorf("polymarket: signing order for wallet %s: %w", o.Signer, err)
	}
	signature, err := WrapOrderSignature(inner, o, chainID, opts)
	if err != nil {
		return SignedOrder{}, err
	}
	return SignedOrder{Order: o, Signature: signature}, nil
}

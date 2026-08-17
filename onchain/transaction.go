// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	polymarket "github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/internal/eip712"
	"github.com/ChloePike/go-polymarket/internal/rlp"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// TxTypeEIP1559 is the leading byte of a type-2 transaction. It is part of
// what is signed: the same fields under a different type byte are a different
// transaction, which is what makes the typed forms non-interchangeable.
const TxTypeEIP1559 = 0x02

// A Transaction is an EIP-1559 transaction waiting to be signed.
//
// The access list is always empty. It is an optimisation — prepaying for
// storage a call will touch — and an empty one is always valid, so it stays
// out of this API rather than becoming a field nobody sets correctly. It is
// still part of the signature, as the empty list.
type Transaction struct {
	// ChainID is the chain this transaction is valid on, and nowhere else.
	ChainID *big.Int
	// Nonce is the sender's transaction count. Two transactions from one
	// address with the same nonce compete, and only one is mined.
	Nonce uint64
	// MaxPriorityFeePerGas is the tip to the block producer.
	MaxPriorityFeePerGas *big.Int
	// MaxFeePerGas caps the total price per gas.
	MaxFeePerGas *big.Int
	// Gas is the execution limit. Gas the transaction does not use is not
	// charged; a limit the transaction exceeds reverts it and charges all
	// of it.
	Gas uint64
	// To is the recipient. Empty deploys a contract.
	To string
	// Value is the native amount to send, in wei.
	Value *big.Int
	// Data is the calldata.
	Data []byte
}

// fields returns the nine RLP items every type-2 transaction is defined over,
// in the order the specification fixes. The order is the whole of the format:
// two fields swapped produce a signature that recovers to a stranger.
func (t Transaction) fields() ([][]byte, error) {
	if t.ChainID == nil || t.ChainID.Sign() <= 0 {
		return nil, fmt.Errorf("onchain: transaction needs a positive chain id")
	}
	if t.MaxFeePerGas == nil || t.MaxPriorityFeePerGas == nil {
		return nil, fmt.Errorf("onchain: transaction needs both fee fields; call SuggestFees or Fill")
	}
	if t.MaxFeePerGas.Cmp(t.MaxPriorityFeePerGas) < 0 {
		return nil, fmt.Errorf("onchain: max fee %s is below the priority fee %s, which every node rejects",
			t.MaxFeePerGas, t.MaxPriorityFeePerGas)
	}
	if t.Gas == 0 {
		return nil, fmt.Errorf("onchain: transaction needs a gas limit; call EstimateGas or Fill")
	}

	chainID, err := rlp.Uint(t.ChainID)
	if err != nil {
		return nil, fmt.Errorf("onchain: chain id: %w", err)
	}
	tip, err := rlp.Uint(t.MaxPriorityFeePerGas)
	if err != nil {
		return nil, fmt.Errorf("onchain: priority fee: %w", err)
	}
	maxFee, err := rlp.Uint(t.MaxFeePerGas)
	if err != nil {
		return nil, fmt.Errorf("onchain: max fee: %w", err)
	}
	value, err := rlp.Uint(t.Value)
	if err != nil {
		return nil, fmt.Errorf("onchain: value: %w", err)
	}

	// An empty recipient is the empty string, not twenty zero bytes: the
	// zero address is a real account, and sending there burns the value.
	var to []byte
	if t.To != "" {
		address, err := normalizeAddress(t.To)
		if err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(address[2:])
		if err != nil {
			return nil, fmt.Errorf("onchain: recipient: %w", err)
		}
		to = raw
	}

	return [][]byte{
		chainID,
		rlp.Uint64(t.Nonce),
		tip,
		maxFee,
		rlp.Uint64(t.Gas),
		rlp.String(to),
		value,
		rlp.String(t.Data),
		rlp.List(), // the empty access list
	}, nil
}

// Unsigned returns the bytes a signature covers: the type byte followed by the
// RLP list of the nine fields.
func (t Transaction) Unsigned() ([]byte, error) {
	fields, err := t.fields()
	if err != nil {
		return nil, err
	}
	return append([]byte{TxTypeEIP1559}, rlp.List(fields...)...), nil
}

// SigningHash returns the digest a sender signs.
func (t Transaction) SigningHash() ([32]byte, error) {
	unsigned, err := t.Unsigned()
	if err != nil {
		return [32]byte{}, err
	}
	return eip712.Keccak256(unsigned), nil
}

// A SignedTransaction is a transaction with its signature, ready to broadcast.
type SignedTransaction struct {
	// Transaction is what was signed. Changing a field of it now changes
	// nothing about Raw, which is the copy that counts.
	Transaction Transaction
	// From is the address the signature recovers to.
	From string
	// Hash is the transaction hash: keccak256 over Raw. A node reports the
	// same hash back from eth_sendRawTransaction, so the two can be
	// compared before waiting on a receipt.
	Hash string
	// Raw is the encoded signed transaction, the argument to SendRaw.
	Raw []byte
}

// RawHex returns Raw as a 0x-prefixed hex string, the form a node and a block
// explorer both accept.
func (s SignedTransaction) RawHex() string { return hexData(s.Raw) }

// SignTransaction signs a transaction and returns it encoded.
//
// It signs and encodes only. Nothing is sent, and no node is contacted, so
// this is safe to call to inspect what a transaction would look like.
func SignTransaction(signer polymarket.Signer, t Transaction) (SignedTransaction, error) {
	if signer == nil {
		return SignedTransaction{}, polymarket.ErrNoSigner
	}
	fields, err := t.fields()
	if err != nil {
		return SignedTransaction{}, err
	}
	digest, err := t.SigningHash()
	if err != nil {
		return SignedTransaction{}, err
	}

	sig, err := signer.SignDigest(digest)
	if err != nil {
		return SignedTransaction{}, fmt.Errorf("onchain: signing transaction: %w", err)
	}
	if err := polymarket.VerifySignature(digest, sig, signer.Address()); err != nil {
		return SignedTransaction{}, fmt.Errorf("onchain: signing transaction: %w", err)
	}

	yParity, r, s, err := splitSignature(sig)
	if err != nil {
		return SignedTransaction{}, err
	}
	raw := append([]byte{TxTypeEIP1559}, rlp.List(append(fields, yParity, r, s)...)...)
	hash := eip712.Keccak256(raw)

	return SignedTransaction{
		Transaction: t,
		From:        signer.Address(),
		Hash:        "0x" + hex.EncodeToString(hash[:]),
		Raw:         raw,
	}, nil
}

// splitSignature turns the 65-byte r ‖ s ‖ v this module signs with into the
// three items a typed transaction carries.
//
// The trap is v. Every other signature in this client keeps Ethereum's 27 or
// 28, because that is what the contracts verifying them expect. A typed
// transaction does not: it stores the parity bit itself, 0 or 1. Passing 27
// through unchanged encodes a transaction whose signature recovers to a
// different address, and a node answers "invalid sender" rather than anything
// that names the cause.
func splitSignature(sig []byte) (yParity, r, s []byte, err error) {
	if len(sig) != 65 {
		return nil, nil, nil, fmt.Errorf("onchain: signature is %d bytes, want 65", len(sig))
	}
	v := sig[64]
	if v != 27 && v != 28 {
		return nil, nil, nil, fmt.Errorf("onchain: signature v is %d, want 27 or 28", v)
	}

	rv := new(big.Int).SetBytes(sig[:32])
	sv := new(big.Int).SetBytes(sig[32:64])

	// EIP-2 admits only the lower of the two equivalent s values, and a node
	// drops a transaction carrying the other one. A Signer is required to be
	// canonical already; this catches one that is not, here rather than in
	// an unexplained rejection.
	if sv.Cmp(halfCurveOrder) > 0 {
		return nil, nil, nil, fmt.Errorf("onchain: signature s is not canonical, so a node would reject the transaction")
	}
	if rv.Sign() == 0 || sv.Sign() == 0 {
		return nil, nil, nil, fmt.Errorf("onchain: signature has a zero component")
	}

	rItem, err := rlp.Uint(rv)
	if err != nil {
		return nil, nil, nil, err
	}
	sItem, err := rlp.Uint(sv)
	if err != nil {
		return nil, nil, nil, err
	}
	return rlp.Uint64(uint64(v - 27)), rItem, sItem, nil
}

// halfCurveOrder is the largest canonical s under EIP-2: half the order of the
// secp256k1 group, rounded down.
var halfCurveOrder = new(big.Int).Rsh(secp256k1.Params().N, 1)

// Fill completes the fields of a transaction that a node knows better than the
// caller: the chain id, the nonce, the fees and the gas limit. A field the
// caller has already set is left alone — except a nonce of zero, which is
// indistinguishable from an unset one and is read again. That is right for a
// first transaction and wrong for one deliberately pinned to zero to replace a
// stuck one; build that transaction without Fill.
//
// It issues up to four requests and sends nothing. The gas limit it fills is
// the node's estimate plus a quarter, because the estimate is a simulation of
// the current state and the transaction executes against a later one.
func (c *Client) Fill(ctx context.Context, from string, t Transaction) (Transaction, error) {
	if t.ChainID == nil {
		t.ChainID = big.NewInt(c.ChainID())
	}
	if t.Value == nil {
		t.Value = new(big.Int)
	}
	if t.Nonce == 0 {
		nonce, err := c.Nonce(ctx, from)
		if err != nil {
			return t, err
		}
		t.Nonce = nonce
	}
	if t.MaxFeePerGas == nil || t.MaxPriorityFeePerGas == nil {
		fees, err := c.SuggestFees(ctx)
		if err != nil {
			return t, err
		}
		if t.MaxPriorityFeePerGas == nil {
			t.MaxPriorityFeePerGas = fees.MaxPriorityFeePerGas
		}
		if t.MaxFeePerGas == nil {
			t.MaxFeePerGas = fees.MaxFeePerGas
		}
	}
	if t.Gas == 0 {
		estimate, err := c.EstimateGas(ctx, CallMsg{From: from, To: t.To, Value: t.Value, Data: t.Data})
		if err != nil {
			return t, err
		}
		t.Gas = estimate + estimate/4
	}
	return t, nil
}

// Send fills in what the transaction is missing, signs it, and broadcasts it.
//
// This spends real money and cannot be undone. A caller that wants to see the
// transaction first should call Fill and SignTransaction, which contact no
// node and send nothing, and hand the result to SendRaw when satisfied.
//
// The returned transaction has been accepted by the node, not mined: pass its
// Hash to WaitReceipt, and check the receipt's Succeeded, because a reverted
// transaction still costs its gas.
func (c *Client) Send(ctx context.Context, signer polymarket.Signer, t Transaction) (SignedTransaction, error) {
	if signer == nil {
		return SignedTransaction{}, polymarket.ErrNoSigner
	}
	filled, err := c.Fill(ctx, signer.Address(), t)
	if err != nil {
		return SignedTransaction{}, err
	}
	signed, err := SignTransaction(signer, filled)
	if err != nil {
		return SignedTransaction{}, err
	}
	hash, err := c.SendRaw(ctx, signed.Raw)
	if err != nil {
		return SignedTransaction{}, err
	}
	// A node that reports a different hash has not accepted the transaction
	// that was signed, and waiting on the local hash would wait forever.
	if !equalHex(hash, signed.Hash) {
		return signed, fmt.Errorf("onchain: node accepted %s but the signed transaction hashes to %s", hash, signed.Hash)
	}
	return signed, nil
}

// equalHex compares two hex strings ignoring case and prefix.
func equalHex(a, b string) bool {
	return normalizeHex(a) == normalizeHex(b)
}

// normalizeHex lowercases a hex string and drops its prefix.
func normalizeHex(s string) string {
	body := s
	if len(body) >= 2 && (body[:2] == "0x" || body[:2] == "0X") {
		body = body[2:]
	}
	out := []byte(body)
	for i, c := range out {
		if c >= 'A' && c <= 'F' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}

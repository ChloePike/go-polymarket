// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package relayer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/internal/abi"
	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// A smart wallet cannot pay gas, so it does not send its own transactions.
// The owner signs a description of what the wallet should do, the relayer
// wraps that in a real transaction and pays for it, and the wallet checks the
// signature before acting. This file builds and signs those descriptions.
//
// The three account forms sign three different things, and none of them is an
// order:
//
//   - a Safe signs a SafeTx struct, EIP-712, under a domain that carries only
//     the chain and the Safe's own address;
//   - a proxy signs a packed hash that is not typed data at all;
//   - a deposit wallet signs a Batch struct, EIP-712, under the wallet's
//     domain.
//
// The first two are then signed as *personal messages* — the prefixed
// keccak256 that eth_sign produces — because that is what the contracts
// recover. Signing their digests directly gives a signature that recovers to
// an address nobody expects, and the relayer accepts it: the failure appears
// on chain.

// Relayer transaction paths.
const epSubmit = "/submit"

// A TransactionType names which contract will carry out a submitted
// transaction. It is not the same enumeration as WalletType: creating a
// wallet and using one are different types here and the same wallet there.
type TransactionType string

const (
	// TransactionTypeSafe executes through a Gnosis Safe.
	TransactionTypeSafe TransactionType = "SAFE"
	// TransactionTypeProxy executes through a legacy proxy wallet.
	TransactionTypeProxy TransactionType = "PROXY"
	// TransactionTypeSafeCreate deploys a Safe.
	TransactionTypeSafeCreate TransactionType = "SAFE-CREATE"
	// TransactionTypeWallet executes a batch through a deposit wallet.
	TransactionTypeWallet TransactionType = "WALLET"
	// TransactionTypeWalletCreate deploys a deposit wallet.
	TransactionTypeWalletCreate TransactionType = "WALLET-CREATE"
)

// An Operation is how a wallet performs a call: in its own right, or with the
// target's code running against the wallet's own storage.
//
// The number on the wire is not the same for both wallets — a Safe counts
// from zero and a proxy reserves zero for "invalid" — so this type exists to
// keep one meaning with two encodings rather than two meanings with one
// number.
type Operation uint8

const (
	// OpCall is an ordinary call. It is what almost everything wants.
	OpCall Operation = iota
	// OpDelegateCall runs the target's code as the wallet. Batching uses it;
	// anything else pointed at an untrusted target hands over the wallet.
	OpDelegateCall
)

// safeCode is the number a Gnosis Safe uses for an operation.
func (o Operation) safeCode() uint8 { return uint8(o) }

// proxyCode is the number a Polymarket proxy uses. It reserves zero to mean
// invalid, so both of the real operations are one higher than the Safe's.
func (o Operation) proxyCode() uint8 { return uint8(o) + 1 }

// A Call is one contract call for a wallet to make.
//
// Value is wei as a decimal string, and empty means none. Data is 0x-prefixed
// calldata, and empty means a plain transfer.
type Call struct {
	To        string
	Value     string
	Data      string
	Operation Operation
}

// decode reads a call into the pieces every encoding needs.
func (c Call) decode() (to string, value *big.Int, data []byte, err error) {
	if _, err = eip712.Address(c.To); err != nil {
		return "", nil, nil, fmt.Errorf("relayer: call target: %w", err)
	}
	value = new(big.Int)
	if c.Value != "" {
		if _, ok := value.SetString(c.Value, 10); !ok {
			return "", nil, nil, fmt.Errorf("relayer: call value %q is not a decimal integer", c.Value)
		}
		if value.Sign() < 0 {
			return "", nil, nil, fmt.Errorf("relayer: call value %q is negative", c.Value)
		}
	}
	if c.Data != "" {
		if data, err = eip712.HexBytes(c.Data); err != nil {
			return "", nil, nil, fmt.Errorf("relayer: call data: %w", err)
		}
	}
	return c.To, value, data, nil
}

// SignatureParams are the arguments the wallet contract needs in order to
// recompute the hash that was signed. They travel beside the signature
// because the relayer, not the signer, builds the final transaction.
//
// Which fields are set depends on the wallet: a Safe fills the gas and refund
// fields it accounts with, a proxy fills the relay it is going through.
type SignatureParams struct {
	GasPrice string `json:"gasPrice,omitempty"`

	// Safe fields.
	Operation      string `json:"operation,omitempty"`
	SafeTxnGas     string `json:"safeTxnGas,omitempty"`
	BaseGas        string `json:"baseGas,omitempty"`
	GasToken       string `json:"gasToken,omitempty"`
	RefundReceiver string `json:"refundReceiver,omitempty"`

	// Proxy fields.
	RelayerFee string `json:"relayerFee,omitempty"`
	GasLimit   string `json:"gasLimit,omitempty"`
	RelayHub   string `json:"relayHub,omitempty"`
	Relay      string `json:"relay,omitempty"`
}

// A BatchCall is one call inside a deposit wallet's batch, as the wire names
// its fields.
type BatchCall struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Data   string `json:"data"`
}

// DepositWalletParams is the body a deposit wallet's batch travels in. Its
// calls are sent in full rather than as calldata, because the wallet itself
// re-encodes them.
type DepositWalletParams struct {
	DepositWallet string      `json:"depositWallet"`
	Deadline      string      `json:"deadline"`
	Calls         []BatchCall `json:"calls"`
}

// A SubmitRequest is the body POST /submit takes: a signed instruction for
// one wallet, ready to send.
//
// Build one with BuildSafeTransaction, BuildProxyTransaction or
// BuildWalletBatch rather than by hand — the signature covers most of these
// fields, so an edit after signing produces a request the wallet refuses.
type SubmitRequest struct {
	Type            TransactionType      `json:"type"`
	From            string               `json:"from"`
	To              string               `json:"to"`
	ProxyWallet     string               `json:"proxyWallet,omitempty"`
	Data            string               `json:"data,omitempty"`
	Nonce           string               `json:"nonce,omitempty"`
	Signature       string               `json:"signature,omitempty"`
	SignatureParams *SignatureParams     `json:"signatureParams,omitempty"`
	Metadata        string               `json:"metadata,omitempty"`
	Deposit         *DepositWalletParams `json:"depositWalletParams,omitempty"`
}

// A SubmitResponse is what the relayer answers a submission with. The
// transaction is queued, not sent: poll Client.Transaction with the id until
// its state is terminal.
type SubmitResponse struct {
	TransactionID string           `json:"transactionID"`
	State         TransactionState `json:"state"`
	Hash          string           `json:"hash,omitempty"`
}

// SafeParams describes a transaction for a Gnosis Safe to carry out.
type SafeParams struct {
	// Owner is the address of the key that signs. The Safe itself is derived
	// from it.
	Owner string

	// Nonce is the Safe's own transaction counter, from Client.Nonce with
	// WalletTypeSafe. A stale one is refused on chain, not here.
	Nonce string

	// Calls are what the Safe should do. Several are batched through the
	// multisend contract, which the Safe delegate-calls.
	Calls []Call

	// ChainID selects the contracts and binds the signature to one chain.
	// Zero means Polygon.
	ChainID int64

	// Metadata is passed through by the relayer.
	Metadata string
}

// safeTxFields is the Gnosis Safe transaction struct, in the order it hashes.
var safeTxFields = []polymarket.TypedDataField{
	{Name: "to", Type: "address"},
	{Name: "value", Type: "uint256"},
	{Name: "data", Type: "bytes"},
	{Name: "operation", Type: "uint8"},
	{Name: "safeTxGas", Type: "uint256"},
	{Name: "baseGas", Type: "uint256"},
	{Name: "gasPrice", Type: "uint256"},
	{Name: "gasToken", Type: "address"},
	{Name: "refundReceiver", Type: "address"},
	{Name: "nonce", Type: "uint256"},
}

// SafeTypedData returns the EIP-712 payload a Safe transaction's signature
// covers, for an audit log or an external signer to read.
//
// Its domain has no name and no version. That is the Safe's own convention,
// not an omission: adding either changes the separator and the Safe recovers
// a different address.
func SafeTypedData(p SafeParams, c polymarket.Contracts) (polymarket.TypedData, error) {
	safe, err := polymarket.DeriveSafeWallet(p.Owner, c.SafeFactory)
	if err != nil {
		return polymarket.TypedData{}, err
	}
	call, err := aggregate(p.Calls, c.SafeMultisend)
	if err != nil {
		return polymarket.TypedData{}, err
	}
	to, value, data, err := call.decode()
	if err != nil {
		return polymarket.TypedData{}, err
	}
	if p.Nonce == "" {
		return polymarket.TypedData{}, fmt.Errorf("relayer: a Safe transaction needs a nonce")
	}

	domain := polymarket.TypedDataDomain{ChainID: chainOrDefault(p.ChainID), VerifyingContract: safe}
	return polymarket.TypedData{
		Types: map[string][]polymarket.TypedDataField{
			"EIP712Domain": domain.DomainType(),
			"SafeTx":       append([]polymarket.TypedDataField(nil), safeTxFields...),
		},
		PrimaryType: "SafeTx",
		Domain:      domain,
		Message: map[string]any{
			"to":        to,
			"value":     value.String(),
			"data":      hexOrEmpty(data),
			"operation": call.Operation.safeCode(),
			// The Safe can charge a relayer for gas and refund it in a token.
			// Polymarket's relayer does not, so every one of these is zero,
			// and they are signed as zero.
			"safeTxGas":      "0",
			"baseGas":        "0",
			"gasPrice":       "0",
			"gasToken":       polymarket.ZeroAddress,
			"refundReceiver": polymarket.ZeroAddress,
			"nonce":          p.Nonce,
		},
	}, nil
}

// BuildSafeTransaction signs a transaction for a Gnosis Safe and returns the
// request that submits it.
func BuildSafeTransaction(s polymarket.Signer, p SafeParams, c polymarket.Contracts) (SubmitRequest, error) {
	if s == nil {
		return SubmitRequest{}, polymarket.ErrNoSigner
	}
	safe, err := polymarket.DeriveSafeWallet(p.Owner, c.SafeFactory)
	if err != nil {
		return SubmitRequest{}, err
	}
	call, err := aggregate(p.Calls, c.SafeMultisend)
	if err != nil {
		return SubmitRequest{}, err
	}
	td, err := SafeTypedData(p, c)
	if err != nil {
		return SubmitRequest{}, err
	}
	digest, err := td.Digest()
	if err != nil {
		return SubmitRequest{}, fmt.Errorf("relayer: Safe transaction: %w", err)
	}

	// A Safe accepts a signature made over the prefixed personal-message hash
	// of its struct digest, and marks that choice by raising v. It is not the
	// EIP-712 signature the digest suggests.
	sig, err := s.SignDigest(polymarket.PersonalDigest(digest[:]))
	if err != nil {
		return SubmitRequest{}, fmt.Errorf("relayer: signing Safe transaction: %w", err)
	}
	packed, err := safeSignature(sig)
	if err != nil {
		return SubmitRequest{}, err
	}

	return SubmitRequest{
		Type:        TransactionTypeSafe,
		From:        p.Owner,
		To:          call.To,
		ProxyWallet: safe,
		Data:        call.Data,
		Nonce:       p.Nonce,
		Signature:   packed,
		SignatureParams: &SignatureParams{
			GasPrice:       "0",
			Operation:      fmt.Sprint(call.Operation.safeCode()),
			SafeTxnGas:     "0",
			BaseGas:        "0",
			GasToken:       polymarket.ZeroAddress,
			RefundReceiver: polymarket.ZeroAddress,
		},
		Metadata: p.Metadata,
	}, nil
}

// safeSignature converts an ordinary signature into the form a Safe accepts.
//
// A Safe reads v to decide how the signature was made: 27 or 28 is a raw
// EIP-712 signature over the struct digest, and 31 or 32 says the signer
// prefixed it as a personal message first. This client does the latter, so v
// gains four. Leaving it at 27 makes the Safe recover from the wrong hash.
func safeSignature(sig []byte) (string, error) {
	if len(sig) != 65 {
		return "", fmt.Errorf("relayer: signature is %d bytes, want 65", len(sig))
	}
	v := sig[64]
	switch v {
	case 0, 1:
		v += 31
	case 27, 28:
		v += 4
	default:
		return "", fmt.Errorf("relayer: signature v is %d, want 27 or 28", v)
	}
	packed := make([]byte, 65)
	copy(packed, sig[:64])
	packed[64] = v
	return "0x" + hex.EncodeToString(packed), nil
}

// aggregate reduces a list of calls to the single one a Safe performs. One
// call goes straight to its target; several are packed for the multisend
// contract, which the Safe delegate-calls so that the batch runs as the Safe
// rather than as the multisend.
func aggregate(calls []Call, multisend string) (Call, error) {
	switch len(calls) {
	case 0:
		return Call{}, fmt.Errorf("relayer: a transaction needs at least one call")
	case 1:
		return calls[0], nil
	}
	if multisend == "" {
		return Call{}, fmt.Errorf("relayer: batching needs a multisend contract, which this chain has none of")
	}
	packed, err := packMultisend(calls)
	if err != nil {
		return Call{}, err
	}
	return Call{
		To:        multisend,
		Value:     "0",
		Data:      "0x" + hex.EncodeToString(abi.EncodeBytesCall("multiSend(bytes)", packed)),
		Operation: OpDelegateCall,
	}, nil
}

// packMultisend lays the calls out as the multisend contract walks them:
// operation, target, value, the length of the calldata, then the calldata,
// with no padding anywhere. The lengths are what let the contract find the
// next entry, so a wrong one silently reinterprets everything after it.
func packMultisend(calls []Call) ([]byte, error) {
	var out []byte
	for i, call := range calls {
		to, value, data, err := call.decode()
		if err != nil {
			return nil, fmt.Errorf("call %d: %w", i, err)
		}
		address, err := abi.PackedAddress(to)
		if err != nil {
			return nil, err
		}
		amount, err := abi.PackedUint(value, 32)
		if err != nil {
			return nil, fmt.Errorf("call %d value: %w", i, err)
		}
		length, err := abi.PackedUint(big.NewInt(int64(len(data))), 32)
		if err != nil {
			return nil, err
		}
		out = append(out, call.Operation.safeCode())
		out = append(out, address...)
		out = append(out, amount...)
		out = append(out, length...)
		out = append(out, data...)
	}
	return out, nil
}

// ProxyParams describes a transaction for a legacy proxy wallet.
type ProxyParams struct {
	// Owner is the address of the key that signs.
	Owner string

	// Nonce is the proxy's transaction counter, from Client.Nonce with
	// WalletTypeProxy.
	Nonce string

	// Calls are what the proxy should do. Unlike a Safe, a proxy performs a
	// list of calls itself, so batching needs no second contract.
	Calls []Call

	// Relay is the relayer's own address, from Client.RelayPayload. It is
	// covered by the signature, so a payload fetched for one relayer cannot
	// be sent through another.
	Relay string

	// GasPrice is the minimum the signer will pay for, in wei. It is signed.
	GasPrice string

	// GasLimit bounds the relayed call. Zero uses DefaultProxyGasLimit; this
	// client has no node to estimate against.
	GasLimit string

	// ChainID selects the contracts. Zero means Polygon.
	ChainID int64

	// Metadata is passed through by the relayer.
	Metadata string
}

// DefaultProxyGasLimit is what a proxy transaction claims when the caller
// names no limit. The official client estimates against a node and falls back
// to this; without a node it is the only sensible choice.
const DefaultProxyGasLimit = "10000000"

// ProxyRelayHash returns the hash a proxy transaction's signature covers.
//
// It is not EIP-712. The relay hub concatenates the fields with a "rlx:" tag
// and hashes the result, so there is no domain, no type string, and nothing
// for a wallet to render — which is exactly why the deposit wallet that
// replaced this uses typed data.
func ProxyRelayHash(p ProxyParams, c polymarket.Contracts) ([32]byte, error) {
	if c.ProxyFactory == "" || c.RelayHub == "" {
		return [32]byte{}, fmt.Errorf("relayer: this chain has no proxy factory")
	}
	if p.Nonce == "" {
		return [32]byte{}, fmt.Errorf("relayer: a proxy transaction needs a nonce")
	}
	data, err := encodeProxyCalls(p.Calls)
	if err != nil {
		return [32]byte{}, err
	}
	from, err := abi.PackedAddress(p.Owner)
	if err != nil {
		return [32]byte{}, fmt.Errorf("relayer: proxy owner: %w", err)
	}
	// The transaction is sent to the factory, which forwards it to the
	// wallet the owner's address derives.
	to, err := abi.PackedAddress(c.ProxyFactory)
	if err != nil {
		return [32]byte{}, err
	}
	hub, err := abi.PackedAddress(c.RelayHub)
	if err != nil {
		return [32]byte{}, err
	}
	relay, err := abi.PackedAddress(p.Relay)
	if err != nil {
		return [32]byte{}, fmt.Errorf("relayer: relay address: %w", err)
	}

	// The relayer fee is zero: Polymarket pays for these.
	fee, err := packedDecimal("0", 32)
	if err != nil {
		return [32]byte{}, err
	}
	gasPrice, err := packedDecimal(orDefault(p.GasPrice, "0"), 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("relayer: gas price: %w", err)
	}
	gasLimit, err := packedDecimal(orDefault(p.GasLimit, DefaultProxyGasLimit), 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("relayer: gas limit: %w", err)
	}
	nonce, err := packedDecimal(p.Nonce, 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("relayer: nonce: %w", err)
	}

	return eip712.Keccak256([]byte("rlx:"), from, to, data,
		fee, gasPrice, gasLimit, nonce, hub, relay), nil
}

// BuildProxyTransaction signs a transaction for a legacy proxy wallet and
// returns the request that submits it.
func BuildProxyTransaction(s polymarket.Signer, p ProxyParams, c polymarket.Contracts) (SubmitRequest, error) {
	if s == nil {
		return SubmitRequest{}, polymarket.ErrNoSigner
	}
	proxy, err := polymarket.DeriveProxyWallet(p.Owner, c.ProxyFactory)
	if err != nil {
		return SubmitRequest{}, err
	}
	data, err := encodeProxyCalls(p.Calls)
	if err != nil {
		return SubmitRequest{}, err
	}
	hash, err := ProxyRelayHash(p, c)
	if err != nil {
		return SubmitRequest{}, err
	}

	// The relay hub recovers this with an eth_sign prefix, as the Safe does,
	// but takes the signature unchanged rather than raising v.
	sig, err := s.SignDigest(polymarket.PersonalDigest(hash[:]))
	if err != nil {
		return SubmitRequest{}, fmt.Errorf("relayer: signing proxy transaction: %w", err)
	}

	return SubmitRequest{
		Type:        TransactionTypeProxy,
		From:        p.Owner,
		To:          c.ProxyFactory,
		ProxyWallet: proxy,
		Data:        "0x" + hex.EncodeToString(data),
		Nonce:       p.Nonce,
		Signature:   "0x" + hex.EncodeToString(sig),
		SignatureParams: &SignatureParams{
			GasPrice:   orDefault(p.GasPrice, "0"),
			GasLimit:   orDefault(p.GasLimit, DefaultProxyGasLimit),
			RelayerFee: "0",
			RelayHub:   c.RelayHub,
			Relay:      p.Relay,
		},
		Metadata: p.Metadata,
	}, nil
}

// encodeProxyCalls builds calldata for the factory's
// proxy((uint8,address,uint256,bytes)[]).
//
// The tuple holds a bytes member, which makes it dynamic, which makes the
// array an array of offsets rather than of values: the head is one offset per
// call, counted from the start of the head, and each call's own calldata is
// reached by a second offset inside it.
func encodeProxyCalls(calls []Call) ([]byte, error) {
	if len(calls) == 0 {
		return nil, fmt.Errorf("relayer: a transaction needs at least one call")
	}

	elements := make([][]byte, len(calls))
	for i, call := range calls {
		to, value, data, err := call.decode()
		if err != nil {
			return nil, fmt.Errorf("call %d: %w", i, err)
		}
		address, err := eip712.Address(to)
		if err != nil {
			return nil, err
		}
		amount, err := eip712.Uint(value)
		if err != nil {
			return nil, fmt.Errorf("call %d value: %w", i, err)
		}
		// Four words of head, so the bytes member starts at 128.
		element := abi.Encode(
			eip712.Uint8(call.Operation.proxyCode()),
			address,
			amount,
			abi.Uint64(128),
		)
		length := abi.Uint64(uint64(len(data)))
		element = append(element, length[:]...)
		element = append(element, data...)
		if pad := len(data) % 32; pad != 0 {
			element = append(element, make([]byte, 32-pad)...)
		}
		elements[i] = element
	}

	// The array's own head is one offset per element, measured from just
	// after the length word.
	head := make([]byte, 0, 32*len(elements))
	offset := uint64(32 * len(elements))
	body := make([]byte, 0)
	for _, element := range elements {
		word := abi.Uint64(offset)
		head = append(head, word[:]...)
		body = append(body, element...)
		offset += uint64(len(element))
	}

	arrayOffset, length := abi.Uint64(32), abi.Uint64(uint64(len(calls)))
	out := abi.Selector("proxy((uint8,address,uint256,bytes)[])")
	out = append(out, arrayOffset[:]...)
	out = append(out, length[:]...)
	out = append(out, head...)
	out = append(out, body...)
	return out, nil
}

// BatchParams describes a batch for a deposit wallet to carry out.
type BatchParams struct {
	// Owner is the address of the key that signs. The wallet is derived from
	// it when Wallet is empty.
	Owner string

	// Wallet overrides the derived wallet address. Set it for an account
	// created before the June 2026 upgrade, whose wallet is at the
	// pre-upgrade address; see polymarket.DeriveDepositWalletUUPS.
	Wallet string

	// Nonce is the wallet's batch counter, from Client.Nonce.
	Nonce string

	// Deadline is a unix-seconds expiry, after which the batch cannot be
	// executed. Unlike an order's expiration this one is signed.
	Deadline string

	// Calls are what the wallet should do, in order. A batch may legitimately
	// be empty, which does nothing but consume the nonce.
	Calls []Call

	// ChainID selects the contracts and binds the signature. Zero means
	// Polygon.
	ChainID int64
}

// batchFields is the deposit wallet's Batch struct. calls is an array of
// structs, which EIP-712 hashes as the hash of its elements' hashes.
var batchFields = []polymarket.TypedDataField{
	{Name: "wallet", Type: "address"},
	{Name: "nonce", Type: "uint256"},
	{Name: "deadline", Type: "uint256"},
	{Name: "calls", Type: "Call[]"},
}

// batchCallFields is one call inside a Batch.
var batchCallFields = []polymarket.TypedDataField{
	{Name: "target", Type: "address"},
	{Name: "value", Type: "uint256"},
	{Name: "data", Type: "bytes"},
}

// BatchTypedData returns the EIP-712 payload a deposit wallet's batch signs.
//
// Unlike the other two wallets this is ordinary typed data, so an external
// signer sees the calls it is authorising: the targets, the values and the
// calldata, each as a field. Hand it to a TypedDataSigner and the wallet
// doing the signing can render or refuse it.
func BatchTypedData(p BatchParams, c polymarket.Contracts) (polymarket.TypedData, error) {
	wallet, err := batchWallet(p, c)
	if err != nil {
		return polymarket.TypedData{}, err
	}
	if p.Nonce == "" || p.Deadline == "" {
		return polymarket.TypedData{}, fmt.Errorf("relayer: a batch needs a nonce and a deadline")
	}

	calls := make([]any, len(p.Calls))
	for i, call := range p.Calls {
		to, value, data, err := call.decode()
		if err != nil {
			return polymarket.TypedData{}, fmt.Errorf("call %d: %w", i, err)
		}
		calls[i] = map[string]any{
			"target": to,
			"value":  value.String(),
			"data":   hexOrEmpty(data),
		}
	}

	domain := polymarket.TypedDataDomain{
		Name:              DepositWalletDomainName,
		Version:           DepositWalletDomainVersion,
		ChainID:           chainOrDefault(p.ChainID),
		VerifyingContract: wallet,
	}
	return polymarket.TypedData{
		Types: map[string][]polymarket.TypedDataField{
			"EIP712Domain": domain.DomainType(),
			"Batch":        append([]polymarket.TypedDataField(nil), batchFields...),
			"Call":         append([]polymarket.TypedDataField(nil), batchCallFields...),
		},
		PrimaryType: "Batch",
		Domain:      domain,
		Message: map[string]any{
			"wallet":   wallet,
			"nonce":    p.Nonce,
			"deadline": p.Deadline,
			"calls":    calls,
		},
	}, nil
}

// The deposit wallet's EIP-712 domain. Unlike an order's, it names the
// wallet itself as the verifying contract, so a batch signed for one wallet
// cannot be replayed into another.
const (
	DepositWalletDomainName    = "DepositWallet"
	DepositWalletDomainVersion = "1"
)

// BuildWalletBatch signs a batch for a deposit wallet and returns the request
// that submits it.
func BuildWalletBatch(s polymarket.Signer, p BatchParams, c polymarket.Contracts) (SubmitRequest, error) {
	if s == nil {
		return SubmitRequest{}, polymarket.ErrNoSigner
	}
	wallet, err := batchWallet(p, c)
	if err != nil {
		return SubmitRequest{}, err
	}
	td, err := BatchTypedData(p, c)
	if err != nil {
		return SubmitRequest{}, err
	}
	sig, err := polymarket.SignTypedData(s, td)
	if err != nil {
		return SubmitRequest{}, fmt.Errorf("relayer: signing batch for wallet %s: %w", wallet, err)
	}

	calls := make([]BatchCall, len(p.Calls))
	for i, call := range p.Calls {
		to, value, data, err := call.decode()
		if err != nil {
			return SubmitRequest{}, fmt.Errorf("call %d: %w", i, err)
		}
		calls[i] = BatchCall{Target: to, Value: value.String(), Data: hexOrEmpty(data)}
	}

	return SubmitRequest{
		Type:      TransactionTypeWallet,
		From:      p.Owner,
		To:        c.DepositWalletFactory,
		Nonce:     p.Nonce,
		Signature: "0x" + hex.EncodeToString(sig),
		Deposit: &DepositWalletParams{
			DepositWallet: wallet,
			Deadline:      p.Deadline,
			Calls:         calls,
		},
	}, nil
}

// BuildWalletCreate returns the request that deploys a deposit wallet. It
// carries no signature: the address is fixed by the owner, so there is
// nothing to authorise.
func BuildWalletCreate(owner string, c polymarket.Contracts) (SubmitRequest, error) {
	if _, err := eip712.Address(owner); err != nil {
		return SubmitRequest{}, fmt.Errorf("relayer: wallet owner: %w", err)
	}
	if c.DepositWalletFactory == "" {
		return SubmitRequest{}, fmt.Errorf("relayer: this chain has no deposit wallet factory")
	}
	return SubmitRequest{
		Type: TransactionTypeWalletCreate,
		From: owner,
		To:   c.DepositWalletFactory,
	}, nil
}

// batchWallet resolves which wallet a batch is for.
func batchWallet(p BatchParams, c polymarket.Contracts) (string, error) {
	if p.Wallet != "" {
		if _, err := eip712.Address(p.Wallet); err != nil {
			return "", fmt.Errorf("relayer: batch wallet: %w", err)
		}
		return p.Wallet, nil
	}
	return polymarket.DeriveDepositWallet(p.Owner, c.DepositWalletFactory, c.DepositWalletBeacon)
}

// Submit sends a signed transaction to the relayer.
//
// This spends money: the wallet does whatever the calls say, and the
// signature is already made, so there is nothing left to check. The relayer
// answers with an id rather than a result — poll Client.Transaction until the
// state is terminal.
func (c *Client) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if req.Type == "" {
		return SubmitResponse{}, fmt.Errorf("relayer: submit needs a transaction type")
	}
	body, err := marshalBody(req)
	if err != nil {
		return SubmitResponse{}, err
	}
	authed, err := c.authContext(ctx, http.MethodPost, epSubmit, body)
	if err != nil {
		return SubmitResponse{}, err
	}
	var out SubmitResponse
	if err := c.session.Do(authed, polymarket.Request{
		Method: http.MethodPost,
		Path:   epSubmit,
		Body:   req,
		Out:    &out,
	}); err != nil {
		return SubmitResponse{}, err
	}
	return out, nil
}

// marshalBody renders a request body the way the session will, so that a
// credential signature computed over it covers the bytes actually sent.
func marshalBody(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("relayer: encoding request body: %w", err)
	}
	return string(b), nil
}

// chainOrDefault reads a chain id, treating zero as Polygon.
func chainOrDefault(chainID int64) int64 {
	if chainID == 0 {
		return polymarket.ChainPolygon
	}
	return chainID
}

// hexOrEmpty renders calldata, which is "0x" rather than "" when absent.
func hexOrEmpty(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}

// packedDecimal encodes a decimal string into a fixed number of bytes.
func packedDecimal(s string, size int) ([]byte, error) {
	n, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal integer", s)
	}
	return abi.PackedUint(n, size)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

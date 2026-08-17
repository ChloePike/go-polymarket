// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package onchain

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Block tags a call can be made against. A pending nonce counts the
// transactions a node has accepted but not yet mined, which is the one to
// build the next transaction on; a latest nonce ignores them and produces a
// second transaction with a number the first already used.
const (
	BlockLatest    = "latest"
	BlockPending   = "pending"
	BlockFinalized = "finalized"
)

// NodeChainID asks the node which chain it serves.
func (c *Client) NodeChainID(ctx context.Context) (int64, error) {
	var out string
	if err := c.call(ctx, "eth_chainId", nil, &out); err != nil {
		return 0, err
	}
	v, err := parseQuantity(out)
	if err != nil {
		return 0, fmt.Errorf("onchain: eth_chainId: %w", err)
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("onchain: eth_chainId: chain id %s out of range", v)
	}
	return v.Int64(), nil
}

// CheckChainID confirms the node serves the chain this client is configured
// for. Call it once at start-up: a transaction signed for one chain and sent
// to another is rejected, and an approval sent to the wrong chain approves a
// contract that is not the one the caller meant.
func (c *Client) CheckChainID(ctx context.Context) error {
	got, err := c.NodeChainID(ctx)
	if err != nil {
		return err
	}
	if got != c.ChainID() {
		return fmt.Errorf("onchain: node serves chain %d, client is configured for %d", got, c.ChainID())
	}
	return nil
}

// Nonce returns the number the address's next transaction must carry, counting
// transactions the node has accepted but not yet mined.
func (c *Client) Nonce(ctx context.Context, address string) (uint64, error) {
	return c.NonceAt(ctx, address, BlockPending)
}

// NonceAt returns the transaction count of an address at a block.
func (c *Client) NonceAt(ctx context.Context, address, block string) (uint64, error) {
	if _, err := normalizeAddress(address); err != nil {
		return 0, err
	}
	var out string
	if err := c.call(ctx, "eth_getTransactionCount", []any{address, block}, &out); err != nil {
		return 0, err
	}
	return parseUint64(out)
}

// Balance returns an address's native balance in wei. On Polygon that is POL,
// the gas token — not the USDC a Polymarket account trades with, which is an
// ERC-20 and is read with TokenBalance.
func (c *Client) Balance(ctx context.Context, address string) (*big.Int, error) {
	if _, err := normalizeAddress(address); err != nil {
		return nil, err
	}
	var out string
	if err := c.call(ctx, "eth_getBalance", []any{address, BlockLatest}, &out); err != nil {
		return nil, err
	}
	return parseQuantity(out)
}

// A CallMsg describes a call to make without sending it: the argument to
// eth_call and eth_estimateGas. Zero fields are omitted, which lets a node
// fill in its own defaults.
type CallMsg struct {
	// From is the sender the call runs as. A read usually leaves it empty; a
	// gas estimate should set it, because a contract that checks msg.sender
	// estimates differently for a different caller.
	From string
	// To is the contract being called. Empty means contract creation.
	To string
	// Gas caps execution. Zero lets the node choose.
	Gas uint64
	// Value is the native amount sent with the call, in wei.
	Value *big.Int
	// Data is the calldata.
	Data []byte
}

// callParams is the JSON object eth_call and eth_estimateGas take. Every field
// is a hex quantity or hex data, and an empty one is omitted rather than sent
// as a zero, because a node reads an explicit zero gas as a gas cap of zero.
type callParams struct {
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Gas   string `json:"gas,omitempty"`
	Value string `json:"value,omitempty"`
	Data  string `json:"data,omitempty"`
}

// params converts a CallMsg to its wire form, checking the addresses.
func (m CallMsg) params() (callParams, error) {
	var p callParams
	if m.From != "" {
		from, err := normalizeAddress(m.From)
		if err != nil {
			return p, err
		}
		p.From = from
	}
	if m.To != "" {
		to, err := normalizeAddress(m.To)
		if err != nil {
			return p, err
		}
		p.To = to
	}
	if m.Gas != 0 {
		p.Gas = hexUint64(m.Gas)
	}
	if m.Value != nil && m.Value.Sign() != 0 {
		if m.Value.Sign() < 0 {
			return p, fmt.Errorf("onchain: negative value %s", m.Value)
		}
		p.Value = hexQuantity(m.Value)
	}
	if len(m.Data) > 0 {
		p.Data = hexData(m.Data)
	}
	return p, nil
}

// Call runs a call against the latest block and returns what it returned. It
// changes nothing and costs nothing.
func (c *Client) Call(ctx context.Context, m CallMsg) ([]byte, error) {
	p, err := m.params()
	if err != nil {
		return nil, err
	}
	var out string
	if err := c.call(ctx, "eth_call", []any{p, BlockLatest}, &out); err != nil {
		return nil, err
	}
	return parseData(out)
}

// EstimateGas asks the node what the call would cost to execute.
//
// The estimate is a simulation against the current state, so it is a
// prediction and not a promise: a transaction that lands after another one
// changes the same storage can need more. Send adds a margin for that reason.
func (c *Client) EstimateGas(ctx context.Context, m CallMsg) (uint64, error) {
	p, err := m.params()
	if err != nil {
		return 0, err
	}
	var out string
	if err := c.call(ctx, "eth_estimateGas", []any{p}, &out); err != nil {
		return 0, err
	}
	return parseUint64(out)
}

// Fees are the two prices an EIP-1559 transaction names, and the base fee they
// were derived from.
type Fees struct {
	// BaseFee is the latest block's base fee per gas, which the protocol
	// sets and the sender does not choose. It is burnt.
	BaseFee *big.Int
	// MaxPriorityFeePerGas is the tip to the block producer.
	MaxPriorityFeePerGas *big.Int
	// MaxFeePerGas caps what the sender pays per gas in total. Anything
	// above base fee plus tip is refunded, so the headroom below costs
	// nothing unless the base fee actually rises into it.
	MaxFeePerGas *big.Int
}

// blockHeader is the slice of a block this client reads.
type blockHeader struct {
	Number        string `json:"number"`
	BaseFeePerGas string `json:"baseFeePerGas"`
}

// SuggestFees prices a transaction from the current block. It issues two
// requests: one for the latest block's base fee, one for the node's suggested
// tip.
//
// MaxFeePerGas is twice the base fee plus the tip. The doubling is headroom:
// the base fee can rise by at most 12.5% per block, so twice the current one
// survives roughly six consecutive full blocks, and the excess is refunded
// rather than spent.
func (c *Client) SuggestFees(ctx context.Context) (Fees, error) {
	var f Fees

	var head blockHeader
	if err := c.call(ctx, "eth_getBlockByNumber", []any{BlockLatest, false}, &head); err != nil {
		return f, err
	}
	if head.BaseFeePerGas == "" {
		return f, fmt.Errorf("onchain: node reported no base fee, so the chain is pre-London and this client cannot price a transaction for it")
	}
	base, err := parseQuantity(head.BaseFeePerGas)
	if err != nil {
		return f, fmt.Errorf("onchain: base fee: %w", err)
	}

	var tipHex string
	if err := c.call(ctx, "eth_maxPriorityFeePerGas", nil, &tipHex); err != nil {
		return f, err
	}
	tip, err := parseQuantity(tipHex)
	if err != nil {
		return f, fmt.Errorf("onchain: priority fee: %w", err)
	}

	f.BaseFee = base
	f.MaxPriorityFeePerGas = tip
	f.MaxFeePerGas = new(big.Int).Add(new(big.Int).Lsh(base, 1), tip)
	return f, nil
}

// SendRaw broadcasts an already-signed transaction and returns its hash.
//
// This spends money. It is the one call in this package that does, and it is
// not retried: a node that answered nothing may still have accepted the
// transaction, and sending it again risks paying twice for one intent.
func (c *Client) SendRaw(ctx context.Context, raw []byte) (string, error) {
	var hash string
	if err := c.call(ctx, "eth_sendRawTransaction", []any{hexData(raw)}, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

// A Receipt is what a mined transaction left behind.
type Receipt struct {
	TransactionHash string
	BlockHash       string
	BlockNumber     uint64
	From            string
	To              string
	// ContractAddress is set only when the transaction deployed a contract.
	ContractAddress string
	GasUsed         uint64
	// EffectiveGasPrice is what the sender actually paid per gas, at or
	// below MaxFeePerGas.
	EffectiveGasPrice *big.Int
	// Status is 1 for success and 0 for a reverted transaction. A reverted
	// transaction still consumed its gas.
	Status uint64
	Logs   []Log
}

// Succeeded reports whether the transaction did what it was sent to do. A
// receipt exists either way, so this is the check that matters.
func (r Receipt) Succeeded() bool { return r.Status == 1 }

// A Log is one event a transaction emitted.
type Log struct {
	Address string
	Topics  []string
	Data    string
	// Removed is true when a reorganisation took the log back.
	Removed bool
}

// receiptWire is the receipt as the node writes it: every number a hex string.
type receiptWire struct {
	TransactionHash   string    `json:"transactionHash"`
	BlockHash         string    `json:"blockHash"`
	BlockNumber       string    `json:"blockNumber"`
	From              string    `json:"from"`
	To                string    `json:"to"`
	ContractAddress   string    `json:"contractAddress"`
	GasUsed           string    `json:"gasUsed"`
	EffectiveGasPrice string    `json:"effectiveGasPrice"`
	Status            string    `json:"status"`
	Logs              []logWire `json:"logs"`
}

// logWire is one log as the node writes it.
type logWire struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	Removed bool     `json:"removed"`
}

// receipt converts the wire form, tolerating the fields a node omits.
func (w receiptWire) receipt() (Receipt, error) {
	r := Receipt{
		TransactionHash: w.TransactionHash,
		BlockHash:       w.BlockHash,
		From:            w.From,
		To:              w.To,
		ContractAddress: w.ContractAddress,
	}
	var err error
	if w.BlockNumber != "" {
		if r.BlockNumber, err = parseUint64(w.BlockNumber); err != nil {
			return r, fmt.Errorf("onchain: receipt block number: %w", err)
		}
	}
	if w.GasUsed != "" {
		if r.GasUsed, err = parseUint64(w.GasUsed); err != nil {
			return r, fmt.Errorf("onchain: receipt gas used: %w", err)
		}
	}
	if w.EffectiveGasPrice != "" {
		if r.EffectiveGasPrice, err = parseQuantity(w.EffectiveGasPrice); err != nil {
			return r, fmt.Errorf("onchain: receipt gas price: %w", err)
		}
	}
	if w.Status != "" {
		if r.Status, err = parseUint64(w.Status); err != nil {
			return r, fmt.Errorf("onchain: receipt status: %w", err)
		}
	}
	for _, l := range w.Logs {
		r.Logs = append(r.Logs, Log{Address: l.Address, Topics: l.Topics, Data: l.Data, Removed: l.Removed})
	}
	return r, nil
}

// Receipt returns the receipt for a transaction hash, reporting false when the
// node has none: the transaction is unknown, or known and not yet mined.
func (c *Client) Receipt(ctx context.Context, hash string) (Receipt, bool, error) {
	var w receiptWire
	ok, err := c.present(ctx, "eth_getTransactionReceipt", []any{hash}, &w)
	if err != nil || !ok {
		return Receipt{}, false, err
	}
	r, err := w.receipt()
	return r, err == nil, err
}

// WaitReceipt polls until the transaction is mined, the context ends, or the
// node reports an error.
//
// It returns the receipt whatever the transaction did, so check Succeeded: a
// reverted transaction is mined, is paid for, and did nothing. A poll interval
// of zero means two seconds, which is a little over one Polygon block.
func (c *Client) WaitReceipt(ctx context.Context, hash string, poll time.Duration) (Receipt, error) {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		r, ok, err := c.Receipt(ctx, hash)
		if err != nil {
			return Receipt{}, err
		}
		if ok {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return Receipt{}, fmt.Errorf("onchain: waiting for %s: %w", hash, ctx.Err())
		case <-time.After(poll):
		}
	}
}

// normalizeAddress checks an address and returns it in lowercase hex with a
// 0x prefix, the form a node expects. It does not checksum: EIP-55 is for
// display, and a node compares bytes.
func normalizeAddress(address string) (string, error) {
	body := strings.TrimPrefix(strings.TrimPrefix(address, "0x"), "0X")
	if len(body) != 40 {
		return "", fmt.Errorf("onchain: address %q is not 20 bytes", address)
	}
	b, err := hex.DecodeString(strings.ToLower(body))
	if err != nil {
		return "", fmt.Errorf("onchain: address %q is not hex: %w", address, err)
	}
	return "0x" + hex.EncodeToString(b), nil
}

// hexQuantity encodes an integer the way JSON-RPC writes a number: hex, no
// leading zeros, and "0x0" for zero.
func hexQuantity(x *big.Int) string {
	if x == nil || x.Sign() == 0 {
		return "0x0"
	}
	return "0x" + strings.TrimLeft(hex.EncodeToString(x.Bytes()), "0")
}

// hexUint64 encodes a small integer as a quantity.
func hexUint64(v uint64) string { return hexQuantity(new(big.Int).SetUint64(v)) }

// hexData encodes a byte string the way JSON-RPC writes bytes: hex with a
// leading zero kept, and "0x" when empty.
func hexData(b []byte) string { return "0x" + hex.EncodeToString(b) }

// parseQuantity decodes a hex number. An odd number of digits is legal for a
// quantity — "0x1" is one — so this cannot go through hex.DecodeString.
func parseQuantity(s string) (*big.Int, error) {
	body := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if body == "" {
		return nil, fmt.Errorf("onchain: %q is not a hex quantity", s)
	}
	v, ok := new(big.Int).SetString(body, 16)
	if !ok {
		return nil, fmt.Errorf("onchain: %q is not a hex quantity", s)
	}
	return v, nil
}

// parseUint64 decodes a hex number that must fit in 64 bits.
func parseUint64(s string) (uint64, error) {
	v, err := parseQuantity(s)
	if err != nil {
		return 0, err
	}
	if !v.IsUint64() {
		return 0, fmt.Errorf("onchain: %s does not fit in 64 bits", v)
	}
	return v.Uint64(), nil
}

// parseData decodes hex bytes.
func parseData(s string) ([]byte, error) {
	body := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if body == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("onchain: %q is not hex data: %w", s, err)
	}
	return b, nil
}

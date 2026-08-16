// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// TestContractWalletOrderSignatures is the golden test for the whole ERC-7739
// path: a wrapped payload, an inner signature over it, and the layout the
// exchange unpacks.
//
// Nothing else can catch a mistake here. The wrapper is not a digest anybody
// can recompute from the order, the exchange verifies it inside a contract, and
// a wrong one is refused only after the order has been sent. These signatures
// come from the official SDK.
func TestContractWalletOrderSignatures(t *testing.T) {
	g := loadGolden(t)
	if len(g.WalletOrders) == 0 {
		t.Fatal("no contract-wallet vectors")
	}
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range g.WalletOrders {
		t.Run(want.Name, func(t *testing.T) {
			salt, err := strconv.ParseInt(want.Order.Salt, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			ts, err := strconv.ParseInt(want.Order.Timestamp, 10, 64)
			if err != nil {
				t.Fatal(err)
			}

			wallet := Wallet{
				SignatureType: SigEIP1271,
				Owner:         key.Address(),
				Address:       want.Input.Wallet,
			}
			opts := wallet.OrderOptions()
			opts.TickSize = want.Input.TickSize
			opts.NegRisk = want.Input.NegRisk
			opts.Version = want.Input.Version
			opts.Salt = salt
			opts.Timestamp = ts

			order, err := BuildOrder(UserOrder{
				TokenID: want.Input.TokenID,
				Price:   want.Input.Price,
				Size:    want.Input.Size,
				Side:    Side(want.Input.Side),
			}, key.Address(), opts)
			if err != nil {
				t.Fatal(err)
			}

			// The wallet is both maker and signer. The key that produced the
			// bytes appears nowhere in the order.
			if order.Maker != want.Order.Maker {
				t.Errorf("maker = %s, want %s", order.Maker, want.Order.Maker)
			}
			if order.Signer != want.Order.Signer {
				t.Errorf("signer = %s, want %s", order.Signer, want.Order.Signer)
			}
			if order.Signer == key.Address() {
				t.Error("the order named the signing key as its signer, not the wallet")
			}

			signed, err := SignOrder(order, g.ChainID, opts, key)
			if err != nil {
				t.Fatal(err)
			}
			if signed.Signature != want.Signature {
				t.Fatalf("signature =\n\t%s\nwant\n\t%s", signed.Signature, want.Signature)
			}
		})
	}
}

// TestWrappedSignatureLayout takes the wrapper apart and checks each piece
// against what it is supposed to be. The golden test above proves the bytes
// match; this one says which byte is which, so a future failure points at a
// part rather than at a 317-byte string.
func TestWrappedSignatureLayout(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	v := g.WalletOrders[0]

	wallet := Wallet{SignatureType: SigEIP1271, Owner: key.Address(), Address: v.Input.Wallet}
	opts := wallet.OrderOptions()
	opts.NegRisk = v.Input.NegRisk
	opts.Version = v.Input.Version
	salt, _ := strconv.ParseInt(v.Order.Salt, 10, 64)
	ts, _ := strconv.ParseInt(v.Order.Timestamp, 10, 64)
	opts.Salt, opts.Timestamp, opts.TickSize = salt, ts, v.Input.TickSize

	order, err := BuildOrder(UserOrder{
		TokenID: v.Input.TokenID, Price: v.Input.Price, Size: v.Input.Size, Side: Side(v.Input.Side),
	}, key.Address(), opts)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignOrder(order, g.ChainID, opts, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(signed.Signature, "0x"))
	if err != nil {
		t.Fatal(err)
	}

	typeStringLen := len(orderTypeString)
	if got, want := len(raw), 65+32+32+typeStringLen+2; got != want {
		t.Fatalf("wrapper is %d bytes, want %d", got, want)
	}

	// The trailing two bytes are the type string's length, big-endian, and
	// the type string itself sits directly before them. A wallet reads this
	// end first, so a wrong length makes every field after it garbage.
	declared := int(raw[len(raw)-2])<<8 | int(raw[len(raw)-1])
	if declared != typeStringLen {
		t.Errorf("declared type string length %d, want %d", declared, typeStringLen)
	}
	if got := string(raw[len(raw)-2-typeStringLen : len(raw)-2]); got != orderTypeString {
		t.Errorf("type string = %q, want %q", got, orderTypeString)
	}

	// Bytes 65..97 are the exchange's domain separator, not the wallet's.
	// That is the part of ERC-7739 that reads like a mistake and is not.
	domain, err := orderDomain(g.ChainID, opts.version(), opts.NegRisk)
	if err != nil {
		t.Fatal(err)
	}
	separator, err := domain.Separator()
	if err != nil {
		t.Fatal(err)
	}
	if got := raw[65:97]; string(got) != string(separator[:]) {
		t.Errorf("domain separator = %x, want %x", got, separator)
	}

	// Bytes 97..129 are the order's own struct hash, which is what makes the
	// order recoverable from the wrapper.
	td, err := OrderTypedData(order, g.ChainID, opts)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := td.StructHash()
	if err != nil {
		t.Fatal(err)
	}
	if got := raw[97:129]; string(got) != string(contents[:]) {
		t.Errorf("contents hash = %x, want %x", got, contents)
	}

	// The inner signature recovers to the key, never to the wallet: the
	// wallet has no key, which is the whole reason for the wrapper.
	inner, err := WrappedOrderTypedData(order, g.ChainID, opts)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := inner.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(digest, raw[:65], key.Address()); err != nil {
		t.Errorf("the inner signature does not recover to the owner: %v", err)
	}
	if err := VerifySignature(digest, raw[:65], v.Input.Wallet); err == nil {
		t.Error("the inner signature recovered to the wallet, which holds no key")
	}
}

// TestWrappedTypedDataShape checks what an external signer is shown. A
// hardware wallet, an HSM or a policy engine reads this payload rather than
// the order, so the order has to be legible inside it.
func TestWrappedTypedDataShape(t *testing.T) {
	g := loadGolden(t)
	v := g.WalletOrders[0]
	order := Order{
		Salt: v.Order.Salt, Maker: v.Order.Maker, Signer: v.Order.Signer,
		Taker: ZeroAddress, TokenID: v.Order.TokenID,
		MakerAmount: v.Order.MakerAmount, TakerAmount: v.Order.TakerAmount,
		Side: Side(v.Order.Side), SignatureType: SigEIP1271,
		Timestamp: v.Order.Timestamp, Expiration: "0",
		Metadata: v.Order.Metadata, Builder: v.Order.Builder,
	}
	opts := OrderOptions{Version: v.Input.Version, NegRisk: v.Input.NegRisk, SignatureType: SigEIP1271}

	td, err := WrappedOrderTypedData(order, g.ChainID, opts)
	if err != nil {
		t.Fatal(err)
	}

	// EIP-712 sorts referenced types after the primary one, so a payload with
	// a nested struct has two definitions and a fixed order between them.
	typeString, err := td.TypeString()
	if err != nil {
		t.Fatal(err)
	}
	const wantType = "TypedDataSign(Order contents,string name,string version," +
		"uint256 chainId,address verifyingContract,bytes32 salt)" + orderTypeString
	if typeString != wantType {
		t.Errorf("type string =\n\t%s\nwant\n\t%s", typeString, wantType)
	}

	// The domain is the exchange's; the wallet's own domain is in the message.
	if td.Domain.Name != exchangeDomainName {
		t.Errorf("domain name = %q, want the exchange's %q", td.Domain.Name, exchangeDomainName)
	}
	if got := td.Message["name"]; got != depositWalletDomainName {
		t.Errorf("message name = %v, want %q", got, depositWalletDomainName)
	}
	if got := td.Message["verifyingContract"]; got != order.Signer {
		t.Errorf("message verifyingContract = %v, want the wallet %s", got, order.Signer)
	}

	// The order survives whole inside contents, which is what makes the
	// payload auditable at all.
	contents, ok := td.Message["contents"].(map[string]any)
	if !ok {
		t.Fatalf("contents is %T, want a message", td.Message["contents"])
	}
	if got := contents["makerAmount"]; got != order.MakerAmount {
		t.Errorf("contents makerAmount = %v, want %s", got, order.MakerAmount)
	}
	if len(contents) != len(orderStructFields) {
		t.Errorf("contents has %d fields, want the order's %d", len(contents), len(orderStructFields))
	}
}

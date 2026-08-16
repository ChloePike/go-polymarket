// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package sign

import (
	"errors"

	"github.com/ChloePike/go-polymarket/types"
)

// ErrNotImplemented marks the M1 signing work still to be done.
var ErrNotImplemented = errors.New("go-polymarket/sign: not implemented (M1)")

// Signer abstracts an EOA private key. The concrete implementation (M1) wraps a
// secp256k1 key and signs a 32-byte EIP-712 digest, returning a 65-byte
// r‖s‖v signature with v ∈ {27,28}.
type Signer interface {
	Address() string // 0x-checksummed EOA address
	SignDigest(digest [32]byte) ([]byte, error)
}

// OrderDomain identifies which exchange contract verifies the order.
type OrderDomain struct {
	VerifyingContract string // ExchangeV2Polygon or NegRiskExchangeV2Polygon
	ChainID           int64
}

// DomainForMarket picks the verifying contract from a market's neg-risk flag.
func DomainForMarket(negRisk bool, chainID int64) OrderDomain {
	c := types.ExchangeV2Polygon
	if negRisk {
		c = types.NegRiskExchangeV2Polygon
	}
	return OrderDomain{VerifyingContract: c, ChainID: chainID}
}

// OrderHash computes the EIP-712 digest for a V2 order:
//
//	digest = keccak256(0x1901 ‖ domainSeparator ‖ hashStruct(Order))
//
// domainSeparator hashes {name:"Polymarket CTF Exchange", version:"2",
// chainId, verifyingContract}. hashStruct hashes ORDER_TYPE_HASH followed by
// the 11 signed fields (see types.OrderTypeString), each left-padded to 32
// bytes; bytes32 fields verbatim, address left-padded, uintN big-endian.
//
// TODO(M1): implement with go-ethereum crypto/keccak + abi packing. Pin it
// with the golden test in sign/eip712_golden_test.go.
func OrderHash(o types.Order, d OrderDomain) ([32]byte, error) {
	return [32]byte{}, ErrNotImplemented
}

// SignOrder produces the SignedOrder for the EOA path (signatureType 0).
//
// TODO(M1): digest := OrderHash(o, d); sig := signer.SignDigest(digest);
// return SignedOrder{Order: o, Signature: hexutil.Encode(sig)}.
func SignOrder(o types.Order, d OrderDomain, signer Signer) (types.SignedOrder, error) {
	return types.SignedOrder{}, ErrNotImplemented
}

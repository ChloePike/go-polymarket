// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ChloePike/go-polymarket/internal/eip712"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// A Signer produces the secp256k1 signatures the exchange verifies. Orders and
// the level-1 authentication payload are both EIP-712 typed data, so a Signer
// only ever sees a finished 32-byte digest.
//
// Implement it to keep key material outside this process — in a hardware
// wallet, a remote signing service, or an enclave. PrivateKey is the in-memory
// implementation.
type Signer interface {
	// Address returns the signing address in EIP-55 checksummed form.
	Address() string

	// SignDigest returns a 65-byte signature laid out as r ‖ s ‖ v, where v is
	// 27 or 28. The signature must be canonical: s in the lower half of the
	// curve order, as EIP-2 requires.
	SignDigest(digest [32]byte) ([]byte, error)
}

// PrivateKey is a Signer holding a secp256k1 key in memory.
type PrivateKey struct {
	key     *secp256k1.PrivateKey
	address string
}

// NewPrivateKey parses a 32-byte secp256k1 key, with or without a 0x prefix.
//
// The key stays in this process's memory for as long as the returned value
// lives. Load it from the environment or a secrets manager; a key written into
// source is a key that has been published.
func NewPrivateKey(hexKey string) (*PrivateKey, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(hexKey, "0x"), "0X"))
	if err != nil {
		return nil, fmt.Errorf("polymarket: private key is not hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("polymarket: private key is %d bytes, want 32", len(b))
	}
	key := secp256k1.PrivKeyFromBytes(b)
	if key.Key.IsZero() {
		return nil, errors.New("polymarket: private key is zero")
	}
	return &PrivateKey{key: key, address: addressFromPublicKey(key.PubKey())}, nil
}

// Address returns the EIP-55 checksummed address of the key.
func (k *PrivateKey) Address() string { return k.address }

// SignDigest signs a 32-byte digest, returning r ‖ s ‖ v.
func (k *PrivateKey) SignDigest(digest [32]byte) ([]byte, error) {
	// SignCompact returns v ‖ r ‖ s with v already offset by 27; Ethereum
	// orders the same bytes as r ‖ s ‖ v. Getting this backwards yields a
	// signature that verifies to a different address, so it is pinned by a
	// golden test rather than left to inspection.
	compact := ecdsa.SignCompact(k.key, digest[:], false)
	if len(compact) != 65 {
		return nil, fmt.Errorf("polymarket: compact signature is %d bytes, want 65", len(compact))
	}
	sig := make([]byte, 65)
	copy(sig, compact[1:])
	sig[64] = compact[0]
	return sig, nil
}

// addressFromPublicKey derives an Ethereum address: the low 20 bytes of the
// Keccak-256 hash of the uncompressed public key with its 0x04 prefix removed.
func addressFromPublicKey(pub *secp256k1.PublicKey) string {
	uncompressed := pub.SerializeUncompressed()
	h := eip712.Keccak256(uncompressed[1:])
	return ChecksumAddress("0x" + hex.EncodeToString(h[12:]))
}

// ChecksumAddress applies the EIP-55 mixed-case checksum to a hex address.
// Input case is ignored. A malformed address is returned unchanged, because
// callers use this for display and the exchange itself does not require the
// checksum form.
func ChecksumAddress(address string) string {
	body := strings.TrimPrefix(strings.TrimPrefix(address, "0x"), "0X")
	if len(body) != 40 {
		return address
	}
	lower := []byte(strings.ToLower(body))
	if _, err := hex.DecodeString(string(lower)); err != nil {
		return address
	}
	hash := eip712.Keccak256(lower)

	out := make([]byte, 0, 42)
	out = append(out, '0', 'x')
	for i, c := range lower {
		// Each address character is one nibble of the hash; a nibble of 8 or
		// more uppercases its character.
		nibble := hash[i/2] >> 4
		if i%2 == 1 {
			nibble = hash[i/2] & 0x0f
		}
		if nibble >= 8 && c >= 'a' && c <= 'f' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

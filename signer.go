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

// A Signer produces the secp256k1 signatures the exchange verifies.
//
// Implement it to keep key material outside this process — in a hardware
// wallet, a remote signing service, or an enclave. PrivateKey is the in-memory
// implementation.
//
// This interface sees only the finished digest, which is enough to sign but
// not enough to show anyone what is being signed. A signer that needs the
// fields — to render them, log them, or apply a policy to them — should also
// implement TypedDataSigner, which this package prefers when it is available.
type Signer interface {
	// Address returns the signing address in EIP-55 checksummed form.
	Address() string

	// SignDigest returns a 65-byte signature laid out as r ‖ s ‖ v, where v is
	// 27 or 28. The signature must be canonical: s in the lower half of the
	// curve order, as EIP-2 requires.
	SignDigest(digest [32]byte) ([]byte, error)
}

// A TypedDataSigner is a Signer that wants the whole EIP-712 payload rather
// than its hash. Implement it when the signature is produced somewhere that
// needs to see what it is authorising: a hardware wallet rendering the order
// for a human, an audit log recording the fields, or a policy engine refusing
// an order for its size.
//
// It is optional. This package prefers it when a Signer implements it and
// falls back to SignDigest otherwise, so an existing Signer keeps working.
//
// An implementation MUST derive the digest from the payload it was given,
// with TypedData.Digest, and must not accept a digest from elsewhere: showing
// one thing and signing another is the failure this interface exists to
// prevent. This package verifies the returned signature recovers to
// Address(), which catches that mistake locally.
type TypedDataSigner interface {
	Signer

	// SignTypedData signs an EIP-712 payload, returning r ‖ s ‖ v as
	// SignDigest does.
	SignTypedData(td TypedData) ([]byte, error)
}

// SignTypedData signs an EIP-712 payload with a Signer.
//
// It prefers a TypedDataSigner, so an external signer is shown the fields
// rather than a hash, and checks that whatever comes back recovers to the
// signing address.
//
// Use it for a payload this package does not build for you — a relayer
// transaction, a permit, anything the API grows next.
func SignTypedData(s Signer, td TypedData) ([]byte, error) {
	if s == nil {
		return nil, ErrNoSigner
	}
	return signTypedData(s, td)
}

// PersonalDigest returns the 32 bytes an eth_sign or personal_sign covers:
//
//	keccak256("\x19Ethereum Signed Message:\n" ‖ len(message) ‖ message)
//
// This is not EIP-712 and is not interchangeable with it. Two places in the
// Polymarket protocol need it: a Gnosis Safe transaction, whose signature the
// Safe re-prefixes before recovering it, and a proxy-wallet relay, whose hash
// is not typed data at all. An order never uses it.
func PersonalDigest(message []byte) [32]byte {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	return eip712.Keccak256([]byte(prefix), message)
}

// signTypedData produces a signature over a payload, preferring a signer that
// can see the payload and falling back to the digest.
func signTypedData(s Signer, td TypedData) ([]byte, error) {
	digest, err := td.Digest()
	if err != nil {
		return nil, err
	}

	var sig []byte
	if rich, ok := s.(TypedDataSigner); ok {
		if sig, err = rich.SignTypedData(td); err != nil {
			return nil, err
		}
		// An external signer is the one place a signature can cover
		// something other than what was shown. Recovering it costs one
		// operation and turns a silent mis-sign into an immediate error
		// rather than a rejection from the exchange.
		if err := VerifySignature(digest, sig, s.Address()); err != nil {
			return nil, fmt.Errorf("polymarket: the signer returned a signature for different data: %w", err)
		}
		return sig, nil
	}

	return s.SignDigest(digest)
}

// VerifySignature reports whether a 65-byte r ‖ s ‖ v signature over digest
// recovers to address. It is what makes an externally produced signature
// checkable before it is sent.
func VerifySignature(digest [32]byte, sig []byte, address string) error {
	if len(sig) != 65 {
		return fmt.Errorf("polymarket: signature is %d bytes, want 65", len(sig))
	}
	// RecoverCompact wants v ‖ r ‖ s, the inverse of the wire order.
	compact := make([]byte, 65)
	compact[0] = sig[64]
	copy(compact[1:], sig[:64])

	pub, _, err := ecdsa.RecoverCompact(compact, digest[:])
	if err != nil {
		return fmt.Errorf("polymarket: signature does not recover: %w", err)
	}
	got := addressFromPublicKey(pub)
	if !strings.EqualFold(got, address) {
		return fmt.Errorf("polymarket: signature recovers to %s, want %s", got, address)
	}
	return nil
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

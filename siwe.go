// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Sign-in-with-Ethereum constants. Polymarket fixes all four, and the login
// rebuilds the message from the fields it is handed: a value that differs from
// these produces text the server never reconstructs, so the signature recovers
// to nobody and the login is refused.
const (
	// SIWEDomain is the domain the message names. It is the bare host, with
	// no scheme, as EIP-4361 requires.
	SIWEDomain = "polymarket.com"
	// SIWEURI is the uri field, which unlike the domain carries its scheme.
	SIWEURI = "https://polymarket.com"
	// SIWEStatement is the human-readable line the wallet shows the user.
	SIWEStatement = "Welcome to Polymarket! Sign to connect."
	// SIWEVersion is the EIP-4361 version. The standard defines only "1".
	SIWEVersion = "1"
)

// siweTokenSeparator divides the message fields from the signature inside the
// bearer token.
//
// Trap: it sits INSIDE the base64, not between two base64 blobs. The token is
// one encoding of the plaintext "json:::signature"; joining an encoded body to
// a signature with the same three bytes is the reading that looks equally
// right and authenticates as nobody. The server answers 401 "invalid login"
// either way, which is also what a bad signature gets, so the status does not
// say which half is wrong.
const siweTokenSeparator = ":::"

// siweTimeFormat is how the timestamps are rendered. Both the signed text and
// the JSON carry them, and they must agree exactly.
const siweTimeFormat = time.RFC3339

// A SIWEMessage is the sign-in-with-Ethereum message of EIP-4361, in the shape
// Polymarket accepts. NewSIWEMessage fills in the four constant fields.
//
// The message authenticates the ACCOUNT rather than one request, which is what
// separates it from the level-1 and level-2 schemes: it is exchanged once for
// a session cookie, and that cookie is what mints a relayer API key.
type SIWEMessage struct {
	// Domain is the domain requesting the signature, SIWEDomain.
	Domain string
	// Address is the signing account, EIP-55 checksummed.
	Address string
	// Statement is the line shown to the user, SIWEStatement.
	Statement string
	// URI is the subject of the sign-in, SIWEURI.
	URI string
	// Version is the EIP-4361 version, SIWEVersion.
	Version string
	// ChainID is the chain the account is claimed on, 137 for Polygon.
	ChainID int64
	// Nonce is the value GET /nonce returned. It is bound to the cookie that
	// same response set: presenting one without the other is refused.
	Nonce string
	// IssuedAt is when the message was created.
	IssuedAt time.Time
	// ExpirationTime is when it stops being accepted.
	ExpirationTime time.Time
}

// NewSIWEMessage returns a message for an address and a nonce, with
// Polymarket's constants filled in and the timestamps set from now.
//
// The nonce comes from the Gamma nonce endpoint, and the cookie that endpoint
// set has to travel with the login too.
func NewSIWEMessage(address, nonce string, chainID int64, lifetime time.Duration) SIWEMessage {
	now := time.Now().UTC()
	return SIWEMessage{
		Domain:         SIWEDomain,
		Address:        address,
		Statement:      SIWEStatement,
		URI:            SIWEURI,
		Version:        SIWEVersion,
		ChainID:        chainID,
		Nonce:          nonce,
		IssuedAt:       now,
		ExpirationTime: now.Add(lifetime),
	}
}

// String renders the EIP-4361 text, which is what gets signed.
func (m SIWEMessage) String() string {
	return fmt.Sprintf(
		"%s wants you to sign in with your Ethereum account:\n%s\n\n%s\n\nURI: %s\nVersion: %s\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpiration Time: %s",
		m.Domain, m.Address, m.Statement, m.URI, m.Version, m.ChainID,
		m.Nonce, m.IssuedAt.UTC().Format(siweTimeFormat),
		m.ExpirationTime.UTC().Format(siweTimeFormat))
}

// Digest returns the 32 bytes the signature covers: the message taken as a
// personal message, the same EIP-191 step the relayer's meta-transactions use.
func (m SIWEMessage) Digest() [32]byte {
	return PersonalDigest([]byte(m.String()))
}

// siweFields is the JSON half of the bearer token. The server rebuilds the
// signed text from these, so every field the text carries appears here.
type siweFields struct {
	Address        string `json:"address"`
	ChainID        int64  `json:"chainId"`
	Domain         string `json:"domain"`
	ExpirationTime string `json:"expirationTime"`
	IssuedAt       string `json:"issuedAt"`
	Nonce          string `json:"nonce"`
	Statement      string `json:"statement"`
	URI            string `json:"uri"`
	Version        string `json:"version"`
}

// fields returns the message as the token's JSON half.
func (m SIWEMessage) fields() siweFields {
	return siweFields{
		Address:        m.Address,
		ChainID:        m.ChainID,
		Domain:         m.Domain,
		ExpirationTime: m.ExpirationTime.UTC().Format(siweTimeFormat),
		IssuedAt:       m.IssuedAt.UTC().Format(siweTimeFormat),
		Nonce:          m.Nonce,
		Statement:      m.Statement,
		URI:            m.URI,
		Version:        m.Version,
	}
}

// Token returns the value of the Authorization header the login takes, without
// its "Bearer " prefix, for a signature over Digest.
//
// The layout is base64(json ‖ ":::" ‖ "0x" ‖ hex(signature)). See
// siweTokenSeparator for why the separator's position is the trap here.
func (m SIWEMessage) Token(signature []byte) (string, error) {
	if len(signature) != 65 {
		return "", fmt.Errorf("polymarket: siwe signature is %d bytes, want 65", len(signature))
	}
	body, err := json.Marshal(m.fields())
	if err != nil {
		return "", fmt.Errorf("polymarket: encoding siwe fields: %w", err)
	}
	plain := string(body) + siweTokenSeparator + "0x" + hex.EncodeToString(signature)
	return base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

// A PersonalSigner is a Signer that wants the message text rather than its
// digest.
//
// Implement it when the key lives behind a service that will not sign a bare
// 32 bytes — which is the safe posture, not a limitation to work around. An
// unframed signature is valid for whatever else that digest might have been,
// so a custodial signer that refuses one is refusing to let a login double as
// a transaction. Signing the text lets it apply the EIP-191 framing itself and
// see what it is authorising.
//
// The signature must cover PersonalDigest(message) and come back as the same
// 65 bytes SignDigest returns.
type PersonalSigner interface {
	Signer

	// SignPersonal signs message as an EIP-191 personal message.
	SignPersonal(message []byte) ([]byte, error)
}

// SignSIWE signs a message with a signer and returns its bearer token.
//
// A PersonalSigner is given the text; anything else is given the digest. The
// two produce the same signature — PersonalDigest is exactly the step being
// moved across the boundary — so which path runs is a property of where the
// key lives and never of what gets signed.
func SignSIWE(s Signer, m SIWEMessage) (string, error) {
	if s == nil {
		return "", fmt.Errorf("polymarket: siwe login needs a signer")
	}
	var (
		sig []byte
		err error
	)
	if ps, ok := s.(PersonalSigner); ok {
		sig, err = ps.SignPersonal([]byte(m.String()))
	} else {
		sig, err = s.SignDigest(m.Digest())
	}
	if err != nil {
		return "", fmt.Errorf("polymarket: signing siwe message: %w", err)
	}
	return m.Token(sig)
}

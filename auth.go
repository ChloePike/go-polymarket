// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The CLOB has two levels of authentication.
//
// Level 1 proves control of the wallet with an EIP-712 signature, and is used
// only to create or derive an API key. Level 2 authenticates each ordinary
// request with an HMAC over the request line, using the credentials level 1
// handed out. Trading uses level 2; the private key is not involved once the
// credentials exist.

// APICreds are the level-2 credentials the auth endpoints return. Treat all
// three as secrets: together they can trade on the account.
type APICreds struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// APIKey returns the credential's key, which identifies it and is not secret.
func (c APICreds) APIKey() string { return c.Key }

// AuthHeaders signs one request, timestamped now. It is what makes APICreds an
// L2Authenticator, and it is the implementation a session uses when the
// credentials are held in this process.
func (c APICreds) AuthHeaders(address, method, requestPath, body string) (L2Headers, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return BuildL2Headers(c, address, ts, method, requestPath, body)
}

// An L2Authenticator renders the level-2 headers for one request.
//
// Implement it when the API secret must not enter this process: a KMS, an
// HSM, or a signing service that holds the secret and returns signatures.
// APICreds is the implementation for a secret held in memory, and is what
// WithCredentials installs.
//
// It is the level-2 counterpart of Signer, which already lets the wallet key
// live elsewhere, and the sibling of relayer.Authenticator, which does the
// same for the relayer's credentials. This one returns typed headers rather
// than a map because the CLOB's set is fixed.
//
// Two limits are worth knowing before building one:
//
//   - The address still comes from the session's Signer, because POLY_ADDRESS
//     names the wallet rather than the credential. A session with an
//     authenticator and no Signer cannot make a level-2 request. Signer is an
//     interface too, so a remote one satisfies this without a key here.
//   - It protects the CLOB's REST requests only. The websocket user channel
//     authenticates by putting the secret itself in the subscribe frame, so
//     there is nothing there for a signing service to hold back.
//
// An implementation must sign the request it was given: the timestamp it
// returns in the headers must be the one it signed, or the exchange answers
// 401.
type L2Authenticator interface {
	// APIKey returns the credential's key. It travels in the clear and
	// identifies which key signed; an order is attributed to it.
	APIKey() string

	// AuthHeaders returns the headers authenticating one request.
	// requestPath is the bare path, without a query string, and body is the
	// encoded request body or "" when there is none.
	AuthHeaders(address, method, requestPath, body string) (L2Headers, error)
}

// The environment variables CredentialsFromEnv reads.
const (
	EnvAPIKey        = "POLYMARKET_API_KEY"
	EnvAPISecret     = "POLYMARKET_API_SECRET"
	EnvAPIPassphrase = "POLYMARKET_API_PASSPHRASE"
)

// CredentialsFromEnv loads level-2 credentials from the environment, which is
// where they belong: a secret in a source file is a secret that has been
// published. It reports an error naming what is missing rather than returning
// a partial set, because a partial set authenticates as nobody.
func CredentialsFromEnv() (APICreds, error) {
	creds := APICreds{
		Key:        os.Getenv(EnvAPIKey),
		Secret:     os.Getenv(EnvAPISecret),
		Passphrase: os.Getenv(EnvAPIPassphrase),
	}
	var missing []string
	if creds.Key == "" {
		missing = append(missing, EnvAPIKey)
	}
	if creds.Secret == "" {
		missing = append(missing, EnvAPISecret)
	}
	if creds.Passphrase == "" {
		missing = append(missing, EnvAPIPassphrase)
	}
	if len(missing) > 0 {
		return APICreds{}, fmt.Errorf("polymarket: no credentials in the environment: %s unset",
			strings.Join(missing, ", "))
	}
	return creds, nil
}

// ClobAuthDigest returns the level-1 EIP-712 digest for an address at a
// timestamp and nonce.
//
// Two of the four fields are strings, which EIP-712 hashes rather than pads:
// timestamp is a string even though it holds a number, while nonce is a real
// uint256. Encoding timestamp as a number produces a well-formed signature
// that authenticates as nobody.
func ClobAuthDigest(address string, chainID int64, timestamp string, nonce int64) ([32]byte, error) {
	digest, err := ClobAuthTypedData(address, chainID, timestamp, nonce).Digest()
	if err != nil {
		return [32]byte{}, fmt.Errorf("polymarket: clob auth: %w", err)
	}
	return digest, nil
}

// L1Headers are the headers that authenticate a key-management request.
type L1Headers struct {
	Address   string
	Signature string
	Timestamp string // unix seconds
	Nonce     string
}

// Header renders the headers under their canonical POLY_ names.
func (h L1Headers) Header() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":   h.Address,
		"POLY_SIGNATURE": h.Signature,
		"POLY_TIMESTAMP": h.Timestamp,
		"POLY_NONCE":     h.Nonce,
	}
}

// BuildL1Headers signs the level-1 payload for a wallet.
func BuildL1Headers(s Signer, chainID int64, timestamp string, nonce int64) (L1Headers, error) {
	if s == nil {
		return L1Headers{}, fmt.Errorf("polymarket: level-1 authentication needs a Signer")
	}
	sig, err := signTypedData(s, ClobAuthTypedData(s.Address(), chainID, timestamp, nonce))
	if err != nil {
		return L1Headers{}, fmt.Errorf("polymarket: signing level-1 payload: %w", err)
	}
	return L1Headers{
		Address:   s.Address(),
		Signature: "0x" + hex.EncodeToString(sig),
		Timestamp: timestamp,
		Nonce:     strconv.FormatInt(nonce, 10),
	}, nil
}

// L2Headers authenticate one ordinary request.
type L2Headers struct {
	Address    string
	Signature  string
	APIKey     string
	Passphrase string
	Timestamp  string // unix seconds
}

// Header renders the headers under their canonical POLY_ names.
func (h L2Headers) Header() map[string]string {
	return map[string]string{
		"POLY_ADDRESS":    h.Address,
		"POLY_SIGNATURE":  h.Signature,
		"POLY_API_KEY":    h.APIKey,
		"POLY_PASSPHRASE": h.Passphrase,
		"POLY_TIMESTAMP":  h.Timestamp,
	}
}

// BuildL2Headers signs one request with level-2 credentials.
//
// requestPath is the path ALONE, without a query string. The exchange
// excludes the query from the signature, which is what lets one set of
// headers authenticate every page of a paginated walk. Including it produces
// a 401 as soon as any parameter is present.
func BuildL2Headers(creds APICreds, address, timestamp, method, requestPath, body string) (L2Headers, error) {
	sig, err := SignHMAC(creds.Secret, timestamp, method, requestPath, body)
	if err != nil {
		return L2Headers{}, err
	}
	return L2Headers{
		Address:    address,
		Signature:  sig,
		APIKey:     creds.Key,
		Passphrase: creds.Passphrase,
		Timestamp:  timestamp,
	}, nil
}

// SignHMAC produces the level-2 request signature:
//
//	message   = timestamp ‖ method ‖ requestPath ‖ body
//
// requestPath is the bare path; the exchange does not sign the query string.
//
//	key       = base64url-decode(secret)
//	signature = base64url(HMAC-SHA256(key, message))
//
// The secret arrives base64url-encoded and is decoded before use; signing with
// the encoded text produces a signature the server rejects.
func SignHMAC(secret, timestamp, method, requestPath, body string) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", fmt.Errorf("polymarket: api secret: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeSecret decodes a base64url secret, tolerating missing padding and the
// standard alphabet, which different parts of the API have both been seen to
// emit.
func decodeSecret(s string) ([]byte, error) {
	if b, err := base64.URLEncoding.DecodeString(padBase64(s)); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(padBase64(s))
}

func padBase64(s string) string {
	if m := len(s) % 4; m != 0 {
		return s + "===="[m:]
	}
	return s
}

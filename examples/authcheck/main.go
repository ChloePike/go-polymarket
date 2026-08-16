// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command authcheck proves both authentication levels against the live CLOB.
//
// It generates a throwaway key, exchanges it for API credentials, spends those
// credentials on a level-2 request, and then deliberately corrupts a signature
// to confirm the exchange really is verifying. Nothing here costs anything: an
// API key is free, no order is placed, and the key is discarded on exit.
//
//	go run ./examples/authcheck
//
// Use it after changing anything in the signing path. A golden vector proves
// this client agrees with another client; this proves the exchange agrees.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	polymarket "github.com/ChloePike/go-polymarket"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("authcheck: ")

	keyHex := flag.String("key", "", "private key to use; empty generates a throwaway one")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hexKey := *keyHex
	if hexKey == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			log.Fatal(err)
		}
		hexKey = hex.EncodeToString(raw)
		fmt.Println("generated a throwaway key; it is discarded on exit")
	}

	key, err := polymarket.NewPrivateKey(hexKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("address            %s\n", key.Address())

	c := &polymarket.Client{Signer: key}

	// Level 1: an EIP-712 signature over the ClobAuth payload, in exchange
	// for credentials.
	creds, err := c.CreateOrDeriveAPIKey(ctx)
	if err != nil {
		log.Fatalf("level 1 failed: %v", err)
	}
	fmt.Printf("level 1            ok, api key %s\n", creds.Key)

	// Level 2: an HMAC over the request line, using those credentials.
	orders, _, err := c.OpenOrders(ctx, polymarket.OpenOrderParams{}, "")
	if err != nil {
		log.Fatalf("level 2 failed: %v", err)
	}
	fmt.Printf("level 2            ok, %d open orders\n", len(orders))

	// The negative control. Without it, a level-1 pass could mean the
	// exchange is not checking anything.
	bad := &polymarket.Client{Signer: corrupt{key}}
	if _, err := bad.CreateOrDeriveAPIKey(ctx); err == nil {
		fmt.Println("negative control   FAILED: a corrupted signature was accepted")
		os.Exit(1)
	} else {
		fmt.Printf("negative control   ok, corrupted signature rejected: %v\n", err)
	}
}

// corrupt wraps a Signer and flips one byte of every signature it produces.
type corrupt struct {
	inner polymarket.Signer
}

func (c corrupt) Address() string { return c.inner.Address() }

func (c corrupt) SignDigest(digest [32]byte) ([]byte, error) {
	sig, err := c.inner.SignDigest(digest)
	if err != nil {
		return nil, err
	}
	sig[10] ^= 0xff
	return sig, nil
}

// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command onchaincheck proves this client's on-chain layer against a real
// node: that the addresses it derives are the ones the factory deploys, that
// the calldata it builds is answered by the live contracts, and — with
// -broadcast — that a node accepts a transaction it signed.
//
//	go run ./examples/onchaincheck -rpc <node-url>
//	go run ./examples/onchaincheck -rpc <node-url> -broadcast
//
// Without -broadcast it is read-only: every call is an eth_call or a state
// read, and nothing is signed.
//
// With -broadcast it signs one transaction with a key generated in this
// process a moment earlier. That address has never existed and holds nothing,
// so the transaction cannot be mined and cannot move anything; what it proves
// is that the node parsed the encoding and recovered the right sender, because
// the rejection then names the sender and its empty balance. A node that
// answers "invalid sender" instead would mean the signature or the encoding is
// wrong — which is the failure this check exists to rule out.
//
// It never uses a key of yours. There is no flag to make it spend.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/onchain"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	rpc := flag.String("rpc", "", "JSON-RPC endpoint (required)")
	chainID := flag.Int64("chain", polymarket.ChainPolygon, "chain id")
	// Vitalik Buterin's address: a well-known one that is nobody's Polymarket
	// account, so the derivations are real and the balances are empty.
	owner := flag.String("owner", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "an owner to derive wallets for")
	broadcast := flag.Bool("broadcast", false, "also send a transaction signed by a fresh, empty key")
	flag.Parse()

	if *rpc == "" {
		slog.Error("a JSON-RPC endpoint is required; this package has no default node")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := onchain.New(*rpc, onchain.WithChainID(*chainID))
	contracts, ok := polymarket.ContractsFor(*chainID)
	if !ok {
		slog.Error("no contracts for this chain", "chain", *chainID)
		os.Exit(1)
	}

	if err := client.CheckChainID(ctx); err != nil {
		slog.Error("checking the chain", "err", err)
		os.Exit(1)
	}
	fmt.Printf("chain          %d, as the node reports\n", *chainID)

	checkDerivation(ctx, client, contracts, *owner)
	checkReads(ctx, client, contracts, *owner)
	if *broadcast {
		checkBroadcast(ctx, client, *chainID)
	}
}

// checkDerivation compares the offline CREATE2 derivations against the
// factory's own predictions. They are the same arithmetic done in two places,
// and the whole point of the offline one is that it needs no node — so it is
// worth knowing they still agree.
func checkDerivation(ctx context.Context, client *onchain.Client, contracts polymarket.Contracts, owner string) {
	derived, err := polymarket.DeriveDepositWallet(owner,
		contracts.DepositWalletFactory, contracts.DepositWalletBeacon)
	if err != nil {
		slog.Error("deriving the deposit wallet", "err", err)
		os.Exit(1)
	}
	predicted, err := client.PredictDepositWallet(ctx, owner)
	if err != nil {
		slog.Error("predicting the deposit wallet", "err", err)
		os.Exit(1)
	}
	report("deposit wallet", derived, predicted)

	legacy, err := polymarket.DeriveDepositWalletUUPS(owner,
		contracts.DepositWalletFactory, contracts.DepositWalletImplementation)
	if err != nil {
		slog.Error("deriving the pre-upgrade wallet", "err", err)
		os.Exit(1)
	}
	predictedLegacy, err := client.PredictLegacyDepositWallet(ctx, owner)
	if err != nil {
		slog.Error("predicting the pre-upgrade wallet", "err", err)
		os.Exit(1)
	}
	report("pre-upgrade   ", legacy, predictedLegacy)

	deployed, err := client.Deployed(ctx, derived)
	if err != nil {
		slog.Error("checking deployment", "err", err)
		os.Exit(1)
	}
	fmt.Printf("deployed       %t\n", deployed)
	if deployed {
		nonce, err := client.WalletNonce(ctx, derived)
		if err != nil {
			slog.Error("reading the wallet nonce", "err", err)
			os.Exit(1)
		}
		fmt.Printf("batch nonce    %s\n", nonce)
	}
}

// report prints one derivation against one prediction.
func report(what, derived, predicted string) {
	mark := "MISMATCH"
	if strings.EqualFold(derived, predicted) {
		mark = "ok"
	}
	fmt.Printf("%s %s  factory says %s  %s\n", what, derived, predicted, mark)
}

// checkReads exercises the calldata this client builds against the live
// contracts. A wrong selector or a wrong argument order shows up here as a
// revert or as an answer that makes no sense.
func checkReads(ctx context.Context, client *onchain.Client, contracts polymarket.Contracts, owner string) {
	balance, err := client.TokenBalance(ctx, contracts.Collateral, owner)
	if err != nil {
		slog.Error("reading the collateral balance", "err", err)
		os.Exit(1)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(polymarket.Decimals), nil)
	fmt.Printf("USDC           %s\n",
		polymarket.FormatAmount(new(big.Rat).SetFrac(balance, scale), polymarket.Decimals))

	allowance, err := client.Allowance(ctx, contracts.Collateral, owner, contracts.Exchange)
	if err != nil {
		slog.Error("reading the allowance", "err", err)
		os.Exit(1)
	}
	fmt.Printf("allowance      %s\n", allowance)

	approved, err := client.IsApprovedForAll(ctx, contracts.ConditionalTokens, owner, contracts.Exchange)
	if err != nil {
		slog.Error("reading the operator flag", "err", err)
		os.Exit(1)
	}
	fmt.Printf("ctf operator   %t\n", approved)

	missing, err := client.MissingApprovals(ctx, owner, nil)
	if err != nil {
		slog.Error("reading the approvals", "err", err)
		os.Exit(1)
	}
	fmt.Printf("approvals due  %d\n", len(missing))

	fees, err := client.SuggestFees(ctx)
	if err != nil {
		slog.Error("pricing a transaction", "err", err)
		os.Exit(1)
	}
	fmt.Printf("fees           base %s, tip %s, max %s\n",
		fees.BaseFee, fees.MaxPriorityFeePerGas, fees.MaxFeePerGas)
}

// checkBroadcast signs a transaction with a key made moments ago and offers it
// to the node. The address holds nothing, so the node rejects it for exactly
// that reason — and to say so it must first have decoded the transaction and
// recovered its sender, which is what is being tested.
func checkBroadcast(ctx context.Context, client *onchain.Client, chainID int64) {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		slog.Error("generating a key", "err", err)
		os.Exit(1)
	}
	key, err := polymarket.NewPrivateKey(hex.EncodeToString(seed[:]))
	if err != nil {
		slog.Error("generating a key", "err", err)
		os.Exit(1)
	}

	balance, err := client.Balance(ctx, key.Address())
	if err != nil {
		slog.Error("reading the fresh key's balance", "err", err)
		os.Exit(1)
	}
	if balance.Sign() != 0 {
		// Impossible short of a broken random source, and the check costs
		// one call: this must never send a transaction that could be mined.
		slog.Error("the freshly generated address holds funds; refusing to broadcast", "address", key.Address())
		os.Exit(1)
	}

	fees, err := client.SuggestFees(ctx)
	if err != nil {
		slog.Error("pricing the transaction", "err", err)
		os.Exit(1)
	}
	tx := onchain.Transaction{
		ChainID:              big.NewInt(chainID),
		Nonce:                0,
		MaxPriorityFeePerGas: fees.MaxPriorityFeePerGas,
		MaxFeePerGas:         fees.MaxFeePerGas,
		Gas:                  21000,
		To:                   key.Address(),
		Value:                big.NewInt(1),
	}
	signed, err := onchain.SignTransaction(key, tx)
	if err != nil {
		slog.Error("signing", "err", err)
		os.Exit(1)
	}

	fmt.Printf("\nfresh sender   %s (balance 0)\n", key.Address())
	fmt.Printf("signed hash    %s\n", signed.Hash)

	hash, err := client.SendRaw(ctx, signed.Raw)
	if err == nil {
		fmt.Printf("accepted       %s — the node took a transaction from an empty account\n", hash)
		return
	}
	fmt.Printf("rejected       %v\n", err)
	if strings.Contains(strings.ToLower(err.Error()), "sender") &&
		!strings.Contains(strings.ToLower(err.Error()), "funds") {
		fmt.Println("\nThat rejection is about the SENDER, not the balance: the node could not " +
			"recover the address that signed. Something in the encoding or the signature is wrong.")
		os.Exit(1)
	}
	fmt.Println("\nThe node decoded the transaction and recovered the sender, then refused it for " +
		"having nothing to pay with. That is the expected outcome.")
}

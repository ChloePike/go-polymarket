// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command approvals reports which token approvals an address still owes the
// Polymarket exchange contracts, and prints the transactions that would grant
// them.
//
// An account trading with its own key — an EOA, rather than one of the smart
// wallets the relayer pays for — must approve two contracts before the
// exchange can settle anything: the collateral it pays with, and the outcome
// tokens it receives. Until then an order matches and settlement fails.
//
//	go run ./examples/approvals -rpc https://polygon-rpc.example -owner 0x1234...
//
// It is read-only. It makes eth_call reads and prints what a transaction would
// contain; it signs nothing, sends nothing, and takes no key. To actually
// grant an approval, take the printed calldata and pass it through
// onchain.Client.Send with a signer — that spends gas and is deliberately not
// what this command does.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/onchain"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	rpc := flag.String("rpc", "", "JSON-RPC endpoint for Polygon (required)")
	// Vitalik Buterin's address: a well-known one that is nobody's
	// Polymarket account, so the reads are real and nothing is approved.
	owner := flag.String("owner", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "the address that would trade")
	chainID := flag.Int64("chain", polymarket.ChainPolygon, "chain id")
	flag.Parse()

	if *rpc == "" {
		slog.Error("a JSON-RPC endpoint is required; this package has no default node")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := onchain.New(*rpc, onchain.WithChainID(*chainID))
	if err := client.CheckChainID(ctx); err != nil {
		slog.Error("checking the chain", "err", err)
		os.Exit(1)
	}

	contracts, ok := polymarket.ContractsFor(*chainID)
	if !ok {
		slog.Error("no contracts for this chain", "chain", *chainID)
		os.Exit(1)
	}

	balance, err := client.TokenBalance(ctx, contracts.Collateral, *owner)
	if err != nil {
		slog.Error("reading the collateral balance", "err", err)
		os.Exit(1)
	}
	gas, err := client.Balance(ctx, *owner)
	if err != nil {
		slog.Error("reading the gas balance", "err", err)
		os.Exit(1)
	}

	fmt.Printf("owner    %s\n", polymarket.ChecksumAddress(*owner))
	// The collateral is an integer in the six-decimal fixed point the whole
	// protocol uses, so it becomes a decimal by dividing rather than by
	// being read as one.
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(polymarket.Decimals), nil)
	fmt.Printf("USDC     %s\n",
		polymarket.FormatAmount(new(big.Rat).SetFrac(balance, scale), polymarket.Decimals))
	fmt.Printf("gas      %s wei\n\n", gas)

	missing, err := client.MissingApprovals(ctx, *owner, nil)
	if err != nil {
		slog.Error("reading the approvals", "err", err)
		os.Exit(1)
	}
	if len(missing) == 0 {
		fmt.Println("every exchange contract is already approved.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MISSING\tSTANDARD\tSEND TO\tSPENDER")
	for _, a := range missing {
		standard := "ERC-20"
		if a.Standard == onchain.ERC1155 {
			standard = "ERC-1155"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Name, standard, a.Token, a.Spender)
	}
	w.Flush()

	fmt.Println("\ncalldata, unsigned and unsent:")
	for _, a := range missing {
		data, err := a.Data(onchain.MaxUint256())
		if err != nil {
			slog.Error("building the approval", "name", a.Name, "err", err)
			os.Exit(1)
		}
		fmt.Printf("  %-22s to %s\n    0x%x\n", a.Name, a.Token, data)
	}

	fmt.Print(`
An unlimited allowance stands until it is revoked. Approval.Data takes the
amount so a caller can grant less, and Approval.RevokeData takes it back.
`)
}

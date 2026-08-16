// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Command wallets shows which Polymarket account a key controls.
//
// A Polymarket account is usually not the key that signs for it. Signing up
// with Google or an email link, connecting MetaMask, or opening an account
// today each gives a different smart contract, and the address on
// polymarket.com is that contract's — so an order made in the key's own name
// spends a balance that is somewhere else.
//
// Every one of those addresses is fixed by the owner and can be worked out
// offline. This derives all four, asks the relayer which are deployed, and
// asks the data API what each holds:
//
//	go run ./examples/wallets -owner 0x1234...
//
// With no -owner it uses a fixed public address, so it runs with no arguments.
// It is read-only: nothing here signs, spends or deploys anything.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ChloePike/go-polymarket"
	"github.com/ChloePike/go-polymarket/data"
	"github.com/ChloePike/go-polymarket/relayer"
)

// A form is one account form: how it is created, and what to call it.
type form struct {
	name          string
	how           string
	signatureType polymarket.SignatureType
	walletType    relayer.WalletType
}

// The four account forms, oldest first. Their signature types are the numbers
// an order carries, and they double as the index into the derivations.
var forms = []form{
	{"EOA", "the key trading for itself", polymarket.SigEOA, ""},
	{"Proxy", "Magic Link or Google sign-in", polymarket.SigPolyProxy, relayer.WalletTypeProxy},
	{"Safe", "MetaMask or another external signer", polymarket.SigPolyGnosisSafe, relayer.WalletTypeSafe},
	{"Deposit", "every account created since May 2026", polymarket.SigEIP1271, relayer.WalletTypeWallet},
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Vitalik Buterin's address: a well-known one that is nobody's Polymarket
	// account, so the derivations are real and the balances are empty.
	owner := flag.String("owner", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", "the signing key's address")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	contracts, ok := polymarket.ContractsFor(polymarket.ChainPolygon)
	if !ok {
		slog.Error("no contracts for Polygon")
		os.Exit(1)
	}

	rc := relayer.New()
	dc := data.New()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "owner\t%s\n\n", polymarket.ChecksumAddress(*owner))
	fmt.Fprintln(w, "FORM\tSIGTYPE\tADDRESS\tDEPLOYED\tVALUE\tCREATED BY")

	for _, f := range forms {
		address, err := polymarket.DeriveWallet(f.signatureType, *owner, contracts)
		if err != nil {
			fmt.Fprintf(w, "%s\t%d\t-\t-\t-\t%s\n", f.name, f.signatureType, f.how)
			continue
		}

		deployed := "n/a"
		if f.walletType != "" {
			// The relayer answers this for free and without a key.
			if yes, err := rc.Deployed(ctx, *owner, f.walletType); err != nil {
				deployed = "?"
			} else if yes {
				deployed = "yes"
			} else {
				deployed = "no"
			}
		}

		value := "?"
		if v, err := dc.Value(ctx, address, nil); err == nil && len(v) > 0 {
			value = "$" + v[0].Value.String()
		}

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			f.name, f.signatureType, address, deployed, value, f.how)
	}
	w.Flush()

	// The pre-upgrade deposit wallet is a separate address for the same
	// owner. An account opened before June 2026 is only reachable there.
	legacy, err := polymarket.DeriveDepositWalletUUPS(*owner,
		contracts.DepositWalletFactory, contracts.DepositWalletImplementation)
	if err != nil {
		slog.Error("deriving the pre-upgrade deposit wallet", "err", err)
		os.Exit(1)
	}
	fmt.Printf("\nDeposit wallets opened before the June 2026 upgrade sit elsewhere:\n  %s\n", legacy)

	fmt.Print(`
To trade from one of these, let the wallet fill in the order:

    wallet, err := polymarket.NewWallet(polymarket.SigEIP1271, owner, polymarket.ChainPolygon)
    opts := wallet.OrderOptions()   // sets both the signature type and the funder
`)
}

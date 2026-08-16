// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import "testing"

// A walletCase is one owner key and the wallet each account form derives to
// for it.
//
// The expected addresses are the output of Polymarket's own relayer client,
// @polymarket/builder-relayer-client, not of this package. A derivation that
// agrees with itself proves nothing: the wallet holds the money, and an
// address this client invented would send orders on behalf of an account that
// does not exist.
type walletCase struct {
	name        string
	owner       string
	proxy       string
	safe        string
	depositUUPS string
}

// The two legacy factories salt CREATE2 differently — the proxy factory over
// the owner's twenty raw bytes, the Safe factory over the address padded to a
// word — so every owner here derives to two unrelated addresses. A bug that
// swapped the two salts would still produce well-formed addresses.
var walletCases = []walletCase{
	{
		name:        "the hardhat key the golden vectors use",
		owner:       "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		proxy:       "0x365f0CA36Ae1f641E02fE3B7743673da42A13A70",
		safe:        "0xd93B25cb943D14d0d34FBaF01Fc93a0f8b5F6E47",
		depositUUPS: "0xdf8b9E8f9AB23f261F6e1B171B7454ae6E46Ba76",
	},
	{
		name:        "the lowest non-zero address",
		owner:       "0x0000000000000000000000000000000000000001",
		proxy:       "0x7754536ecd85c00b2E0CF9c1aA679340D8550756",
		safe:        "0x766b6851A199BF91Ae3fa13B1cfaC5187355118f",
		depositUUPS: "0x57ffBc34De23124fAeb8387fcd689d314E57aCcD",
	},
	{
		name:        "every bit set",
		owner:       "0xffffffffffffffffffffffffffffffffffffffff",
		proxy:       "0x2CDCdEfE9F04b9f78b0573755Ee270d03F2c319c",
		safe:        "0xB9a5f0449Db856186d6c86d347B28E18C440594C",
		depositUUPS: "0x5F1b3436c68810d6B8E341EBd9A7AACb5a0ACf05",
	},
	{
		name:        "a second hardhat key",
		owner:       "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		proxy:       "0xd9d24e482c11F586cd9A1a53dC3eEc6dE3883862",
		safe:        "0x8ac5D4Bd2752AFc9F5CA531f19D617647216B893",
		depositUUPS: "0x5F4f45aBd6e86C60B3Df2a4aF103f85256A2Ce6d",
	},
	{
		name:        "an address with mixed case in its checksum",
		owner:       "0xdD2FD4581271e230360230F9337D5c0430Bf44C0",
		proxy:       "0x33644b30068E1a463C4c747088D4e902b0120C1D",
		safe:        "0xf506f113b4A1Aa282E74fDcDdf9C9cDc9D48F750",
		depositUUPS: "0x00C6fc1bF02Fe3EB6a924e9794F4f4B0bD7D13ca",
	},
}

func TestDeriveWalletMatchesTheOfficialClient(t *testing.T) {
	c, ok := ContractsFor(ChainPolygon)
	if !ok {
		t.Fatal("no contracts for Polygon")
	}
	for _, tc := range walletCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveProxyWallet(tc.owner, c.ProxyFactory)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.proxy {
				t.Errorf("proxy wallet = %s, want %s", got, tc.proxy)
			}

			if got, err = DeriveSafeWallet(tc.owner, c.SafeFactory); err != nil {
				t.Fatal(err)
			}
			if got != tc.safe {
				t.Errorf("Safe = %s, want %s", got, tc.safe)
			}

			got, err = DeriveDepositWalletUUPS(tc.owner, c.DepositWalletFactory, c.DepositWalletImplementation)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.depositUUPS {
				t.Errorf("deposit wallet = %s, want %s", got, tc.depositUUPS)
			}
		})
	}
}

// A depositWalletCase is one deposit-wallet derivation. Target is the
// implementation for a UUPS wallet and the beacon for a beacon one; the two
// eras of the wallet hash different creation code, so the same owner and
// factory derive different addresses.
type depositWalletCase struct {
	name    string
	owner   string
	factory string
	target  string
	beacon  bool
	want    string
}

// These come from py-builder-relayer-client, a second official implementation
// written independently of the TypeScript one the cases above are pinned to.
// The beacon variant is here because it is the current wallet and npm has no
// release that exports it: agreeing with a different language's client is the
// strongest check available without deploying one.
var depositWalletCases = []depositWalletCase{
	{
		name:    "the current beacon wallet on Polygon",
		owner:   "0x0000000000000000000000000000000000000001",
		factory: "0x00000000000Fb5C9ADea0298D729A0CB3823Cc07",
		target:  "0x7A18EDfe055488A3128f01F563e5B479D92ffc3a",
		beacon:  true,
		want:    "0x94bF330955A0b957662fEaF878dE77bf25f76cD9",
	},
	{
		name:    "a pre-upgrade wallet on Polygon",
		owner:   "0xA60601A4d903af91855C52BFB3814f6bA342f201",
		factory: "0x00000000000Fb5C9ADea0298D729A0CB3823Cc07",
		target:  "0x58CA52ebe0DadfdF531Cde7062e76746de4Db1eB",
		want:    "0x8b60BF0f650Bf7a0d93F10D72375b37De18F8c40",
	},
	{
		// A factory other than Polymarket's own, which proves the factory is
		// read from the argument rather than baked into the derivation: it
		// appears twice, once as the CREATE2 deployer and once inside the
		// arguments that make the salt.
		name:    "another factory entirely",
		owner:   "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		factory: "0x801c740Bcd28531d75a5da176D5511F3329Ab049",
		target:  "0x24f3257BF9451bA575E864777ab6f8D7Eac0139B",
		want:    "0x3c6D4D368D1Af2C1555Effbf17Da30Add851A6Ae",
	},
}

func TestDeriveDepositWallet(t *testing.T) {
	for _, tc := range depositWalletCases {
		t.Run(tc.name, func(t *testing.T) {
			derive := DeriveDepositWalletUUPS
			if tc.beacon {
				derive = DeriveDepositWallet
			}
			got, err := derive(tc.owner, tc.factory, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("deposit wallet = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestDepositWalletErasDiffer pins the fact that makes the two derivations
// worth keeping apart. Given the same owner and the same factory, the beacon
// wallet and the pre-upgrade wallet are different addresses, so an account
// created before the upgrade is not reachable through the current derivation.
func TestDepositWalletErasDiffer(t *testing.T) {
	c, _ := ContractsFor(ChainPolygon)
	owner := walletCases[0].owner

	beacon, err := DeriveDepositWallet(owner, c.DepositWalletFactory, c.DepositWalletBeacon)
	if err != nil {
		t.Fatal(err)
	}
	uups, err := DeriveDepositWalletUUPS(owner, c.DepositWalletFactory, c.DepositWalletImplementation)
	if err != nil {
		t.Fatal(err)
	}
	if beacon == uups {
		t.Fatalf("both eras derived %s; one of the two init code layouts is not being used", beacon)
	}
}

// TestDeriveSafeMatchesTheSecondClient is one more Safe address, taken from
// py-builder-relayer-client rather than from the TypeScript one.
func TestDeriveSafeMatchesTheSecondClient(t *testing.T) {
	const (
		owner = "0x6e0c80c90ea6c15917308F820Eac91Ce2724B5b5"
		want  = "0x6d8c4e9aDF5748Af82Dabe2C6225207770d6B4fa"
	)
	c, _ := ContractsFor(ChainPolygon)
	got, err := DeriveSafeWallet(owner, c.SafeFactory)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Safe = %s, want %s", got, want)
	}
}

// TestDeriveWalletDispatchesOnSignatureType checks that the entry point picks
// the same derivation the caller would have picked by hand, and that an EOA
// derives to itself rather than to a wallet.
func TestDeriveWalletDispatchesOnSignatureType(t *testing.T) {
	c, _ := ContractsFor(ChainPolygon)
	owner := walletCases[0].owner

	if got, err := DeriveWallet(SigEOA, owner, c); err != nil || got != owner {
		t.Errorf("EOA derived to %s (%v), want the owner itself", got, err)
	}
	if got, err := DeriveWallet(SigPolyProxy, owner, c); err != nil || got != walletCases[0].proxy {
		t.Errorf("proxy derived to %s (%v), want %s", got, err, walletCases[0].proxy)
	}
	if got, err := DeriveWallet(SigPolyGnosisSafe, owner, c); err != nil || got != walletCases[0].safe {
		t.Errorf("Safe derived to %s (%v), want %s", got, err, walletCases[0].safe)
	}
	if _, err := DeriveWallet(SignatureType(9), owner, c); err == nil {
		t.Error("an unknown signature type derived an address")
	}
}

// TestDeriveWalletRefusesAnAbsentFactory checks the Amoy case. The proxy
// factory was never deployed there, so its address is empty; running CREATE2
// against an empty deployer would produce a plausible address for a contract
// that cannot exist.
func TestDeriveWalletRefusesAnAbsentFactory(t *testing.T) {
	c, ok := ContractsFor(ChainAmoy)
	if !ok {
		t.Fatal("no contracts for Amoy")
	}
	if _, err := DeriveWallet(SigPolyProxy, walletCases[0].owner, c); err == nil {
		t.Error("derived a proxy wallet on a chain with no proxy factory")
	}
	if _, err := DeriveWallet(SigPolyGnosisSafe, walletCases[0].owner, c); err != nil {
		t.Errorf("Amoy has a Safe factory but derivation failed: %v", err)
	}
}

// TestWalletOrderOptionsSetsBothFields checks the pairing that decides whether
// an order is accepted: the signature type tells the exchange which
// verification path to take, and the funder tells it whose balance to spend.
// Setting one without the other is an order against the wrong account.
func TestWalletOrderOptionsSetsBothFields(t *testing.T) {
	w, err := NewWallet(SigPolyGnosisSafe, walletCases[0].owner, ChainPolygon)
	if err != nil {
		t.Fatal(err)
	}
	opts := w.OrderOptions()
	if opts.SignatureType != SigPolyGnosisSafe {
		t.Errorf("signature type = %d, want %d", opts.SignatureType, SigPolyGnosisSafe)
	}
	if opts.Funder != walletCases[0].safe {
		t.Errorf("funder = %s, want the Safe %s", opts.Funder, walletCases[0].safe)
	}

	// An EOA funds itself, and naming a funder for it would put an address in
	// a field the exchange reads as "somebody else pays".
	eoa, err := NewWallet(SigEOA, walletCases[0].owner, ChainPolygon)
	if err != nil {
		t.Fatal(err)
	}
	if got := eoa.OrderOptions(); got.Funder != "" {
		t.Errorf("an EOA named a funder: %s", got.Funder)
	}
}

// TestDeriveWalletRejectsABadOwner checks that a malformed owner is refused
// rather than hashed into a plausible-looking address.
func TestDeriveWalletRejectsABadOwner(t *testing.T) {
	c, _ := ContractsFor(ChainPolygon)
	for _, owner := range []string{"", "0x", "0xnothex", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb922"} {
		if _, err := DeriveWallet(SigPolyProxy, owner, c); err == nil {
			t.Errorf("derived a wallet for owner %q", owner)
		}
		if _, err := DeriveWallet(SigEIP1271, owner, c); err == nil {
			t.Errorf("derived a deposit wallet for owner %q", owner)
		}
	}
}

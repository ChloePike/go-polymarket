// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

// Polymarket serves its public API from four hosts. The CLOB host carries the
// order book and everything that needs a signature; the others are read-only.
const (
	// DefaultHost is the central limit order book: markets, books, orders,
	// trades, rewards and authentication.
	DefaultHost = "https://clob.polymarket.com"

	// GammaHost serves market and event metadata: the objects behind a
	// Polymarket page, including titles, slugs, tags and resolution state.
	GammaHost = "https://gamma-api.polymarket.com"

	// DataHost serves portfolio and analytics data: positions, activity and
	// holders, keyed by wallet address.
	DataHost = "https://data-api.polymarket.com"

	// WSHost is the CLOB streaming endpoint. The market and user channels
	// hang off it; see the ws package.
	WSHost = "wss://ws-subscriptions-clob.polymarket.com/ws"
)

// Supported chains. Polymarket runs on Polygon; Amoy is its testnet.
const (
	ChainPolygon int64 = 137
	ChainAmoy    int64 = 80002
)

// Contracts holds the on-chain addresses for one chain. The exchange contracts
// double as the EIP-712 verifying contract when signing an order, so picking
// the wrong one produces a signature the exchange rejects.
type Contracts struct {
	Exchange          string // V1 exchange
	NegRiskExchange   string // V1 exchange for neg-risk markets
	ExchangeV2        string
	NegRiskExchangeV2 string
	ExchangeV3        string
	NegRiskAdapter    string
	Collateral        string // USDC
	ConditionalTokens string // ERC-1155 outcome tokens

	// The wallet factories. Polymarket accounts are mostly not the key that
	// signs for them: a smart wallet holds the funds and an ordinary key
	// authorises it. Each factory deploys its wallets at an address derived
	// from the owner, so the address is known before the wallet exists.
	// See DeriveWallet.
	ProxyFactory         string // POLY_PROXY, from Magic Link or Google sign-in
	SafeFactory          string // POLY_GNOSIS_SAFE, from an external signer
	SafeMultisend        string // batches several calls into one Safe transaction
	RelayHub             string // the proxy relay a gasless proxy transaction names
	DepositWalletFactory string // the current wallet, deployed since May 2026

	// DepositWalletBeacon and DepositWalletImplementation are two eras of the
	// same wallet. Wallets created after the June 2026 upgrade sit behind an
	// ERC-1967 beacon; earlier ones point straight at an implementation. The
	// two derive different addresses for the same owner, so the wrong one
	// names a wallet that holds nothing.
	DepositWalletBeacon         string
	DepositWalletImplementation string
}

var (
	polygonContracts = Contracts{
		Exchange:          "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
		NegRiskExchange:   "0xC5d563A36AE78145C45a50134d48A1215220f80a",
		ExchangeV2:        "0xE111180000d2663C0091e4f400237545B87B996B",
		NegRiskExchangeV2: "0xe2222d279d744050d28e00520010520000310F59",
		ExchangeV3:        "0xe3333700cA9d93003F00f0F71f8515005F6c00Aa",
		NegRiskAdapter:    "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
		Collateral:        "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
		ConditionalTokens: "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045",

		ProxyFactory:                "0xaB45c5A4B0c941a2F231C04C3f49182e1A254052",
		SafeFactory:                 "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b",
		SafeMultisend:               "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761",
		RelayHub:                    "0xD216153c06E857cD7f72665E0aF1d7D82172F494",
		DepositWalletFactory:        "0x00000000000Fb5C9ADea0298D729A0CB3823Cc07",
		DepositWalletBeacon:         "0x7A18EDfe055488A3128f01F563e5B479D92ffc3a",
		DepositWalletImplementation: "0x58CA52ebe0DadfdF531Cde7062e76746de4Db1eB",
	}
	amoyContracts = Contracts{
		Exchange:          "0xdFE02Eb6733538f8Ea35D585af8DE5958AD99E40",
		NegRiskExchange:   "0xC5d563A36AE78145C45a50134d48A1215220f80a",
		ExchangeV2:        "0xE111180000d2663C0091e4f400237545B87B996B",
		NegRiskExchangeV2: "0xe2222d279d744050d28e00520010520000310F59",
		ExchangeV3:        "0x9fE6e61422AdB6F610d8597F9684b16912D50C3D",
		NegRiskAdapter:    "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
		Collateral:        "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB",
		ConditionalTokens: "0x69308FB512518e39F9b16112fA8d994F4e2Bf8bB",

		// The proxy factory was never deployed on Amoy, so ProxyFactory and
		// RelayHub stay empty and DeriveWallet refuses rather than computing
		// an address for a factory that is not there.
		SafeFactory:                 "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b",
		SafeMultisend:               "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761",
		DepositWalletFactory:        "0x00000000000Fb5C9ADea0298D729A0CB3823Cc07",
		DepositWalletBeacon:         "0x7A18EDfe055488A3128f01F563e5B479D92ffc3a",
		DepositWalletImplementation: "0x50a88fE9a441cB4c9c2aD6A2207CE2795C7D7Fbd",
	}
)

// ContractsFor returns the addresses for a chain, and reports whether the
// chain is known.
func ContractsFor(chainID int64) (Contracts, bool) {
	switch chainID {
	case ChainPolygon:
		return polygonContracts, true
	case ChainAmoy:
		return amoyContracts, true
	}
	return Contracts{}, false
}

// EIP-712 domain for order signing. The name is shared by every exchange
// version; the version string and the verifying contract are what separate
// them, so an order signed for V2 will not verify on V3.
const (
	exchangeDomainName = "Polymarket CTF Exchange"
	exchangeV1Version  = "1"
	exchangeV2Version  = "2"
	exchangeV3Version  = "3"
)

// EIP-712 domain for level-1 authentication. It deliberately carries no
// verifying contract: the field is absent from the type string, not zeroed.
const (
	clobAuthDomainName    = "ClobAuthDomain"
	clobAuthDomainVersion = "1"
	clobAuthMessage       = "This message attests that I control the given wallet"

	clobAuthTypeString = "ClobAuth(address address,string timestamp,uint256 nonce,string message)"
)

// orderTypeString is the V2 and V3 Order struct. Exactly eleven fields are
// signed. The wire JSON also carries taker and expiration, and neither appears
// here — signing them produces a signature the exchange rejects.
const orderTypeString = "Order(uint256 salt,address maker,address signer," +
	"uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side," +
	"uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

// Fixed-point scale of USDC and of the conditional tokens alike.
const Decimals = 6

// ZeroAddress is the taker of an order open to anyone.
const ZeroAddress = "0x0000000000000000000000000000000000000000"

// ZeroBytes32 is the default metadata and builder value.
const ZeroBytes32 = "0x0000000000000000000000000000000000000000000000000000000000000000"

// Cursor sentinels for the paginated CLOB endpoints. A page request starts at
// CursorStart, and a response repeats CursorEnd once the last page is served.
// Both are base64: "MA==" is "0" and "LTE=" is "-1".
const (
	CursorStart = "MA=="
	CursorEnd   = "LTE="
)

// BuilderFeeBps is the denominator builder fee rates are quoted against, so a
// rate of 100 is one percent.
const BuilderFeeBps = 10000

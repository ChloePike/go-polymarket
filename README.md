# go-polymarket

[![Go Reference](https://pkg.go.dev/badge/github.com/ChloePike/go-polymarket.svg)](https://pkg.go.dev/github.com/ChloePike/go-polymarket)
[![CI](https://github.com/ChloePike/go-polymarket/actions/workflows/ci.yml/badge.svg)](https://github.com/ChloePike/go-polymarket/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/license-GPL--3.0--or--later-blue.svg)](LICENSE)

A clean-room Go client for the [Polymarket](https://polymarket.com) APIs —
the order book, market metadata, portfolio data, cross-chain funding, the
gasless relayer and the streaming feeds.

Not affiliated with Polymarket. Protocol details are transcribed from the
public API and the official TypeScript SDK; no third-party client source is
vendored. **GPL-3.0-or-later.**

## One package per API

Polymarket is six services, so this is six client packages over one shared
foundation, plus one that bypasses Polymarket entirely and talks to Polygon.
Import only what you use.

| Import | What it talks to | Auth |
|---|---|---|
| `go-polymarket/clob` | the order book: markets, prices, orders, trades, rewards | none to trade-level |
| `go-polymarket/gamma` | market and event metadata: titles, slugs, tags, resolution | none, plus a sign-in |
| `go-polymarket/data` | portfolio and analytics: positions, activity, holders | none |
| `go-polymarket/bridge` | deposits and withdrawals across thirteen chains | none |
| `go-polymarket/relayer` | gasless transactions for a smart wallet | none to key |
| `go-polymarket/ws` | live market and user streams | none to trade-level |
| `go-polymarket/onchain` | Polygon directly: approvals, positions, signed transactions | a key and a node |
| `go-polymarket` | the wallet, the order types, signing, and the shared session | — |

## Reading needs nothing

```go
import "github.com/ChloePike/go-polymarket/clob"

c := clob.New()

book, err := c.OrderBook(ctx, tokenID)
mid, err := c.Midpoint(ctx, tokenID)
```

```go
import "github.com/ChloePike/go-polymarket/data"

d := data.New()
positions, err := d.Positions(ctx, data.PositionsParams{User: address})
```

## Trading needs a key

```go
import (
    polymarket "github.com/ChloePike/go-polymarket"
    "github.com/ChloePike/go-polymarket/clob"
)

key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
c := clob.New(clob.WithSigner(key))

// Level 1 proves you control the wallet and returns level-2 credentials.
creds, err := c.CreateOrDeriveAPIKey(ctx)

// Everything after that is signed with those credentials.
order, err := c.CreateOrder(ctx, polymarket.UserOrder{
    TokenID: tokenID,
    Price:   "0.52",
    Size:    "100",
    Side:    polymarket.Buy,
}, polymarket.OrderOptions{})

resp, err := c.PostOrder(ctx, order, polymarket.GTC, clob.SubmitOptions{})
```

`CreateOrder` looks up the market's tick size and neg-risk flag, sizes the
order and signs it. Signing an order authorises a trade, so the client checks
what it can — price inside the tradable band, token id well formed, amounts
exactly representable — before signing rather than after.

Note where the two imports divide. The **vocabulary of an order** lives in the
root package, because an order means the same thing however it is sent:
`UserOrder`, `Buy`, `GTC`, `OrderOptions`. **Sending one** lives in `clob`,
because that is a property of the endpoint: `SubmitOptions`, `PostOrder`,
`CancelAll`.

## Sharing one session

A `Session` carries the wallet, the `http.Client` and the retry policy.
Configure it once and hand it to as many clients as you need.

```go
s := polymarket.NewSession(polymarket.DefaultHost, polymarket.WithSigner(key))
c := clob.NewWithSession(s)
```

## What makes this client careful

**Exact money math, everywhere and not just where it is signed.** Prices and
sizes are decimal strings, the integer amounts an order carries are computed
with `math/big.Rat`, and **no field carrying money is a `float64`** — a
position size, a mark-to-market value, a best bid all decode to `json.Number`
and keep the exact text the server sent. That matters because the size you
read is the size you sign when you close a position:

```go
size, err := polymarket.ParseAmount(string(position.Size))   // exact
```

The official SDK does this arithmetic in `float64`; a 2520-point grid
captured from that SDK agrees with this implementation on every point, so
exactness costs nothing.

**Byte-identical signatures.** `testdata/vectors.json` holds order digests,
signatures, authentication payloads and amounts produced by
`@polymarket/clob-client-v2`, and the tests assert this client reproduces them
exactly.

**Verified against the exchange, not just against another client.**
`examples/authcheck` generates a throwaway key, exchanges it for real
credentials, spends them on an authenticated call and then corrupts a
signature on purpose to confirm the exchange is really checking. It costs
nothing and places no order. It is how the signing bug in the level-2 HMAC was
found: the query string is not covered by the signature, which no golden
vector could have shown.

**Writes are never retried.** Reads retry twice on a connection failure.
Resending an order that may already have been received is how an account ends
up holding a position it asked for once.

**It knows which account you actually have.** A Polymarket account is usually
not the key that signs for it: Google sign-in, MetaMask and a new account
today each give a different smart contract, and that contract is what holds
the money. This derives all of them from the key, offline, and signs each
form's orders the way that form requires — including the wrapped ERC-7739
signature a current deposit wallet needs, which is 317 bytes rather than 65
and is nested the opposite way round from what the ERC's own text describes.

```go
wallet, err := polymarket.NewWallet(polymarket.SigEIP1271, owner, polymarket.ChainPolygon)
opts := wallet.OrderOptions()   // sets both the signature type and the funder
```

**A relayer credential from nothing but a wallet.** Every authenticated relayer
call takes a key, and listing keys takes a key too — so the credential has to
come from somewhere. It comes from signing in with Ethereum: `gamma` exchanges
a wallet signature for a session cookie, and the relayer mints a key against
that session. One jar carries the login between the two hosts.

```go
jar, _ := polymarket.NewCookieJar()

g := gamma.New(gamma.WithSigner(key), gamma.WithCookieJar(jar))
if _, err := g.Login(ctx); err != nil { ... }

r := relayer.New(relayer.WithCookieJar(jar))
minted, err := r.MintAPIKey(ctx)      // {apiKey, address, createdAt}

r = relayer.New(relayer.WithAPIKey(minted.Credentials()))
```

This is the only cookie-carrying flow in the library, and a session stores
cookies only when `WithCookieJar` asks it to — so nothing else accumulates one.

**Two ways to move money on chain.** Polymarket's relayer pays the gas for a
smart wallet, and `relayer` signs the three meta-transaction families it takes.
When there is no relayer in the picture — an EOA trading for itself, or a
recovery that must not depend on a third party — `onchain` signs an EIP-1559
transaction and broadcasts it through a node of your choosing. There is no
default node: an endpoint sees every address you ask about.

```go
c := onchain.New(nodeURL)
missing, err := c.MissingApprovals(ctx, owner, nil)   // reads only, spends nothing
```

Splitting, merging and redeeming positions are there too, for both the
conditional-token framework and the neg-risk adapter — including the pair of
`redeemPositions` calls that take the same Go types and mean different things,
one index sets and one amounts.

A smart wallet cannot be driven this way, and that is the contracts' rule
rather than a gap here: its factory deploys only for a Polymarket operator, and
the wallet performs a batch only when the factory asks. The package documents
what each call answers instead, and reads the wallet — deployed, batch nonce,
owner, and whether the address derived offline is the one the factory would
deploy.

**Three dependencies, all pure Go.** Keccak-256 from `x/crypto`, secp256k1
from `decred` — the same library go-ethereum's own pure-Go path wraps — and
`coder/websocket`. No cgo.

## Examples

```bash
go run ./examples/book         # an order book, live
go run ./examples/watch        # stream one book and print every move
go run ./examples/portfolio    # what a wallet holds
go run ./examples/wallets      # which account a key actually controls
go run ./examples/authcheck    # prove the signing stack against production
go run ./examples/check-builder <builder-code>
go run ./examples/approvals -rpc <node-url>    # which approvals an address still owes
go run ./examples/onchaincheck -rpc <node-url> # prove the on-chain layer against a real node
```

The first six run with no arguments and no credentials. The last two need a
Polygon JSON-RPC endpoint, because there is no default node to borrow.

## Safety

Trading moves real money. Every example here is read-only except two, and
neither can spend anything of yours: `authcheck` creates a free API key, and
`onchaincheck -broadcast` offers a node one transaction signed by a key
generated seconds earlier that holds nothing — it checks the balance first and
refuses to send if it is not zero. No test touches the live network. Keep
your key in the environment, never in a source file.

## Auditing and institutional signing

`Signer` sees only a digest, which is enough to sign but not enough to show
anyone what is being signed. Implement `TypedDataSigner` instead and you get
the whole EIP-712 payload — the fields, the domain, the type string — in the
exact shape `eth_signTypedData_v4` takes:

```go
func (s *auditSigner) SignTypedData(td polymarket.TypedData) ([]byte, error) {
    json.NewEncoder(s.log).Encode(td)   // the audit record is the payload itself
    digest, err := td.Digest()          // derive it; never accept one
    ...
}
```

The digest has a single implementation, so the payload an auditor reads and
the bytes a wallet signs cannot drift apart, and this client checks that the
returned signature recovers to the signing address before sending it.

The API secret has the same seam. `WithCredentials` holds the level-2 triple
in memory; `WithL2Authenticator` does not hold it at all — the interface takes
a request and returns its headers, so the secret can stay in a KMS, an HSM or
a signing service:

```go
type L2Authenticator interface {
    APIKey() string
    AuthHeaders(address, method, requestPath, body string) (polymarket.L2Headers, error)
}

c := clob.New(clob.WithSigner(remoteSigner), clob.WithL2Authenticator(kms))
```

The relayer's credentials have had the same hook all along, as
`relayer.WithAuthenticator`. Two bounds are worth knowing: `POLY_ADDRESS`
names the wallet rather than the credential, so a level-2 request still needs
a `Signer` — itself an interface a remote signer satisfies — and the websocket
user channel authenticates by putting the secret in the subscribe frame, where
no signing service can stand in for it.

Credentials also load from the environment, which is where they belong:

```go
creds, err := polymarket.CredentialsFromEnv()   // POLYMARKET_API_KEY, _SECRET, _PASSPHRASE
```

## Documentation

- **[TRADING.md](TRADING.md)** — running this in production: sessions, rate
  limits, book drift, why a timed-out write must never be resubmitted.
- **[DESIGN.md](DESIGN.md)** — protocol reference and the traps worth knowing.

## Contributing

Patches are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) has the four gates a
change has to pass and the four rules that are not style — named structs, no
`float64` for money, clean-room, and golden vectors for anything that signs.

Found something in the signing path? [SECURITY.md](SECURITY.md) — report it
privately, not as an issue.

## License

GPL-3.0-or-later — see [LICENSE](LICENSE).

# go-polymarket

[![Go Reference](https://pkg.go.dev/badge/github.com/ChloePike/go-polymarket.svg)](https://pkg.go.dev/github.com/ChloePike/go-polymarket)
[![CI](https://github.com/ChloePike/go-polymarket/actions/workflows/ci.yml/badge.svg)](https://github.com/ChloePike/go-polymarket/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/license-GPL--3.0--or--later-blue.svg)](LICENSE)

A clean-room Go client for the [Polymarket](https://polymarket.com) APIs —
the order book, market metadata, portfolio data and the streaming feeds.

Not affiliated with Polymarket. Protocol details are transcribed from the
public API and the official TypeScript SDK; no third-party client source is
vendored. **GPL-3.0-or-later.**

## One package per API

Polymarket is four services, so this is four client packages over one shared
foundation. Import only what you use.

| Import | What it talks to | Auth |
|---|---|---|
| `go-polymarket/clob` | the order book: markets, prices, orders, trades, rewards | none to trade-level |
| `go-polymarket/gamma` | market and event metadata: titles, slugs, tags, resolution | none |
| `go-polymarket/data` | portfolio and analytics: positions, activity, holders | none |
| `go-polymarket/ws` | live market and user streams | none to trade-level |
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

## Sharing one session

A `Session` carries the wallet, the `http.Client` and the retry policy.
Configure it once and hand it to as many clients as you need.

```go
s := polymarket.NewSession(polymarket.DefaultHost, polymarket.WithSigner(key))
c := clob.NewWithSession(s)
```

## What makes this client careful

**Exact money math.** Prices and sizes are decimal strings and the integer
amounts an order carries are computed with `math/big.Rat`. The official SDK
does this arithmetic in `float64`; a 2520-point grid captured from that SDK
agrees with this implementation on every point, so exactness costs nothing.

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

**Three dependencies, all pure Go.** Keccak-256 from `x/crypto`, secp256k1
from `decred` — the same library go-ethereum's own pure-Go path wraps — and
`coder/websocket`. No cgo.

## Examples

```bash
go run ./examples/book        # an order book, live
go run ./examples/portfolio   # what a wallet holds
go run ./examples/authcheck   # prove the signing stack against production
go run ./examples/check-builder <code>
```

## Safety

Trading moves real money. Every example here is read-only except `authcheck`,
which only creates a free API key, and no test touches the live network. Keep
your key in the environment, never in a source file.

## Documentation

Protocol reference, the traps worth knowing, and status:
**[DESIGN.md](DESIGN.md)**.

## License

GPL-3.0-or-later — see [LICENSE](LICENSE).

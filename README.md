# go-polymarket

A clean-room Go client for the [Polymarket](https://polymarket.com) APIs —
the order book, market metadata, portfolio data and the streaming feeds.

Not affiliated with Polymarket. Protocol details are transcribed from the
public API and the official TypeScript SDK; no third-party client source is
vendored. **GPL-3.0-or-later.**

```go
import "github.com/ChloePike/go-polymarket"
```

## Reading the market needs nothing

The zero `Client` is ready for every endpoint that takes no credentials.

```go
var c polymarket.Client

book, err := c.OrderBook(ctx, tokenID)
tick, err := c.TickSize(ctx, tokenID)
```

## Trading needs a key

```go
key, err := polymarket.NewPrivateKey(os.Getenv("POLYMARKET_KEY"))
c := &polymarket.Client{Signer: key}

// Level 1 proves you control the wallet and hands back level-2 credentials.
creds, err := c.CreateOrDeriveAPIKey(ctx)

// Everything after that is signed with those credentials.
order, err := c.CreateOrder(ctx, polymarket.UserOrder{
    TokenID: tokenID,
    Price:   "0.52",
    Size:    "100",
    Side:    polymarket.Buy,
}, polymarket.OrderOptions{})
resp, err := c.PostOrder(ctx, order, polymarket.GTC)
```

`CreateOrder` looks up the market's tick size and neg-risk flag, sizes the
order, and signs it. Signing an order authorises a trade, so the client checks
what it can — price inside the tradable band, token id well formed, amounts
exactly representable — before signing rather than after.

## What makes this client careful

**Exact money math.** Prices and sizes are decimal strings and the integer
amounts an order carries are computed with `math/big.Rat`. The official SDK
does this arithmetic in `float64`; a 2520-point grid captured from that SDK
agrees with this implementation on every point, so exactness costs nothing.

**Byte-identical signatures.** `testdata/vectors.json` holds order digests,
signatures, authentication payloads and amounts produced by
`@polymarket/clob-client-v2`. The test suite asserts this client reproduces
them exactly. Same inputs, same bytes.

**Three dependencies, all pure Go.** Keccak-256 from `x/crypto`, secp256k1 from
`decred` — the same library go-ethereum's own pure-Go path wraps — and
`coder/websocket`. No cgo, no 140-module dependency tree.

## Packages

| Import | Purpose |
|---|---|
| `github.com/ChloePike/go-polymarket` | the client: markets, orders, trades, rewards, metadata, portfolio |
| `github.com/ChloePike/go-polymarket/ws` | market and user streams |

## Safety

Trading moves real money. The examples in this repository are read-only unless
they say otherwise, and no test touches the live network. Keep your key in the
environment, never in a source file.

## Documentation

Protocol reference, the traps worth knowing, and the milestone status:
**[DESIGN.md](DESIGN.md)**.

## License

GPL-3.0-or-later — see [LICENSE](LICENSE).

# Running this in production

Engineering notes for systematic and automated trading against Polymarket.
This is about using the API correctly — what the exchange checks, where a
naive integration loses money to a bug rather than to the market. It is not
trading advice and says nothing about what to trade.

---

## Set up once, not per request

A `Session` carries the wallet, the `http.Client`, the user agent and the
retry policy. Build one and share it. A client per request means a TLS
handshake per request, no connection reuse, and a rate limit reached far
sooner than it needs to be.

```go
s := polymarket.NewSession(polymarket.DefaultHost,
    polymarket.WithSigner(key),
    polymarket.WithUserAgent("desk-mm/2.1"),
    polymarket.WithHTTPClient(&http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{
            MaxIdleConnsPerHost: 32,   // the default of 2 throttles a busy desk
            IdleConnTimeout:     90 * time.Second,
        },
    }),
)
c := clob.NewWithSession(s)
```

`Session` is safe for concurrent use. Do the level-1 handshake once at
start-up, keep the credentials, and reuse them: it is a signature per call
otherwise, and the wallet key does not need to be reachable after it.

**Watch the clock.** Every level-2 request carries a timestamp inside its
signature. A host whose clock has drifted gets 401s that look like bad
credentials. `Client.Time` returns the exchange's clock; compare against it at
start-up and alert on a skew of more than a few seconds.

## Read the stream, not the endpoint

Polling `/book` in a loop is the most common way to be both slow and rate
limited. The market channel pushes.

```go
conn, err := ws.DialMarket(ctx, tokenIDs)
for {
    event, err := conn.Read(ctx)
    ...
}
```

Three things a book consumer must handle:

- **`ReconnectEvent` means your local book is stale.** The connection restores
  its own subscription; it cannot restore your state. Discard the book and
  wait for the next snapshot. A consumer that keeps applying deltas across a
  reconnect is trading on a book that silently diverged.
- **Check the hash.** A snapshot carries one. `ws.BookHash` recomputes it, so
  drift is detectable rather than hypothetical.
- **Read from one goroutine.** Fan out after `Read`, not around it.

When you do need REST for many tokens, use the plural endpoints — `Books`,
`Midpoints`, `Prices`, `Spreads` take a list and answer in one round trip.
They return maps keyed by token id, not lists.

## Money math

Prices and sizes are decimal **strings** all the way to the wire. That is not
fussiness: an order's amounts are integers at six decimals, and they are
covered by a signature, so a rounding artefact is not a rounding artefact — it
is a signed commitment to the wrong number.

- Never format a price through `float64`. `strconv.FormatFloat(0.29*100, ...)`
  is how a desk ends up quoting 28.999999999999996.
- The tick size decides the rounding, and it varies per market. `CreateOrder`
  fetches it; if you cache it, cache it per token.
- Below `min_order_size` the exchange refuses the order. It is on the book
  response.
- Estimate fees **before** sizing, not after: `FeeCurve` then
  `AdjustBuyAmountForFees`. An order sized to the whole balance bounces off
  the fee.

## Failure, and why writes are never retried

Reads retry twice on a connection failure. Writes never do, at any level, and
your own code should hold the same line.

**A timed-out `PostOrder` is not a failed `PostOrder`.** The request may have
arrived. Resending it is how an account ends up holding twice the position it
asked for. On a timeout, reconcile:

```go
resp, err := c.PostOrder(ctx, order, polymarket.GTC, clob.SubmitOptions{})
if err != nil {
    // Do not resubmit. Find out what actually happened.
    orders, _, qerr := c.OpenOrders(ctx, clob.OpenOrderParams{Market: conditionID}, "")
    ...
}
```

Give every order a client-side identity you can recognise — a deterministic
salt derived from your own order id makes an order idempotent in practice,
because the same salt produces the same signature and the exchange sees a
duplicate rather than a second order.

**`order_version_mismatch` means rebuild, not retry.** The exchange has moved
to a different order version; the bytes you signed are for the old one. Call
`APIVersion`, rebuild with the new `OrderOptions.Version`, sign again.

**Rate limits are an `*polymarket.Error` with a status.** Branch on the code,
not on the text:

```go
var apiErr *polymarket.Error
if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
    backOff()
}
```

## Order lifecycle

| Type | Meaning | Notes |
|---|---|---|
| `GTC` | rests until cancelled | the default for quoting |
| `GTD` | rests until `Expiration` | see the caveat below |
| `FOK` | fills completely at once or not at all | cannot be post-only |
| `FAK` | takes what is there now, cancels the rest | cannot be post-only |

**`Expiration` is on the wire but is not signed.** Eleven fields are covered
by the signature and expiration is not one of them. The exchange honours it,
but do not treat it as a cryptographic guarantee the way you can treat the
price, the size or the builder code. Cancel what you need cancelled.

`CancelAll` is account-wide across every market. For a per-market flush use
`CancelMarketOrders`; for a list of ids use `CancelOrders`, which is one round
trip rather than N.

**Allowance is not balance.** A funded wallet with no allowance cannot trade.
`BalanceAllowance` reports both, and `UpdateBalanceAllowance` asks the
exchange to re-read the chain after you change one.

## Attribution

A builder code earns a share of the fee, and it is one of the eleven signed
fields. That is the point: nobody can re-attribute your order in flight
without invalidating the signature. Set `UserOrder.BuilderCode` and check the
rates with `BuilderFees` before you assume you are earning anything — a code
with both rates at zero is attributed and worth nothing.

## Auditing and institutional signing

If a signature is produced outside your process — a hardware wallet, an HSM,
an MPC service, a policy engine — implement `TypedDataSigner` rather than
`Signer`. It receives the whole EIP-712 payload instead of an opaque hash, so
the thing doing the signing can render it, log it, or refuse it.

```go
func (s *policySigner) SignTypedData(td polymarket.TypedData) ([]byte, error) {
    json.NewEncoder(s.auditLog).Encode(td)          // this is the audit record
    if err := s.policy.Allow(td.Message); err != nil { // this is the control
        return nil, err
    }
    digest, err := td.Digest()                       // derive it, never accept it
    ...
}
```

The payload marshals into exactly what `eth_signTypedData_v4` takes, so the
same JSON goes to ethers, viem or a wallet unchanged. This client verifies
that whatever comes back recovers to the signing address, so a signer that
signs something other than what it was shown fails locally instead of at the
exchange.

## Testing

- No test in this library touches the network. Point a client at an
  `httptest.Server` with `clob.WithHost(srv.URL)` and do the same.
- `go run ./examples/authcheck` proves the whole signing stack against
  production for free: it creates a throwaway key, exchanges it for real
  credentials, spends them, and confirms a corrupted signature is rejected.
  Run it after touching anything in the signing path. Do not put it in CI —
  it leaves a credential on the server each time.
- Every read endpoint is public. A staging environment for market data is
  just production with no key.

## The short version

1. One session, reused. Check your clock.
2. Stream the book; rebuild it on reconnect; verify the hash.
3. Strings for money, never floats. Fees before sizing.
4. A timed-out write is not a failed write. Reconcile, never resubmit.
5. Branch on status codes, not on error text.
6. Sign through `TypedDataSigner` if anyone will ever ask what you signed.

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

## Your account is probably not your key

This is the first thing to get right, because getting it wrong produces an
order that is signed correctly and rejected — or worse, accepted against an
account with nothing in it.

Polymarket gives most accounts a smart contract that holds the funds. The key
only authorises it. An account made with Google or an email link gets a proxy
wallet, one made by connecting MetaMask gets a Gnosis Safe, and everything
made since May 2026 gets a deposit wallet. The address on your dashboard is
that contract's, not your key's.

Derive it rather than pasting it, and let the wallet fill in the order:

```go
wallet, err := polymarket.NewWallet(polymarket.SigEIP1271, key.Address(), polymarket.ChainPolygon)

opts := wallet.OrderOptions()   // signature type and funder, which must agree
opts.TickSize = tickSize
order, err := polymarket.BuildOrder(user, key.Address(), opts)
```

Two fields decide this and they have to match: `SignatureType` tells the
exchange which verification path to take, and `Funder` tells it whose balance
to spend. `Wallet.OrderOptions` sets both. Setting one by hand and forgetting
the other is the common way to lose an afternoon.

`go run ./examples/wallets -owner 0x…` prints every form for a key, which is
deployed, and what each holds. If you are unsure which account you have, start
there.

**An older account may be at a different address.** Deposit wallets created
before the June 2026 upgrade sit at a pre-upgrade address;
`DeriveDepositWalletUUPS` derives that one. The relayer's `Deployed` will tell
you which exists.

**A contract wallet's order signature is not 65 bytes.** It is a wrapped
ERC-7739 blob, and `SignOrder` produces it automatically for `SigEIP1271`. If
you log signature lengths, or validate them, allow for it.

**Approvals and transfers go through the relayer, not through you.** A smart
wallet holds no gas. `relayer.BuildWalletBatch` and its siblings sign what the
wallet should do; `relayer.Submit` hands it to Polymarket to pay for and send.
That call spends money and cannot be taken back, and the relayer answers with
an id rather than a result — poll `relayer.Client.Transaction` until the state
is terminal rather than assuming the queue means success.

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

**You cannot place an order over the websocket.** Both CLOB channels are
read-only feeds. The formal AsyncAPI spec for the market channel lists every
message it knows about — `subscriptionRequest`, `subscriptionRequestUpdate`,
`ping`, `pong`, and the seven inbound event types — and none of them is an
order. The user channel takes an auth frame and subscribe/unsubscribe
operations, and pushes your orders and fills back; it accepts no commands
either. Order entry is `POST /order`, over HTTP, and the official TypeScript
client contains no websocket code at all.

The one exception is the RFQ gateway, a separate host where a market maker
sends `RFQ_QUOTE`, `RFQ_QUOTE_CANCEL` and `RFQ_CONFIRMATION_RESPONSE` frames —
that is quoting into a request-for-quote flow with a last-look step, not
posting to the book. It is documented but not implemented here and not
verified live. Perpetuals also publish `wss/perps-orders` documentation; that
is a different product from the prediction-market CLOB this client targets.

So the shape of a trading loop is settled by the protocol, not by taste:
stream the book, submit over REST.

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

**Classify the failure before you do anything about it.** Most failed writes
created nothing and need no reconciliation: a 400 means the exchange read the
order and refused it, a 401 means it never got past auth, and a local
validation failure never left the process. Querying open orders after one of
those is a wasted round trip on every rejected order — and on a busy desk that
is a lot of wasted round trips.

The failures that matter are the ones where nobody knows: a dropped
connection, a timeout, a 5xx. There the exchange may have received the order,
acted on it, and failed to say so. `Indeterminate` draws that line:

```go
resp, err := c.PostOrder(ctx, order, polymarket.GTC, clob.SubmitOptions{})
switch {
case err == nil:
    handle(resp)

case !polymarket.Indeterminate(err):
    // Refused, or never sent. No order exists; nothing to reconcile.
    // Fix the order, or back off on a 429, and move on.
    return err

default:
    // Its fate is unknown. Find out — never resubmit.
    orders, _, qerr := c.OpenOrders(ctx, clob.OpenOrderParams{Market: conditionID}, "")
    ...
}
```

**A timed-out `PostOrder` is not a failed `PostOrder`.** That is the whole
reason the distinction exists. Resending a request that may have arrived is
how an account ends up holding twice the position it asked for, and it is
indistinguishable from a legitimate second order at the exchange.

Give every order a client-side identity you can recognise during that
reconciliation. A deterministic salt derived from your own order id makes an
order idempotent in practice: the same salt produces the same signature, so a
resubmission the exchange has already seen is a duplicate rather than a second
position.

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
4. Classify a failed write with `Indeterminate` before reacting. A refusal
   needs no reconciliation; an unknown fate needs one, and never a resubmit.
5. Branch on status codes, not on error text.
6. Sign through `TypedDataSigner` if anyone will ever ask what you signed.

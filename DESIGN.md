# go-polymarket — design and protocol reference

A from-scratch Go client for **all** the public Polymarket APIs. GPL-3.0-or-later.

Every constant and layout below is transcribed from the public Polymarket
protocol as the official `@polymarket/clob-client-v2` SDK exposes it. These are
protocol facts, not copied source: this is a clean-room implementation.

---

## Shape

One root package, `polymarket`, in Go standard-library shape. Callers write
`polymarket.Client`, never `client.Client`. Machinery a caller never touches
lives under `internal/`.

```
client.go      package doc, Client, transport, Error, auth levels
protocol.go    hosts, chains, contracts, EIP-712 domains, endpoint paths
signer.go      Signer, PrivateKey, address derivation, EIP-55
auth.go        APICreds, level-1 EIP-712 headers, level-2 HMAC headers
order.go       order types, BuildOrder, BuildMarketOrder, digest, signing
market.go      market data reads
trading.go     orders, cancels, queries, API-key lifecycle
rewards.go     liquidity rewards
builder.go     builder-code attribution
data.go        the data API
fees.go        the pre-trade fee model
gamma.go       market and event metadata
internal/eip712/   Keccak-256, typed-data encoding, domain separator
internal/amount/   exact price/size to maker/taker conversion
ws/            market and user streams
testdata/      golden vectors and the script that regenerates them
examples/      runnable commands, read-only unless stated
```

## Hosts

| Host | Serves | Auth |
|---|---|---|
| `https://clob.polymarket.com` | order book, orders, trades, rewards, auth | none / L1 / L2 |
| `https://gamma-api.polymarket.com` | market and event metadata | none |
| `https://data-api.polymarket.com` | positions, activity, holders, value | none |
| `wss://ws-subscriptions-clob.polymarket.com/ws` | market and user streams | none / L2 |

## Dependencies

Three, all pure Go, no cgo:

- `golang.org/x/crypto/sha3` — Keccak-256. The standard library's `crypto/sha3`
  is FIPS-202 and pads differently; it is **not** interchangeable.
- `github.com/decred/dcrd/dcrec/secp256k1/v4` — signing. This is the same
  library go-ethereum's own pure-Go path wraps, so the signatures are identical
  for about three modules in `go.sum` instead of about 140.
- `github.com/coder/websocket` — the streaming feeds. Zero transitive deps.

---

## Protocol reference

### Contracts (Polygon, chain 137)

```
Exchange (V1)        0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E
NegRiskExchange (V1) 0xC5d563A36AE78145C45a50134d48A1215220f80a
ExchangeV2           0xE111180000d2663C0091e4f400237545B87B996B
NegRiskExchangeV2    0xe2222d279d744050d28e00520010520000310F59
ExchangeV3           0xe3333700cA9d93003F00f0F71f8515005F6c00Aa
USDC                 0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174
ConditionalTokens    0x4D97DCd97eC945f40cF65F87097ACe5EA0476045
```

Pick the exchange by the market's neg-risk flag (`GET /neg-risk?token_id=`) and
by the order version. V3 has a single exchange and ignores the flag. Amoy
(chain 80002) has its own set; see `ContractsFor`.

### Order EIP-712

Domain: `{name:"Polymarket CTF Exchange", version, chainId, verifyingContract}`
where `version` is `"2"` for V2 and `"3"` for V3. **V2 and V3 sign the same
eleven-field struct** and differ only in that version string and the contract,
which makes V3 support nearly free.

```
Order(uint256 salt,address maker,address signer,uint256 tokenId,
      uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType,
      uint256 timestamp,bytes32 metadata,bytes32 builder)
```

`side`: BUY=0, SELL=1. `signatureType`: EOA=0, POLY_PROXY=1, POLY_GNOSIS_SAFE=2,
EIP-1271=3. `timestamp` is unix **milliseconds**.

**Trap — eleven fields, not thirteen.** The wire JSON also carries `taker` (the
zero address) and `expiration` ("0"), and **neither is signed**. Signing them
produces a well-formed signature the exchange rejects.

```
digest = keccak256(0x19 ‖ 0x01 ‖ domainSeparator ‖ hashStruct(Order))
sig    = secp256k1(digest)            65 bytes, r ‖ s ‖ v, v ∈ {27,28}
```

**Trap — signature byte order.** `decred`'s `SignCompact` returns v ‖ r ‖ s with
v already offset by 27. Ethereum wants r ‖ s ‖ v. The reorder is pinned by a
golden test because a wrong order still verifies — to a different address.

**Trap — salt width.** The salt is signed as a uint256 but `POST /order` carries
it as a JSON *number*. A parser reading it as float64 loses precision above
2^53, so the exchange verifies a different struct than the one signed and
rejects the order, intermittently and only for large salts. `randomSalt` draws
below 2^52.

### Level-1 authentication (ClobAuth)

```
domain = {name:"ClobAuthDomain", version:"1", chainId}      // NO verifyingContract
type   = ClobAuth(address address,string timestamp,uint256 nonce,string message)
message= "This message attests that I control the given wallet"
```

**Trap — the absent field.** The domain has no verifying contract, so the field
leaves the *type string* entirely. Encoding it as the zero address yields a
different, wrong separator.

**Trap — string versus number.** `timestamp` is an EIP-712 `string` even though
it holds a number, so it is hashed; `nonce` is a real `uint256`, so it is
padded. Swapping the two authenticates as nobody.

Headers `POLY_ADDRESS / POLY_SIGNATURE / POLY_TIMESTAMP / POLY_NONCE` to
`POST /auth/api-key` or `GET /auth/derive-api-key`, which return
`{apiKey, secret, passphrase}`.

### Level-2 authentication (request HMAC)

```
message   = timestamp ‖ method ‖ requestPath ‖ body
key       = base64url-decode(secret)
signature = base64url(HMAC-SHA256(key, message))
```

Headers `POLY_ADDRESS / POLY_SIGNATURE / POLY_API_KEY / POLY_PASSPHRASE /
POLY_TIMESTAMP`.

**Trap — the query is not signed.** `requestPath` is the path *alone*. This is
easy to get backwards, and the failure is invisible until a request carries a
parameter: signing path+query authenticates fine against an endpoint with no
query and returns 401 for every filtered or paginated call. The proof that the
query is excluded is in the official client, which signs one set of headers and
then reuses them across every page of a pagination loop while `next_cursor`
changes underneath. Verified against production both ways.

### Amount math

Rounding limits come from the market tick size:

| tick | price | size | amount |
|---|---|---|---|
| 0.1 | 1 | 2 | 3 |
| 0.01 | 2 | 2 | 4 |
| 0.005 | 3 | 2 | 5 |
| 0.0025 | 4 | 2 | 6 |
| 0.001 | 3 | 2 | 5 |
| 0.0001 | 4 | 2 | 6 |

Limit order — price rounds **half-up**, size is in shares for both sides:

```
buy:  taker = roundDown(size);  maker = converge(taker × price)
sell: maker = roundDown(size);  taker = converge(maker × price)
```

Market order — price rounds **down**, and a buy is sized in **USDC**, so the
share count is a division:

```
buy:  maker = roundDown(usdc);   taker = converge(maker ÷ price)
sell: maker = roundDown(shares); taker = converge(maker × price)
```

`converge` nudges a derived amount up four places beyond the limit and only
then truncates, which recovers a value a division left just short. Both steps
are load-bearing.

Wire amounts are integers at 6 decimals for both USDC and the outcome tokens.

**All of this is exact.** The official SDK does it in float64; this client uses
`math/big.Rat`. A 2520-point grid of prices, sizes and tick sizes captured from
that SDK agrees on every single point, so exactness costs no compatibility and
removes a class of rounding artefact.

---

## Golden vectors

`testdata/vectors.json` is the observable output of
`@polymarket/clob-client-v2@1.1.0`, regenerated by `testdata/gen-vectors.mjs`
with a fixed salt, a fixed timestamp and the public Hardhat development key.

| Pinned | Count |
|---|---|
| order digests and signatures (V2, V3, neg-risk, builder code, expiration, five tick sizes) | 10 |
| level-1 ClobAuth digests and signatures | 3 |
| level-2 HMAC signatures | 4 |
| limit-order amount grid | 2520 |
| market-order amounts | 36 |
| key-to-address derivations | 4 |
| EIP-712 type hashes | 2 |

Regenerate:

```bash
mkdir -p /tmp/pmsdk && cd /tmp/pmsdk && npm init -y
npm install --ignore-scripts @polymarket/clob-client-v2@1.1.0
node <repo>/testdata/gen-vectors.mjs /tmp/pmsdk > <repo>/testdata/vectors.json
```

Same inputs, identical bytes. That is the whole correctness story.

---

## Status

| Component | State |
|---|---|
| `internal/eip712` — Keccak, typed data, domains | done, golden-pinned |
| `internal/amount` — exact amount math | done, 2556 golden points |
| `signer.go` — keys, addresses, EIP-55 | done, golden-pinned |
| `auth.go` — level 1 and level 2 | done, golden-pinned |
| `order.go` — build and sign, V1/V2/V3, limit and market | done, golden-pinned |
| `client.go` — transport, errors, auth plumbing | done |
| CLOB read and trade endpoints | in flight |
| data API | in flight |
| fee model | in flight |
| Gamma | in flight |
| websockets | in flight |

## Remaining risk

The signing core is retired: it reproduces the official SDK byte for byte. What
is left is breadth — one Go method per endpoint — plus two things no golden
vector can settle:

1. **Live trade acceptance.** No order has been placed. `POST /auth/api-key`
   costs nothing and proves level 1 end to end; a real order needs a funded
   wallet and an explicit decision.
2. **Response drift.** Field names are transcribed from live responses and the
   SDK's types. An endpoint nobody could observe live is marked as such.

## Clean-room rule

Reference the TS SDK and `Polymarket/go-order-utils` for *how the protocol
works*. Never paste third-party source into this tree.

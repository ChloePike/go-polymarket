# go-polymarket — design and protocol reference

A from-scratch Go client for **all** the public Polymarket APIs. GPL-3.0-or-later.

Every constant and layout below is transcribed from the public Polymarket
protocol as the official `@polymarket/clob-client-v2` SDK exposes it. These are
protocol facts, not copied source: this is a clean-room implementation.

---

## Shape

Polymarket is six services, so this is six client packages over one shared
foundation. A caller importing `gamma` never compiles a line of trading code.

```
polymarket/          the foundation, no endpoints
  session.go     Session, Option, Request, AuthLevel, Do and its helpers
  errors.go      Error, ErrNoSigner, ErrNoCredentials, Indeterminate
  signer.go      Signer, PrivateKey, address derivation, EIP-55, EIP-191
  auth.go        APICreds, level-1 EIP-712 headers, level-2 HMAC headers
  typeddata.go   TypedData, the EIP-712 encoder, TypedDataSigner
  order.go       order types, BuildOrder, BuildMarketOrder, digest, signing
  wallet.go      the account forms and their CREATE2 derivations
  erc7739.go     the wrapped signature a contract wallet's order carries
  protocol.go    hosts, chains, contracts, EIP-712 domains, decimals
  internal/eip712/   Keccak-256, typed-data encoding, domain separator
  internal/amount/   exact price/size to maker/taker conversion
  internal/abi/      Solidity ABI encoding, CREATE2
  internal/rlp/      the transaction encoding, encode only

clob/                the order book and everything that needs a signature
  client.go      Client, New, NewWithSession, the shared options
  endpoints.go   every CLOB path
  market.go      market data reads
  trading.go     orders, cancels, queries, the API-key lifecycle
  rewards.go     liquidity rewards
  builder.go     builder-code attribution
  fees.go        the pre-trade fee estimate

gamma/               market and event metadata, no authentication
  client.go      Client, New, NewWithSession, the shared options
  gamma.go       events and markets, and the stringified-array decoders
  extra.go       series, tags, comments, search, profiles, sports

data/                positions, activity, holders, no authentication

bridge/              deposits and withdrawals across chains, no authentication

relayer/             gasless transactions for a smart wallet
  client.go      Client, its own option type, both credential schemes
  relayer.go     nonces, relay payloads, transactions, deployment state
  transaction.go the Safe, proxy and deposit-wallet meta-transactions

onchain/             transactions sent straight to a node, no Polymarket host
  client.go      Client, the JSON-RPC plumbing, RPCError
  rpc.go         nonces, fees, calls, estimates, broadcast, receipts
  transaction.go the EIP-1559 transaction, its signature and encoding
  token.go       the ERC-20 and ERC-1155 approvals trading needs
  ctf.go         split, merge, redeem, convert, and the id derivations
  wallet.go      what a smart wallet can be read for from a node

ws/                  market, user and live-data streams
ratelimit.go         the published limits for every host, and the pacing

testdata/            golden vectors and the script that regenerates them
examples/            runnable commands, read-only unless stated
```

Each client package exposes the same constructor shape:

```go
c := clob.New(clob.WithSigner(key))     // its own session
c := clob.NewWithSession(shared)        // one session, several clients
```

The options are the root package's, re-exported, so one import is enough for
the common case and a `Session` is there when several clients should share a
wallet and an `http.Client`.

## Hosts

| Host | Serves | Auth |
|---|---|---|
| `https://clob.polymarket.com` | order book, orders, trades, rewards, auth | none / L1 / L2 |
| `https://gamma-api.polymarket.com` | market and event metadata | none |
| `https://data-api.polymarket.com` | positions, activity, holders, value | none |
| `https://bridge.polymarket.com` | deposits and withdrawals across chains | none |
| `https://relayer-v2.polymarket.com` | gasless transactions for a smart wallet | none / API key / builder HMAC |
| `wss://ws-subscriptions-clob.polymarket.com/ws` | market and user streams | none / L2 |

The relayer serves no specification of its own; the authoritative one is
`https://docs.polymarket.com/api-spec/relayer-openapi.yaml`. Its per-wallet
relay address changes over time, so it must be fetched rather than cached.

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

### Account forms

A Polymarket account is usually not the key that signs for it. The key
authorises a smart contract, the contract holds the funds, and the address on
polymarket.com is the contract's. An order names the contract as `maker`; the
`signer` field and the `signatureType` say how the exchange should verify it.

| `signatureType` | Form | Created by | `maker` | `signer` |
|---|---|---|---|---|
| 0 | EOA | a key trading for itself | the key | the key |
| 1 | Polymarket proxy | Magic Link or Google sign-in | the proxy | the key |
| 2 | Gnosis Safe | MetaMask or another external signer | the Safe | the key |
| 3 | deposit wallet | everything created since May 2026 | the wallet | **the wallet** |

Slot 3 is `POLY_1271` in the CLOB's own enumeration and `DEPOSIT_WALLET` in
the wallet documentation. They are the same slot: a deposit wallet is a
contract that verifies through EIP-1271. Note the last column — for that form
alone the `signer` field names the wallet, not the key, and the key that
produced the bytes appears nowhere in the order.

Every wallet is deployed with CREATE2, so its address is fixed by its owner
and derivable offline.

| Form | Factory (Polygon) | Salt | Init code hash |
|---|---|---|---|
| proxy | `0xaB45c5A4B0c941a2F231C04C3f49182e1A254052` | `keccak256(owner)`, twenty raw bytes | `0xd21df8dc…a00b` |
| Safe | `0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b` | `keccak256(abi.encode(owner))`, padded to a word | `0x2bce2127…cecf` |
| deposit | `0x00000000000Fb5C9ADea0298D729A0CB3823Cc07` | `keccak256(abi.encode(factory, bytes32(owner)))` | Solady `LibClone`, per era |

The two legacy salts differ, and the difference is invisible: both produce
well-formed addresses for the same owner, and they are not the same address.
Deposit wallets have two eras — a beacon proxy since the June 2026 upgrade
(beacon `0x7A18EDfe…ffc3a`) and a direct implementation before it
(`0x58CA52eb…Db1eB`) — which also derive different addresses, so an older
account is unreachable through the current derivation.

The proxy factory was never deployed on Amoy.

### The wrapped order signature (ERC-7739)

A deposit wallet holds no key, so it cannot sign; the exchange asks it to
verify instead. Polymarket wraps the order in ERC-7739 so that a signature
made for one wallet cannot be replayed into another. The owner signs

```
TypedDataSign(Order contents,string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)Order(…)
```

and what reaches the exchange is 317 bytes, not 65:

```
innerSig(65) ‖ appDomainSeparator(32) ‖ contentsHash(32) ‖ orderTypeString ‖ len(orderTypeString)(2)
```

**The nesting is inverted from the ERC's own text and that is deliberate.**
The inner signature is made against the *exchange's* domain, while the
*wallet's* domain travels as message fields — `name` is `"DepositWallet"`,
`version` is `"1"`, `verifyingContract` is the wallet, `salt` is zero. An
implementation corrected toward the specification produces signatures the
exchange refuses.

### Relayer meta-transactions

A smart wallet cannot pay gas, so the relayer sends its transactions. Three
wallets, three unrelated things to sign:

| Form | Signs | Domain | Then |
|---|---|---|---|
| Safe | `SafeTx(…)`, EIP-712 | **chainId and verifyingContract only** — no name, no version | EIP-191 personal sign, `v += 4` |
| proxy | `keccak256("rlx:" ‖ from ‖ to ‖ data ‖ fee ‖ gasPrice ‖ gasLimit ‖ nonce ‖ relayHub ‖ relay)` | none; not typed data | EIP-191 personal sign, `v` unchanged |
| deposit | `Batch(address wallet,uint256 nonce,uint256 deadline,Call[] calls)` | `DepositWallet` / `1` / the wallet | ordinary EIP-712 |

The EIP-191 step is the trap. Signing the EIP-712 digest directly produces a
well-formed signature that the relayer accepts and the wallet recovers to a
stranger; the failure appears on chain with nothing local to catch it. The
Safe's `v += 4` is how a Safe is told the digest was prefixed — leave it at 27
and the Safe recovers from the wrong hash.

A Safe performing several calls delegate-calls the multisend contract, which
changes the target, the calldata and the operation code, all three of which
are signed. A proxy needs no second contract: its factory takes an array of
calls directly, and its operation codes count from one because zero means
invalid, where a Safe's count from zero.

### Transactions sent directly

The `onchain` package is the other half of the relayer: the same effects, paid
for by the sender. It talks to an Ethereum JSON-RPC node, which Polymarket does
not run, so there is no default endpoint — `onchain.New` takes a URL.

Only EIP-1559 (type 2) transactions are built. The signature covers the type
byte and nine fields, in this order and no other:

```
keccak256(0x02 ‖ rlp([chainId, nonce, maxPriorityFeePerGas, maxFeePerGas,
                      gas, to, value, data, accessList]))
```

The broadcast form appends three more items to the same list —
`yParity, r, s` — and the transaction hash is keccak-256 over that. The access
list is always the empty list: it prepays for storage a call will touch, an
optimisation with no correct default, and it is part of the signature either
way.

Three traps, all pinned:

1. **`yParity` is not `v`.** Every other signature in this client carries
   Ethereum's `v` of 27 or 28, because a contract verifying one expects it. A
   typed transaction stores the parity bit alone, 0 or 1. Passing 27 through
   encodes a transaction that recovers to a stranger, and a node answers
   "invalid sender" without naming the cause.
2. **An empty recipient is the empty string, not the zero address.** RLP
   writes an integer and an absent value alike, with no leading zeros, so a
   contract creation and a transfer to `0x00…00` differ by one byte — and
   value sent to the zero address is gone.
3. **`s` must be canonical.** EIP-2 admits only the lower of the two
   equivalent values; a node silently drops the other. `SignTransaction`
   checks rather than trusting the `Signer`.

#### What a smart wallet can and cannot do from here

The deposit wallet looks like it should be drivable on chain — its address is
derivable, its batch is signed, and the batch encoding is public. It is not,
and every step of that was checked against Polygon rather than reasoned about:

| Call | Sent by | Answer |
|---|---|---|
| `factory.deploy(address[],bytes32[])` | anyone else | reverts `OnlyOperator()` |
| `factory.proxy(Batch[],bytes[])` | anyone else | reverts `OnlyOperator()` |
| `wallet.execute(Batch,bytes)` | anyone, any signature | reverts `OnlyFactory()` |
| `wallet.withdrawERC20(...)` | the owner | reverts `NotPaused()` |

So a deposit wallet is deployed and driven by Polymarket's relayer, and by
nothing else: `relayer.BuildWalletCreate` and `relayer.BuildWalletBatch` are
the whole path. What this package adds for one is reading — `Deployed`,
`WalletNonce`, `WalletOwner`, and `PredictDepositWallet`, which asks the
factory for the address that `polymarket.DeriveDepositWallet` computes
offline. Those two agree on Polygon today, for both the beacon era and the
pre-upgrade one; `examples/onchaincheck` is that comparison, runnable.

The account this package can act for is an EOA, and there the whole surface
works: approvals, splits, merges, redemptions, conversions.

#### Positions

Minting and burning outcome tokens is the conditional-token framework's, and
Polymarket's neg-risk adapter wraps it with shorter arguments:

| Operation | Framework | Adapter |
|---|---|---|
| mint a complete set | `splitPosition(address,bytes32,bytes32,uint256[],uint256)` | `splitPosition(bytes32,uint256)` |
| burn a complete set | `mergePositions(address,bytes32,bytes32,uint256[],uint256)` | `mergePositions(bytes32,uint256)` |
| claim after resolution | `redeemPositions(address,bytes32,bytes32,uint256[])` | `redeemPositions(bytes32,uint256[])` |
| convert no-positions | — | `convertPositions(bytes32,uint256,uint256)` |

The trailing arrays are the trap: the framework's redemption takes **index
sets**, the adapter's takes **amounts**. The calls look alike, take the same
Go types, and mean different things, so passing a partition to the adapter
redeems one unit and two units rather than everything. Both are pinned, and a
test asserts they cannot encode alike.

The parent collection id is always zero here. The framework allows a position
to be split again under a second condition; no Polymarket market does, so the
field is not a parameter.

Approvals are the reason most callers are here. An account trading with its own
key must approve every exchange contract twice — the collateral as an ERC-20
allowance, the outcome tokens as an ERC-1155 operator flag — before settlement
can move anything. `RequiredApprovals` lists them per chain and
`MissingApprovals` reads which are outstanding. A smart wallet needs none of
this: the relayer grants the same approvals as gasless calls made by the wallet
itself.

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

#### Where the secrets may live

Neither key has to be in this process. The wallet key is behind `Signer`
already; the API secret is behind `L2Authenticator`, which takes a request and
returns its five headers, so a KMS or a signing service can hold it. Three
things bound that:

| | Held by | Seam |
|---|---|---|
| wallet key | `Signer` / `TypedDataSigner` | sees the digest, or the whole payload |
| CLOB level-2 secret | `APICreds` or an `L2Authenticator` | signs one request at a time |
| relayer credentials | `APIKeyCredentials`, `BuilderCredentials` or a `relayer.Authenticator` | same shape, headers as a map |

`POLY_ADDRESS` names the wallet and not the credential, so a level-2 request
needs a `Signer` whatever holds the secret. And the websocket user channel
sends the secret itself in its subscribe frame — a protocol fact, not an
omission here — so there is nothing for an authenticator to protect on that
socket.

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
| wrapped contract-wallet order signatures (ERC-7739) | 3 |
| wallet derivations (proxy, Safe, deposit UUPS and beacon) | 18 |
| relayer meta-transactions (Safe single and batched, proxy, deposit batch of 0, 1 and 2 calls) | 6 |
| relayer calldata (multisend packing, proxy call arrays) | 3 |

A third file, `testdata/tx-vectors.json`, pins the transactions the `onchain`
package signs. Its generator, `testdata/gen-tx-vectors.mjs`, drives `viem`
rather than a Polymarket SDK, because a transaction's encoding is Ethereum's
and no Polymarket client produces one.

| Pinned | Count |
|---|---|
| EIP-1559 transactions: unsigned encoding, signing hash, signature, raw form and hash | 6 |
| — covering both chains, with and without calldata, with and without a recipient, at both signature parities | |
| token calldata (approve, allowance, both balances, both approval flags) | 8 |
| conditional-token and neg-risk calldata, with their id derivations | 15 |
| nested-tuple encodings (a batch of calls, each carrying bytes) | 3 |
| RLP items and lists, including the 55/56-byte boundary | 10 |

The relayer and wallet vectors come from a second generator,
`testdata/gen-relayer-vectors.mjs`, because they are the output of a
different SDK. The beacon deposit-wallet derivation is pinned against
`py-builder-relayer-client` rather than the TypeScript one — no npm release
of the latter exports it, and agreeing with an implementation written in
another language is the stronger check anyway.

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
| `typeddata.go` — the EIP-712 payload, `TypedDataSigner` | done, cross-checked against viem |
| `auth.go` — level 1 and level 2 | done, verified against production |
| `order.go` — build and sign, V1/V2/V3, limit and market | done, golden-pinned |
| `wallet.go` — the four account forms and their derivations | done, pinned against two official clients |
| `erc7739.go` — the wrapped order signature | done, golden-pinned |
| `session.go` — transport, retries, headers, errors | done |
| `ratelimit.go` — both published limiters | done |
| `clob/` — 69 methods | done |
| `gamma/` — 49 methods | done |
| `data/` — 15 methods | done |
| `bridge/` — 5 methods | done, reads live-verified |
| `relayer/` — 6 reads, 3 transaction builders, submit | done, signing golden-pinned |
| `ws/` — market, user, live data | done, live-verified |
| `internal/rlp` — the transaction encoding | done, golden-pinned |
| `internal/abi` — dynamic arguments, arrays, nested tuples | done, golden-pinned |
| `onchain/` — EIP-1559 signing, JSON-RPC, approvals, positions | done, golden-pinned and live-verified |

There is no public testnet. The official SDKs support chain 80002 (Amoy) with
a full set of contract addresses, but no Polymarket-hosted Amoy CLOB exists:
every candidate hostname resolves to nothing, the documentation never mentions
a testnet, and the SDKs' own examples point `host` at `localhost:8080` with no
public matching engine to run there. To test against something other than
production, point `WithHost` and `ws.WithURL` at your own server.

## Remaining risk

The signing core is retired: it reproduces the official SDK byte for byte. What
is left is breadth — one Go method per endpoint — plus two things no golden
vector can settle:

1. **Live trade acceptance.** No order has been placed. `POST /auth/api-key`
   costs nothing and proves level 1 end to end; a real order needs a funded
   wallet and an explicit decision.
2. **Response drift.** Field names are transcribed from live responses and the
   SDK's types. An endpoint nobody could observe live is marked as such.
3. **Live settlement.** `onchain` has been verified against Polygon as far as
   verification can go without spending: the derivations match the factory's
   own predictions, the reads answer from the live contracts, and a signed
   transaction offered to a public node was decoded, its sender recovered, and
   refused for having no balance — which is the answer that proves the
   encoding and the signature. What remains unproven is a transaction that
   actually lands: a split, a merge or an approval mined and settled. That
   needs a funded key and an explicit decision.

## Clean-room rule

Reference the TS SDK and `Polymarket/go-order-utils` for *how the protocol
works*. Never paste third-party source into this tree.

# go-polymarket — Design & Protocol Reference

A from-scratch Go client for the **Polymarket CLOB V2** API. **V2-only, EOA
signing** for the first milestones. Licensed **GPL-3.0-or-later**.

Every constant and layout below is transcribed from the public Polymarket
protocol (as exposed by the official `@polymarket/clob-client-v2` SDK). These
are protocol facts, not copied source — this is a clean-room implementation.

---

## Status

| Component | State |
|---|---|
| `types` — order/wire/protocol constants | ✅ done |
| `sign/hmac.go` — L2 request HMAC | ✅ done (stdlib) |
| `sign/eip712.go`, `sign/clobauth.go` — EIP-712 signing | ⛳ **M1, stubbed** |
| `order` — amount math + rounding + build | ✅ done (needs golden vectors) |
| `client` — read endpoints | ✅ done, live-verified |
| `client` — auth + PostOrder | wired, blocked on M1 signing |
| `ws` — websockets | M5 |

`go build ./...`, `go vet ./...`, `go test ./...` all pass. The
`examples/check-builder` command runs read-only against production today.

---

## Package layout

```
types/     order.go wire.go protocol.go   — types + all protocol constants
sign/      hmac.go clobauth.go eip712.go  — the 3 signing primitives
order/     rounding.go amounts.go build.go — price/size -> signed order
client/    client.go market.go trading.go — REST
examples/check-builder/                    — read-only demo (works now)
```

---

## Protocol reference (Polygon, chainId 137)

### Contracts (EIP-712 verifyingContract)
```
ExchangeV2         0xE111180000d2663C0091e4f400237545B87B996B   // normal markets
NegRiskExchangeV2  0xe2222d279d744050d28e00520010520000310F59   // neg-risk markets
```
Pick by the market's neg-risk flag (`GET /neg-risk?token_id=`).

### Order EIP-712 (the core signature)
Domain: `{name:"Polymarket CTF Exchange", version:"2", chainId:137, verifyingContract}`

Signed struct — **11 fields** (`types.OrderTypeString`):
```
Order(uint256 salt,address maker,address signer,uint256 tokenId,
      uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType,
      uint256 timestamp,bytes32 metadata,bytes32 builder)
```
`side`: BUY=0 SELL=1. `signatureType`: EOA=0. `builder`: your builder code
(bytes32), zero if none. `metadata`: zero.

**⚠️ Trap:** the wire JSON also carries `taker` (zero address) and
`expiration` ("0"), but **neither is part of the signed struct**. Sign only the
11 fields above.

Digest:
```
digest = keccak256(0x1901 ‖ domainSeparator ‖ hashStruct(Order))
sig    = secp256k1.Sign(digest, priv)   // 65 bytes r‖s‖v, v ∈ {27,28}
```

### Amount math (`order.GetRawAmounts`, verified by `amounts_test.go`)
Look up rounding by tick size (`types.RoundingByTickSize`), then:
```
rawPrice = roundNormal(price, cfg.Price)
BUY:  taker = roundDown(size, cfg.Size);  maker = taker*rawPrice   (pay USDC)
SELL: maker = roundDown(size, cfg.Size);  taker = maker*rawPrice   (pay shares)
// if a derived amount exceeds cfg.Amount decimals: roundUp at Amount+4, then
// roundDown to Amount.
makerAmount = int(maker * 1e6);  takerAmount = int(taker * 1e6)   // 6 decimals
```
All arithmetic uses `math/big.Rat` — never float64.

### Auth
**L1 — ClobAuth EIP-712** (create/derive API key):
```
domain = {name:"ClobAuthDomain", version:"1", chainId:137}   // no verifyingContract
type   = ClobAuth(address address,string timestamp,uint256 nonce,string message)
message= "This message attests that I control the given wallet"
```
Send as headers `POLY_ADDRESS/POLY_SIGNATURE/POLY_TIMESTAMP/POLY_NONCE` to
`POST /auth/api-key` (create) or `GET /auth/derive-api-key`. Response:
`{apiKey, secret, passphrase}`.

**L2 — request HMAC** (`sign.BuildHMAC`, done):
```
message = timestamp + method + requestPath + body   // requestPath includes query
mac     = HMAC_SHA256(base64url_decode(secret), message)
sig     = base64url(mac)
```
Headers: `POLY_ADDRESS/POLY_SIGNATURE/POLY_API_KEY/POLY_PASSPHRASE/POLY_TIMESTAMP`.

### POST /order wire body (`types.PostOrderRequest`)
```json
{
  "deferExec": false,
  "postOnly": false,
  "order": {
    "salt": 123, "maker": "0x..", "signer": "0x..", "taker": "0x00..00",
    "tokenId": "..", "makerAmount": "..", "takerAmount": "..",
    "side": "BUY", "signatureType": 0, "timestamp": "..", "expiration": "0",
    "metadata": "0x00..00", "builder": "0x..", "signature": "0x.."
  },
  "owner": "<L2 api key>",
  "orderType": "GTC"
}
```
The L2 HMAC signs `method+"/order"+body` where `body` is this exact marshalled
JSON. `salt` is an integer here (string everywhere else).

### Endpoints (see `types` EP* constants)
Read (no auth): `/book /tick-size /neg-risk /fee-rate /fees/builder-fees/{code}
/builder/trades /time`. Trade (L2): `/order /orders /data/order/ /data/orders`.
Auth (L1): `/auth/api-key /auth/derive-api-key`.

---

## Milestones

- **M1 — signing core (hardest, do first).** Implement `sign.OrderHash` /
  `SignOrder` and `BuildClobAuthSignature` with go-ethereum
  (`crypto`, `crypto/secp256k1`, keccak, abi packing). Add the private key
  `Signer` impl. **Golden test:** for fixed inputs, assert the order hash and
  signature are byte-identical to `@polymarket/clob-client-v2`. This retires
  the only real risk.
- **M2 — read REST.** Done (`client/market.go`), live-verified.
- **M3 — trade loop.** L1→creds→L2→`PostOrder` (GTC with builder code), cancel,
  query. Smoke against a funded test wallet with a tiny order.
- **M4 — builder attribution.** `GetBuilderTrades`, confirm fee accrual.
- **M5 — websockets.** `/ws/market`, `/ws/user` with reconnect.
- **M6 — later.** Market orders (FOK/FAK), POLY_PROXY/Safe/EIP-1271 wallets,
  Gamma, gasless relayer.

## Golden tests
Capture vectors from the TS SDK once (a tiny Node script that builds+signs a few
orders and dumps `{order, orderHash, signature}` to JSON), drop them in
`internal/testdata/`, and assert against them in `sign` and `order`. Same
inputs → identical bytes is the whole correctness story.

## Clean-room rule
Reference the TS SDK and `Polymarket/go-order-utils` for *how the protocol
works*. Do not paste any third-party Go source into this tree.

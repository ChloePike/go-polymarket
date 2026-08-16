# go-polymarket

A clean-room Go client for the **Polymarket CLOB V2** API. **V2-only, EOA**
to start. **GPL-3.0-or-later.**

Not affiliated with Polymarket. Protocol details are transcribed from the
public API / official TS SDK; no third-party client source is vendored.

## Status

Read-only endpoints work today. Trading is wired but blocked on the EIP-712
signing core (**M1**) — see [DESIGN.md](DESIGN.md).

```
go build ./...   # ok
go vet ./...     # ok
go test ./...    # ok  (order/ amount-math golden vectors pass)
```

## Try the read-only demo (no wallet, no keys)

```bash
go run ./examples/check-builder 0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417
# builder code : 0x11adfa…ae5417
# enabled      : true
# maker / taker: 0 / 0 bps
# attributed trades: 0
```

## Packages

| Package | Purpose |
|---|---|
| `types` | Order/wire types + all protocol constants (contracts, domains, endpoints, rounding) |
| `sign`  | L2 HMAC (done); order + ClobAuth EIP-712 (M1) |
| `order` | price/size → maker/taker integer amounts (big.Rat), order assembly |
| `client`| REST — read endpoints done; auth + PostOrder wired |

## Roadmap

M1 signing → M2 reads (done) → M3 trade loop → M4 builder attribution →
M5 websockets. Details and the full protocol reference: **[DESIGN.md](DESIGN.md)**.

## License

GPL-3.0-or-later — see [LICENSE](LICENSE).

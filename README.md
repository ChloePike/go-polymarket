In the `## Examples` section, add the livetennis line to the fenced list and reword the paragraph that follows it.

REPLACE this block:

```
go run ./examples/onchaincheck -rpc <node-url> # prove the on-chain layer against a real node
```

The first six run with no arguments and no credentials. The last two need a
Polygon JSON-RPC endpoint, because there is no default node to borrow.
```

WITH this block:

```
go run ./examples/onchaincheck -rpc <node-url> # prove the on-chain layer against a real node
go run ./examples/livetennis                   # a tennis market next to the live match state
```

The first six run with no arguments and no credentials. The two `-rpc`
examples need a Polygon JSON-RPC endpoint, because there is no default node to
borrow. `livetennis` reads Polymarket with no key, but pairs each market with
an external live-tennis data feed, so it needs a free Live Tennis API key in
`LIVETENNIS_API_KEY` (https://livetennisapi.com/subscribe/free).
```

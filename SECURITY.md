# Security

This library signs transactions that move money. Report anything in that path
privately.

## Reporting

Open a [private advisory][advisory]. Do not open a public issue, and do not
demonstrate a finding against somebody else's account.

[advisory]: https://github.com/ChloePike/go-polymarket/security/advisories/new

Useful to include, in rough order of usefulness: the smallest program that
shows it, what an attacker gains, and which commit you are on. Never include
a private key — not even one you consider spent.

## What counts

Anything that makes this client sign, send, or accept something other than
what its caller asked for:

- A signature over different values than the caller supplied, or over the
  wrong number of fields.
- An amount, price or size that reaches the wire rounded the wrong way, or
  through a `float64`.
- A key, an API secret or a passphrase reaching a log, an error string, a
  request that should not carry it, or the process's memory longer than it
  needs to.
- A response the client trusts more than it should: a header that stops it
  pacing, a body that redirects a request, a field that changes where funds
  go.
- Level-2 headers computed over the wrong bytes, or reused where they should
  not be.

Out of scope: what Polymarket's own API does — report that to Polymarket —
and anything requiring the attacker to already hold the private key.

## What this library does not do

Worth stating, because a reader may otherwise assume it:

- **It does not protect a key at rest.** `NewPrivateKey` takes hex and holds
  it in memory. If a key needs a boundary, implement `TypedDataSigner` and
  keep it in a hardware wallet, an HSM or an MPC service; the client will
  hand that signer the whole EIP-712 payload rather than an opaque hash, and
  will verify that whatever comes back recovers to the signing address.
- **It does not decide whether an order is sensible.** Size, price and
  exposure are the caller's.
- **It does not retry writes.** A submission whose fate is unknown is
  reported as unknown; see `Indeterminate`.

## Versions

Fixes go to the latest release. Until there is a `v1`, that is the only
supported line.

<!--
Title this pull request the way its commit is titled: a conventional-commit
prefix, then what changes, in the imperative.

    feat: pace requests against the published rate limits
    fix: make a rate-limit refund land on its own slot

CI checks the title. See CONTRIBUTING.md for the full list of prefixes.
-->

## What changes

<!-- One paragraph. What the code does now that it did not do before. -->

## Why

<!--
The reasoning a reader cannot recover from the diff. If this follows a
protocol rule, name the rule. If it fixes a bug, describe the failure, not
the patch.
-->

## How it was verified

<!--
Name the evidence. "The tests pass" is not evidence; which test, and what
would it have caught?

If nothing verifies this change, say so. That is a reviewable fact.
-->

## Checklist

- [ ] `gofmt -l .` prints nothing, and `go build ./...`, `go vet ./...`,
      `go test -race ./...` are all green.
- [ ] Every struct type this change introduces is declared at package level
      with a doc comment — test tables included.
- [ ] No `float64` carries an amount, a price or a size.
- [ ] Every exported identifier has a doc comment starting with its name.
- [ ] No third-party source was pasted into this tree, and every new file
      carries the SPDX header.

### If this touches signing or amount math

<!-- internal/eip712, signer.go, auth.go, typeddata.go, internal/amount, order.go -->

- [ ] A golden vector in `testdata/vectors.json` covers the change, and
      `testdata/gen-vectors.mjs` regenerates it.
- [ ] The signed `Order` still carries **eleven** fields. `taker` and
      `expiration` are on the wire and are not signed.
- [ ] Rounding directions are stated in a comment, with the reason.

### If this changes the public API

- [ ] `README.md` and `DESIGN.md` move in the same commit.
- [ ] Anything a caller must now do differently is spelled out above.

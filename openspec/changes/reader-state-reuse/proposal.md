# reader-state-reuse

## Why

After `reader-allocation-floor` removes the unsized-growth penalty, the
reader is still the only hot component in the codebase with no reuse path
for its own fixed per-call objects. `vm.VM` has `Reset()`
(`core/vm/vm.go:247`) and is checked out from a `sync.Pool`
(`runtime/eval.go:52,89`) — its stack (initial cap 256) and frame slice
(initial cap 64) keep their capacity across checkouts, and GoFunc arguments
are passed as a subslice of the operand stack (`vm.go:1785`) with zero
per-call allocation. `NewReaderWithFlags` (`core/reader.go:74`) and
`NewParserWithDepth` (`core/reader.go:303`) each heap-allocate a fresh object
on every call to `Dialect.ReadWithMaxDepthStats`
(`core/dialect.go:408-424`), which every `Eval` invokes (`runtime/eval.go:541`).
Neither type has a `Reset`. There is exactly one `sync.Pool` in the entire
repository.

Beyond the two fixed objects, `parseList`/`parseVector`/`parseReaderVector`
(`core/reader.go:424,444,508`) each build their element slice from
`var items []Value` with no sizing information, and `NewList`
(`core/types.go:213`) / `NewVector` (`:424`) retain the caller's slice by
reference — so any over-allocated capacity from append-growth is retained in
the AST forever, not just during parsing. This is why `reader-allocation-floor`
did not simply prealloc these slices: a naive prealloc guess would either
under-size (falling back to the same growth cost) or over-size (permanently
wasting AST memory). The correct fix is a pooled scratch region sized during
the build, with a right-sized final slice copied out once, which is squarely
a lifetime/reuse concern and belongs in this change rather than the prior one.

## What Changes

- A package-level `sync.Pool` in `core/` holding a combined
  `{Reader, Parser, tokens []token}` scratch object with a `Reset` method,
  checked out and returned by `Dialect.ReadWithMaxDepthStats`. Precedent:
  `vmPool` (`runtime/eval.go:52`) and `vm.Reset()` (`core/vm/vm.go:247`).
- Parser node slices (`items` in `parseList`/`parseVector`/
  `parseReaderVector`) build into pooled scratch storage during parsing, then
  copy once into an exactly-sized slice before being handed to
  `NewList`/`NewVector` — removing both the append-growth allocation chain
  and the retained over-capacity that blocked a naive prealloc in the prior
  change.
- Determinism and concurrency: pooled state resets fully on return (no stale
  `line`/`col`/`pos`/`depth`/`stats` leaking into the next checkout); `core/`
  stays stdlib-only (`sync` already is); a `-race` run with concurrent
  `Read` calls on the same pool must show no data race.

## Impact

- Affected specs: `core-engine` (new requirement: reader working state is
  reusable across calls without observable cross-call state leakage).
- Affected code: `core/reader.go` (`Reader`, `Parser`, `parseList`/
  `parseVector`/`parseReaderVector`), `core/dialect.go`
  (`ReadWithMaxDepthStats` checkout/return).
- **Retention safety is the whole review surface of this change.** `token.val`
  substrings alias the *source string*, not the pooled token buffer, so
  recycling the token buffer is sound on that count — this must be pinned by
  a test that reuses a pool slot across two `Read` calls and confirms the
  first call's returned values are unaffected, not merely argued in prose.
  The right-sized node-slice copy is what makes the AST-retention side safe:
  parser node slices must never be handed to `NewList`/`NewVector` directly
  from pooled scratch storage.
- Expected: fixed per-`Read` object allocations → 0 amortized;
  `GoldsetParse/*` a further −55 to −70% B/op cumulative against
  `reader-allocation-floor`'s post-implementation baseline.
- Sequencing: after `reader-allocation-floor` lands (this change's baseline is
  that change's result, not v0.10.0's).

## Why

Three places in the engine walk a `List` by position. Past
`listFlatThreshold` a list is a shared chain, so `At(i)` costs i steps and each
of those loops is quadratic in the form's length:

- `core/eval.go:705` — expanding a quasiquoted list
- `core/eval.go:721` — splicing a sequence into a quasiquoted list
- `core/dialect.go:511` — wrapping a multi-expression `cond` clause body in `do`

Measured on a quasiquoted list literal, `GOMAXPROCS=2`, `-benchtime=200ms`,
`-count=6`:

| n | before | after |
| --- | --- | --- |
| 16 (flat) | 634ns | 687ns |
| 64 | 3.96µs | 2.75µs (−30.5%) |
| 256 | 38.97µs | **9.89µs (−74.6%)** |

The shape is what identifies it as a walk rather than work: across those sizes
`allocs/op` grows linearly (8 → 74 → 268) while the old timings grew
quadratically. `B/op` and `allocs/op` are byte-identical before and after at
every size, so nothing about what gets built changed — only how it is reached.
The n=16 cell, below the threshold and therefore flat, does not move, which is
the control: it is the one size where `At(i)` was already O(1).

This is reachable from ordinary macro code. A macro whose template is a list of
more than 32 forms — a generated `do` block, a dispatch table, a long `cond` —
pays it on every expansion.

## What Changes

- The three loops iterate with the existing internal `listCursor` instead of
  indexing by position. Same elements, same order, same allocations.

Nothing else. No API change, no representation change, no new identifier.

## Capabilities

### Modified Capabilities

- `core-engine`: `Sequence representation efficiency` requires constant-time
  indexed reads of a `Vector` and says nothing about a `List`, which is correct
  — a chain cannot offer that. What it left unsaid is the obligation that
  follows for the engine: if positional reads are not constant-time, the engine
  must not traverse by position. It gains that statement plus a scenario, so the
  next such loop is a spec violation rather than a silently quadratic path.

## Impact

- Code: `core/eval.go` and `core/dialect.go`, plus two new benchmark cells.
- Risk: `listCursor.next()` returns `(Value, bool)` and the rewritten loops
  discard the bool, bounding themselves on `Len()` as before. That is safe only
  because cursor and length come from the same immutable list — `List` values
  are immutable and neither loop mutates its source. If either ever iterated a
  value that could change underneath it, the discarded bool would become a real
  hazard.
- Risk: `dialect.go`'s rewrite consumes the clause test from the same cursor
  before the body loop rather than re-reading `At(0)`. Element order is the
  correctness property; the crossval suite covers `cond` under both dialects.
- **Investigated and deliberately not changed: the `NewList`/`NewVector`
  asymmetry.** `NewList` promotes past the threshold while `NewVector` always
  stays flat, which the performance program had listed as debt to reconcile.
  It is not debt. Making `NewList` always flat moves a whole-list promotion onto
  the first `Cons`, which the sequence-extension bound forbids —
  `TestSequenceOperationsAgainstReference/boundary_length_1000` fails with
  `Cons: byte cost = 40040, want 40`. The two constructors face opposite
  obligations: `Cons` on a list must stay O(1), indexed reads on a vector must
  stay O(1). The asymmetry is how both are met, and `NewList` now says so in a
  comment.

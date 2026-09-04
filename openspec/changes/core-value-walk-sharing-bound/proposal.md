## Why

`core` shares structure by design. `List.Cons` on a shared-tail list returns
`ListShallowBytes(1)` whatever the list's length, and stdlib's `cons` charges
exactly that — so consing a list onto itself costs a constant number of ledger
bytes while doubling the number of references into the same nodes.

The value-tree walks do not know that. `boundedDeepBytes`, `boundedString`,
`constructionDepthExceeded` and `ValueNodeCount` all recurse per reference, as
trees rather than graphs, so a node reachable twice is visited twice. Their only
ceiling is `MaxStructuralDepth`, which bounds the depth of the descent and not
the number of nodes it visits: a wide, shallow, heavily shared structure never
trips it and is walked whole.

Measured on a ten-element list consed onto itself 26 times — nesting depth 27,
far under the 1024 limit, 1040 ledger bytes:

| Walk | Result |
| --- | --- |
| `ValueDeepBytes` | reports 24,159,191,024 against a 67,108,864 allocation ceiling |
| `String` | renders 1,476,395,007 characters in 14.69s |
| `CheckConstructionDepthWith` | 1.677s, doubling per cons (3.3µs, 132µs, 6.8ms, 563ms, 1.68s) |

None of the three checks a context, so none is interruptible: an expired
deadline, a cancelled context and an exhausted reduction budget are all
unobservable while a walk runs. Every one is reachable from plain script code
through `str`, `format`, `assert`, and the collection builders.

This predates `stdlib-builtin-resource-migration`. That change records the
defect rather than hiding it: the affected phases carry the `unbounded-tracked`
disposition and a proof ending `Owned by core-value-walk-sharing-bound.`, which
is this change. Retiring that disposition is how this change is known to be
complete.

## What Changes

- Make the value-tree walks sharing-aware, interruptible, or both, so that walk
  work is bounded by a quantity the allocation ledger actually bounds.
- Give the walks a context so cancellation, an expired evaluation deadline, and
  an exhausted reduction budget are observable while one runs.
- Retire the `unbounded-tracked` inventory disposition: every phase carrying it
  becomes budgeted or a bounded exception with a proof that survives sharing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: bound value-tree walk work, not only its depth.

## Impact

- `core/depth.go` (`boundedDeepBytes`, `boundedString`,
  `constructionDepthExceeded`, `nodeCount`), `core/metering.go`
  (`ValueDeepBytes`, `ValueNodeCount`), and every caller that renders or sizes
  an arbitrary `Value`: stdlib `str`, `format`, `assert`, the collection
  builders, and the CL adapters.
- `internal/inventory`: the `unbounded-tracked` disposition and the rows using
  it are removed once the walks are bounded.
- Memoizing shared nodes changes what `ValueDeepBytes` reports for a shared
  structure, which moves existing allocation charges and gold-set cells. That is
  a deliberate, measurable consequence and needs its own before/after evidence,
  not a silent re-baseline.

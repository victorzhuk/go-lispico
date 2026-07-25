## Why

`stdlib-plugin`'s `assoc charges constructed allocation deeply` already requires
the incremental rule, and one of its scenarios states it without qualification:

> **Chained assoc does not re-charge the shared map** — WHEN `assoc` is applied
> repeatedly to the result of the previous `assoc` in a loop, THEN the total
> charge SHALL grow with the values and nodes added, not with the accumulated map
> size per iteration.

The implementation does not satisfy it. `plugins/stdlib/collections.go:449`
charges `HashMapShallowBytes(result.Len())` on every call — precisely "the
accumulated map size per iteration" — and `plugins/stdlib/monotonic_test.go:16-21`
pins that behaviour on purpose, recording the exclusion in a Go comment:

> Making this linear would require giving `HashMap` a persistent representation,
> which is out of scope here.

That scenario and the requirement's incremental wording were introduced by
`persistent-sequence-structures` (`5255b25`), which delivered structural sharing
for `List` and `Vector` and generalized the charging rule to cover maps without
implementing it for them. The spec has promised this since; the code never did.
This change closes that gap rather than proposing a new optimization.

The charge is quadratic because the allocation genuinely is: `Assoc`'s large-form
branch does `maps.Copy` of all n entries per call (`core/types.go:792-797`), and
`Dissoc` rebuilds the map entry by entry (`:820-828`). So the charge cannot be
lowered on its own — it is honest today, and cutting it without making the copy
cheaper would falsify the ledger that exists to bound real allocation. The
representation has to change first; the charge correction is what makes the fix
observable.

It is observable as an outright failure, not as slowness. Measured on this
commit, a loop that `assoc`s distinct keys into an accumulator under default
limits dies between 1440 and 1450 keys, identically under both execution modes:

```
mode=eval n=1440  ok        mode=vm n=1440  ok
mode=eval n=1450  FAILED: ResourceLimitError: allocation limit 67108864 bytes exceeded
mode=vm   n=1450  FAILED: ResourceLimitError: allocation limit 67108864 bytes exceeded
```

That is a guest-reachable ceiling on a kernel whose security model is resource
metering, and it reaches the consumer as `CodeResourceLimit` — indistinguishable
from a genuine runaway. It is the same defect shape, and the same failure mode,
that `persistent-sequence-structures` removed from `cons`: an accumulation that
died at iteration 2895.

Two earlier changes deferred a persistent map — `hashmap-bulk-builder`
("deferred until a consumer needs incremental big-map building") and
`core-hashmap-compact` ("rule workloads are ≤8 keys, and the gold set has no
large-map churn cell. Revisit on evidence."). Both predate metering. They weighed
the HAMT as a speed optimization for a workload nobody had, and on that framing
they were right. Neither could weigh a hard functional ceiling, because the
ledger that creates it did not exist yet. That ceiling is the evidence they asked
for.

## What Changes

- The large form's storage becomes a persistent hash-array-mapped trie, so
  `Assoc` and `Dissoc` copy one root-to-leaf path instead of the whole map.
- `assoc`'s charge becomes incremental: the storage the call actually allocated
  plus the deep size of the inserted value, matching the rule `cons`/`conj`
  already follow. `dissoc` follows the same rule.
- `TestAssocMonotonic_ChargesPerCallHonestly` is retargeted from pinning the
  quadratic total to pinning the linear one, and its scope-exclusion comment is
  deleted rather than reworded — the exclusion is what this change removes.
- The small form (≤ `hashMapSmallLimit`) is untouched, as is promotion, iteration
  order, equality, printing, and every public signature.

`math/bits` is stdlib, so `core/`'s zero-import invariant holds. No new exported
identifiers.

## Capabilities

### Modified Capabilities

- `core-engine`: `Map representation efficiency` constrains the small form's
  allocation behaviour and requires promotion to be invisible, but says nothing
  about what a large-form update costs. That silence is what let an O(n) copy sit
  there unremarked while a sibling spec forbade its consequence. It gains an
  asymptotic bound on large-form updates and a determinism constraint on the key
  hash, plus scenarios pinning both against measurement rather than inspection.
- `stdlib-plugin`: `assoc charges constructed allocation deeply` keeps its
  existing requirement text — it is already correct and already describes the
  target behaviour. It gains one scenario fixing the ceiling in place, so that a
  regression to per-call whole-map charging fails as a stated limit rather than
  as a mystery `ResourceLimitError` at an arbitrary size.

## Impact

- Code: `core/types.go`, `plugins/stdlib/collections.go`, and their tests.
  `core/depth.go` reaches three internal helpers — `sortedEntries()` (`:141`),
  `eachRaw()` (`:205`), `getByHashKey()` (`:209`) — which keep their signatures,
  so the blast radius stops at the package boundary.
- Risk: **`Set` is the exposure, and it is on the path that is actually
  exercised.** All six large-form builders go through it — `hash-map`
  (`plugins/stdlib/collections.go:124`), `merge` (`:536`), `OpMakeMap`
  (`core/vm/vm.go:977`), map literals (`core/eval.go:457,788`), and
  `json/decode` (`plugins/json/plugin.go:122`). A path-copying `Set` would make
  every one of them slower to fix a path none of them take. The change therefore
  measures `Set` before choosing its implementation, and carries a builder path
  if the measurement says so — see `design.md`. This is the decision most likely
  to be got wrong by assuming.
- Risk: **the gold set cannot see this change.** Every map fixture is ≤3 keys
  (`merge-config`, `registry-fold`), so perfgate will report no movement either
  way. A green gate proves only absence of collateral damage; it is not evidence
  the change worked. That evidence is the ledger ceiling moving, plus two new
  benchmarks.
- Risk: collision handling is load-bearing, not decorative — a 32-bit hash
  consumed 5 bits at a time runs out after seven levels, and a trie that keeps
  descending past that loops forever. The control is a test building colliding
  keys by construction, not one hoping to meet a collision.
- Risk: a randomly seeded hash would break the determinism invariant silently —
  identical input would iterate differently across process restarts. `hash/maphash`
  is stdlib and tempting and wrong for this reason. Fixed-seed FNV-1a instead,
  with a test asserting independently built equal maps print identically.
- Risk: `TestHashMap_PromotionBoundary` inspects the unexported `m` field, and
  `TestAssocMonotonic_ChargesPerCallHonestly` asserts the exact quantity this
  change alters. Both must be edited here — the two tests guarding the behaviour
  are themselves in the diff, so a mistake in either is not caught by the thing
  that exists to catch it. Called out for review rather than folded in quietly.

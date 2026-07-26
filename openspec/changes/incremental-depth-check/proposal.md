## Why

`cons` and `conj` re-walk the whole collection they extend to check
construction depth, so a loop that accumulates *collections* is quadratic in
time while allocating linearly.

`persistent-sequence-structures` narrowed this once: the walk is skipped when
the added element is a scalar, since a scalar cannot deepen anything. That
carve-out left the collection case walking the full result on every call.
Measured under the VM, consing a one-element list onto an accumulator against
the scalar control:

| n | scalar element | collection element |
| --- | --- | --- |
| 200 | 67.6µs / 483 allocs | 168.8µs / 886 allocs |
| 400 | 85.3µs / 884 | 452.0µs / 1687 |
| 800 | **146.6µs** / 1684 | **1583.1µs** / 3289 |

Allocations grow linearly in both columns (~1.95× per doubling). Time in the
collection column grows 2.68× then 3.50× per doubling — approaching quadratic —
and at n=800 it is 10.8× the scalar case for 2× the allocations. The excess is
the depth walk, not the work.

The fix follows from what the carve-out already assumed. Placing element `e`
into a collection `C` gives `depth(C+e) = max(depth(C), 1 + depth(e))`, and
`depth(C)` was checked when `C` was built. So only the new element can push the
result past the limit, and the walk can be bounded by the element instead of by
the accumulated result.

| n | before | after |
| --- | --- | --- |
| 200 | 168.8µs | 82.6µs (−51%) |
| 400 | 452.0µs | 149.9µs (−67%) |
| 800 | 1583.1µs | **305.1µs (−81%)** |

Growth per doubling falls from 2.68×/3.50× to 1.81×/2.04×. Allocations are
unchanged, so only the walk moved.

This supersedes the performance program's planned approach for this defect —
caching `depth` and `deepBytes` on every node. That would have grown `listNode`
from 32 to 48 bytes, and `persistent-map-structure` established by measurement
that a 16-byte inline struct growth is visible to the gold set's byte check
(`+0.22% B/op` from exactly that, on a map). The inductive check costs no
memory at all.

## What Changes

- `core` gains `CheckNestedElementDepth`, which checks a value as an element
  one level inside a container. `CheckConstructionDepth` keeps its meaning;
  both route through one shared limit-resolution helper.
- `chargeConsResult` checks each newly added collection element with it,
  instead of re-walking the result once.
- Fresh builders (`list`, `vector`, `range`, `assoc`, `merge`, json decode) are
  untouched. They construct from unrelated values with no already-validated
  container to induct from, and keep the full walk.

## Capabilities

### Modified Capabilities

- `core-engine`: `Value construction is depth-bounded` states the bound but says
  nothing about what enforcing it may cost, which is how an O(result) check
  survived inside an O(1) operation. It gains a bound on the enforcement cost,
  a scenario pinning that accumulation stays linear, and a scenario pinning
  that escalating nesting through `cons`/`conj` still fails — the second is the
  one that makes the narrowing safe to state rather than merely asserted. The
  limit itself is unchanged: the same constructions are rejected, at the same
  depth.

## Impact

- Code: `core/depth.go`, `plugins/stdlib/collections.go`, one test.
- **Risk, and the reason this needs care: this is a resource limit, so a
  narrowing is a security question, not a performance one.** What the check no
  longer re-detects is a too-deep structure *already inside* the collection
  being extended. That can only exist if it was never validated — and the
  scalar carve-out already lets exactly such a value through `cons` untouched,
  so this does not open a hole, it makes `cons` behave the same way whatever
  the element's type. The escalating shapes, where the new element is the
  accumulator and each step nests one level deeper, are still rejected at the
  limit; `TestCollections_ConsDepthEscalationStillCaught` pins both `cons` and
  `conj`, and `TestCollections_ConstructionDepthLimitNotCatchable` continues to
  cover escalation through the fresh builder `list`.
- Risk: the induction is only as good as the claim that a collection was
  checked when built. In-repo constructors all check. A Go embedder calling
  `core.NewList` directly does not, which is a pre-existing property of the
  scalar carve-out rather than something introduced here — noted so it is on
  the record rather than rediscovered.

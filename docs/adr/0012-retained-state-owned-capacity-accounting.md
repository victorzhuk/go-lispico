---
status: accepted
---

# Retained state is charged by owned allocation identity

yagel ADR 0105 requires that routine child environments carry per-scope
retained-state capacity limits: "Retained state is charged by owned
allocation identity and actual backing capacity, not logical length. Each
Routine child environment caps at 32 MiB / 100,000 allocated-capacity
slots. Deleting entries releases unreachable children but not surviving
buckets / slots; only a metered atomic rebuild can replace and release
backing." go-lispico has no env capacity accounting today: even with
reduction and allocation metering, retained bindings accumulate unbounded
across evaluations.

Consumer reality shapes the API. yagel loads each rule via
`EvalWithBindings`, whose internal child env survives only through handler
closures — the embedder never holds a reference to it, so it cannot
rebuild or release that env. Handlers then mutate the shared root env.
Both persistent scopes need caps, a usage probe, and an embedder-reachable
handle.

## Decision

**Owned-capacity counters on every Env.** Each `Env` on the engine path
carries `retainedBytes` and `retainedSlots` counters plus inherited
`maxRetainedBytes` and `maxRetainedSlots` limits. VM per-call frame envs
carry the same counters — uniform accounting, no two-class env machinery.
Counters die with the env.

**Fixed table, shallow charging.** Binding a new name charges the
map-entry + cell + key-string cost from the existing fixed metering table
(`MeterEnvMapEntryBytes` + `MeterEnvCellBytes` +
`StringShallowBytes(len(name))`) plus the value's shallow size
(`ValueShallowBytes(val)`) and one slot. Rebinding through an existing
`Cell` is free. Reviving a tombstoned binding reuses its slot — no new
charge. Deleting a binding tombstones the slot without decrementing the
counter, matching the existing `Env.Delete` tombstone semantics that keep
backing.

**Fail-closed `ResourceLimitError`.** A write that would breach either
ceiling does not occur; prior bindings stay intact. The breach returns
`*core.LispicoError{Code: CodeResourceLimit}`, terminal and
non-catchable.

**`RetainedUsage()` probe.** `(*Env).RetainedUsage() (bytes, slots
int64)` reads the counters under the read lock. The embedder and the
meter integration use this for ledger settlement.

**`Rebuild()` for in-place compaction.** `(*Env).Rebuild()` compacts the
scope's local binding maps under the write lock: fresh backing maps holding
only live bindings, reusing existing live `*Cell` pointers (closures and VM
site caches stay correct), dropping tombstoned cells, recomputing the
capacity counters, and bumping the name-generation counter. Non-recursive —
child envs are untouched. This is the only path that releases dead backing.

**`LoadScope` for embedder scope ownership.** `Engine.LoadScope(ctx,
source, bindings) (Value, *Env, error)` has `EvalWithBindings` semantics
but returns the retained child env so the embedder owns its lifecycle:
`RetainedUsage` for ledger settlement, `Rebuild` for compaction, dropping
the reference for retirement. `EvalWithBindings` is unchanged.

## Fixed size table (env charging)

| Unit | Charge |
| --- | ---: |
| Env map entry overhead | 64 bytes |
| Cell overhead | 32 bytes |
| Key string | `len(utf8 bytes)` |
| Bound value | `ValueShallowBytes(val)` |
| Per-binding slot | 1 slot |

The table reuses the same constants as ADR 0011's allocation ledger
(`MeterEnvMapEntryBytes`, `MeterEnvCellBytes`,
`StringShallowBytes`, `ValueShallowBytes`) — no separate env-specific
metering vocabulary.

## Deviation: shallow charging vs deep identity tracing

ADR 0105 asks for closure captures and aliases "traced before mutation,
with exact transfer/release" — full owned-identity tracing across the
object graph. This change charges shallow backing per binding instead: a
closure stored into a persistent env charges its slot and header, not its
captured env's contents.

Those contents remain bounded by (a) the per-env caps applied at their
creation and (b) the per-evaluation allocation ceiling bounding what any
one evaluation can materialize.

Deep identity-traced transfer is deferred. Trigger: yagel measurement
showing material under-accounting on real rule workloads.

## Consequences

- Per-write overhead on hot `let` paths is one counter add on new-slot
  creation only, under the already-held env lock.
- `Rebuild` under the write lock pauses concurrent readers of that env
  for the copy duration; acceptable because it is embedder-invoked, rare,
  and bounded by live binding count.
- Env creation outside the engine path (`core.NewEnv`) carries zeroed
  config — uncapped, documented.
- Capacity limits are immutable per env, set at construction, and
  inherited down the parent chain. Code cannot raise its own ceilings.

## Considered options

- Deep identity tracing (ADR 0105 literal): rejected for now — adds
  allocation-graph-walking overhead on every binding write and requires a
  transfer/release protocol; the shallow + per-env-cap + per-eval-ceiling
  combination bounds retained state well enough for measured workloads.
- Session-wide retained ledger only (no per-env counters): rejected — the
  embedder cannot identify which scope is growing, and `Rebuild` needs a
  per-scope target.
- Wrapping envs in a `Scope` handle type: rejected — `RootEnv()` already
  exposes `*Env` as the scope currency; a wrapper would duplicate it.

## Why

yagel ADR 0105: "Retained state is charged by owned allocation identity and
actual backing capacity, not logical length. Each Routine child environment
caps at 32 MiB / 100,000 allocated-capacity slots. Deleting entries releases
unreachable children but not surviving buckets / slots; only a metered atomic
rebuild can replace and release backing." go-lispico has no `Env` capacity
accounting: even with reduction and allocation metering, retained bindings
accumulate unbounded across evaluations.

Consumer reality shapes the API: yagel loads each rule via
`EvalWithBindings`, whose internal child env survives only through handler
closures — the embedder never holds a reference to it, so it cannot rebuild
or release that env. Handlers then mutate the shared root env. Both
persistent scopes need caps, a usage probe, and an embedder-reachable
handle.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxRetainedBytesPerEnv int` and
  `MaxRetainedSlotsPerEnv int` (zero → defaults `32 * 1024 * 1024`,
  `100_000`).
- Every `core.Env` on the engine path carries an owned-capacity counter
  (bytes + slots), inherited configuration from its parent chain. Binding a
  new name charges the fixed-table backing cost (map slot + `Cell` + key
  string) plus the value's shallow size and one slot. Rebinding through an
  existing `Cell` charges nothing. Reviving a tombstoned binding reuses its
  slot — no new charge.
- Charging is shallow by decision: values bound into an env charge their
  shallow size only. See "Deviations from yagel ADR 0105" below.
- Deleting a binding tombstones the slot without decrementing the counter
  (the existing `Env.Delete` tombstone semantics already keep the backing —
  this change makes the accounting match reality).
- New `(*Env).RetainedUsage() (bytes, slots int64)` — the usage probe the
  embedder and the meter integration read.
- New `(*Env).Rebuild()` — in place, same `*Env` identity: fresh backing
  maps holding only live bindings, reusing the existing live `*Cell`
  pointers (holders of cells and VM site caches stay correct), dropping
  tombstoned cells, recomputing the capacity counters, bumping the
  name-generation counter, under the env write lock. Non-recursive — child
  envs are untouched. This is the only path that releases dead backing.
- New `Engine.LoadScope(ctx, source string, bindings map[string]core.Value)
  (core.Value, *core.Env, error)` — `EvalWithBindings` semantics, but
  returns the retained child env so the embedder owns its lifecycle:
  `RetainedUsage` for ledger settlement, `Rebuild` for compaction, dropping
  the reference (plus meter release, see `meter-leases-and-session-ledgers`)
  for retirement. `EvalWithBindings` is unchanged.
- VM per-call frame envs (`needsCallEnv` path) carry the same counters —
  uniform accounting, no two-class env machinery; they are transient, so
  their counters die with them.
- Introduces ADR 0012.

## Deviations from yagel ADR 0105

ADR 0105 asks for closure captures and aliases "traced before mutation, with
exact transfer/release" — full owned-identity tracing across the object
graph. This change charges shallow backing per binding instead: a closure
stored into a persistent env charges its slot and header, not its captured
env's contents. Those contents remain bounded by (a) the per-env caps applied
at their creation and (b) the per-evaluation 64 MiB allocation ceiling
bounding what any one evaluation can materialize. Deep identity-traced
transfer is deferred; trigger: yagel measurement showing material
under-accounting on real rule workloads.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new `ResourceLimits` fields + new requirement `Retained
  state is charged by owned capacity` (includes `LoadScope`,
  `RetainedUsage`, `Rebuild`).
- `core-engine`: new requirement `Env owned-capacity accounting`.

## Impact

- Depends on: `engine-reduction-and-allocation-metering` (shared fixed size
  table), `eval-noncatchable-terminal-errors` (terminal breach errors).
- Code: `core/env.go` (counters, `RetainedUsage`, `Rebuild`),
  `core/eval.go` (charge on new binding in `def` / `let` / `fn` / `defn` /
  `defmacro` / `set!`-creating paths), `runtime/engine.go` +
  `runtime/eval.go` (limits threading, `LoadScope`), `core/vm/vm.go`
  (frame-env counter).
- Envs created outside the engine path (bare `core.NewEnv`) carry zeroed
  config — uncapped, documented.
- yagel: `LoadScope` replaces its rule-load call one-for-one and closes the
  0105 "exact release on stop" requirement embedder-side.

# Design — retained-state-owned-capacity-accounting

## Decisions

- D1: Ownership stays with the `Env`; capturing a `Lambda` neither transfers
  nor double-counts. Releasing dead-bucket capacity requires explicit
  `Rebuild`. Matches yagel ADR 0105 "only a metered atomic rebuild can
  replace and release backing".
- D2: `Rebuild` mutates in place — same `*Env` identity, fresh maps, live
  `*Cell` pointers preserved. Rationale: closures and VM site caches hold
  `*Env` and `*Cell` by pointer; swapping in a new `Env` would strand every
  holder on stale state, and minting new `Cell`s would let site caches serve
  stale live values (the exact hazard `Delete`'s tombstone comment
  documents). Dropping only tombstoned cells plus bumping `NameGen`
  invalidates site entries that cached now-dropped cells while keeping live
  resolutions intact. Non-recursive: children are independent owners.
- D3: Shallow charging (user decision): slot + `Cell` + key + shallow value
  header from the change-1 fixed size table. Deep identity tracing is a
  recorded deviation with a trigger, not silent scope creep.
- D4: Charge on binding write, not read; rebind through an existing cell is
  free; tombstone revival is free (slot already owned). A read through a
  tombstone stays free.
- D5: Uniform counters on every engine-path env, including transient VM
  frame envs. One code path, no env classification; transient counters cost
  one add per new slot under the already-held env lock and vanish with the
  env. Cross-scope (meter) retained settlement reads only persistent envs at
  eval end — defined in `meter-leases-and-session-ledgers`.
- D6: `LoadScope` returns the raw `*core.Env` rather than a wrapper type:
  `RootEnv()` already exposes `*core.Env` as the scope currency, the
  embedder's supervisor owns lifecycle anyway, and a `Scope` wrapper would
  duplicate it (rejected: scope handle type).
- D7: Breach raises `CodeResourceLimit` (terminal); the write that would
  breach does not occur; prior bindings stay intact.

## Risks / Trade-offs

- Per-write overhead on hot `let` paths: one counter add on new-slot creation
  only, under the existing lock — goldset gate verifies non-increase.
- Shallow charging under-counts captured-env contents (documented deviation;
  bounded by per-env caps + per-eval allocation ceiling).
- `Rebuild` under the write lock pauses concurrent readers of that env for
  the copy duration; acceptable — embedder-invoked, rare, bounded by live
  binding count.

## Migration Plan

1. Add capacity counters + config inheritance to `Env`; thread limits from
   `engineConfig` at root creation.
2. Charge on new-slot write; red tests for slot/byte ceilings, rebind-free,
   tombstone semantics; race test.
3. `RetainedUsage`, then `Rebuild` with cell-identity and NameGen tests.
4. `LoadScope` in runtime; VM frame-env counter.
5. Goldset gate + existing allocation assertions.

## Open Questions

None blocking. (Resolved from the earlier draft: `Rebuild` does NOT recurse
into children; the embedder rebuilds scopes it owns individually.)

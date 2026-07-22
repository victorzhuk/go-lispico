## 1. Behavior contracts

- [x] 1.1 Red tests: slot ceiling, byte ceiling, rebind-free, tombstone
  revival free, delete-doesn't-release, rebuild-releases,
  closure-no-double-count (per scenarios). Characterization coverage records
  the unchanged baseline.
- [x] 1.2 Cell-identity tests: a `*Cell` held before `Rebuild` still serves
  the live binding after; a VM site-cache resolution survives `Rebuild` of
  live bindings and observes dropped ones as unbound (NameGen bump).
- [x] 1.3 Race test: concurrent binding writes to one child env, and
  `Rebuild` racing readers, under `-race`.

## 2. Implementation

- [x] 2.1 Add `MaxRetainedBytesPerEnv`, `MaxRetainedSlotsPerEnv` to
  `runtime.ResourceLimits`; defaults 32 MiB / 100,000.
- [x] 2.2 `Env` capacity counters + limit config inherited via `Child` /
  `NewEnv` on the engine path; bare `NewEnv` outside the engine stays
  uncapped (documented).
- [x] 2.3 Charge on new-slot write in `Set` / `SetFunc` (fixed table: map
  slot + `Cell` + key + shallow value); no charge on rebind or tombstone
  revival; breach → `CodeResourceLimit`, write not applied.
- [x] 2.4 `Delete` keeps its tombstone; counters unchanged (characterization
  only — behavior already matches).
- [x] 2.5 `(*Env).RetainedUsage() (bytes, slots int64)`.
- [x] 2.6 `(*Env).Rebuild()`: in place, fresh maps, live `*Cell` pointers
  reused, tombstones dropped, counters recomputed, `NameGen` bumped, write
  lock held, non-recursive.
- [x] 2.7 `Engine.LoadScope(ctx, source, bindings) (core.Value, *core.Env,
  error)` in runtime; shares the `EvalWithBindings` implementation.
- [x] 2.8 VM per-call frame env (`needsCallEnv` path) uses the same counter
  path.

## 3. Integration

- [x] 3.1 `go test ./... -race`.
- [x] 3.2 Existing allocation assertions (HashMap / Env alloc tests) still
  hold; `GOLDSET_MODE=vm` goldset gate non-increasing.

## 4. Verification

- [x] 4.1 `openspec validate --strict retained-state-owned-capacity-accounting`.

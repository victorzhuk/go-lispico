## 1. Behavior contracts

- [x] 1.1 Red test: a `GoFunc` that panics, invoked via `Engine.Eval` on the
  bytecode VM and on the tree-walker, returns a `*core.LispicoError` (Code
  `PanicError`) and does NOT abort the process; the panic value is preserved in
  the error.
- [x] 1.2 Red test: same panicking `GoFunc` via `Engine.Call` and via
  `Fn.Call` returns the typed error, not a process abort.
- [x] 1.3 Red test: after a recovered panic, the engine stays usable — a
  subsequent `Eval`/`Call` on the same engine (and the same `Fn`) succeeds,
  proving the pooled VM was not returned corrupted.
- [x] 1.4 Characterization: a normal error return and a recovered panic are
  indistinguishable to the embedder for stats (`EngineStats` eval count/error)
  and `OnEval` callbacks.

## 2. Implementation

- [x] 2.1 `Engine.Call`: replace `defer vmPool.Put(v)` with a deferred closure
  that recovers, sets `core.NewPanicError(name, r)`, `v.Reset()`, then `Put`.
- [x] 2.2 `Fn.Call`: same closure, mirroring `PinnedFn.Call`
  (`runtime/func.go:180-193`).
- [x] 2.3 `Engine.Eval`: top-level deferred recover converting a panic to
  `core.NewPanicError(source, r)`, recording the failed eval in
  stats/callbacks on the same path as a returned error.
- [x] 2.4 Confirm `bytecodeEvaluator.Apply`'s non-deferred `vmPool.Put` needs
  no change (panic skips `Put`, VM dropped) — assert with a test that pool
  reuse after an `Apply` panic yields a clean VM.

## 3. Integration

- [x] 3.1 `go test ./... -race` green; no goroutine/pool leak introduced
  (existing `TestWatchStop_NoGoroutineLeak` style still holds).
- [x] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — the recover `defer`
  is off the allocation hot path (deferred closure only on the boundary call,
  not per instruction).

## 4. Verification

- [x] 4.1 `openspec validate --strict engine-panic-boundary-recovery`.
- [x] 4.2 Update CHANGELOG `[Unreleased]` and, if the never-panics wording in
  `ARCHITECTURE.md`/CLAUDE.md needs it, note the boundary is now enforced on
  all public entry points.

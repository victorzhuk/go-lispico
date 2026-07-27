# engine-lean-call-boundary

## Why

`Engine.Call` costs ~178ns on the reference harness against GopherLua's ~97ns
protected call on the same box (2026-07-27), while the raw VM apply underneath
is ~98ns — the engine boundary adds ~75-80ns per call. The profile itemizes
it: RWMutex RLock/RUnlock 10.3% flat (engine `e.mu.RLock` for `rootEnv` +
`Env.ReadCell`'s lock, both per call), `VM.Reset` 5.5%, two nested
recover-defers plus the callback/stats defer ~5%, `sync.Pool` round-trip,
double `HasEvalState`/`HasEvalMeter` ctx probes (`Call` → `callBoundary` →
`applyOnVM` re-derive the same facts), and the per-call `counter.Add`.

Every component is there for a real contract — panic recovery, stats,
callbacks, meters, deadline ownership — but none of those contracts requires
paying for the *general* case on every call: a bytecode engine with no meter
attached, no callbacks registered, and a context carrying no evaluation
state is the overwhelmingly common yagel shape, and it is statically
knowable at call entry from engine state alone.

## What Changes

- A precomputed fast-path condition on the engine, updated at the few places
  it can change (callback registration, meter configuration, evaluator
  selection): when it holds and the entry context carries no evaluation
  state, `Call`/`Fn.Call` take a lean boundary: one recover-defer covering
  the whole call, no `StartEval`/`FinishEval`, no clock reads, single
  resolution of the cached callee cell through a lock-free versioned
  snapshot read (extending the versioned-read mechanism the VM sites
  already use to the engine call cache — `ReadCell`'s per-call `RLock`
  goes away on the hit path), engine-root read without the engine mutex
  (the root env pointer becomes an atomic snapshot), and the stats counter
  as the single remaining atomic per call.
- VM acquisition: a per-engine claimable VM slot (atomic CAS) with
  `sync.Pool` fallback under contention, so the steady single-goroutine
  caller skips the pool round-trip and the full `Reset` (pairing with
  vm-call-frame-fast-path's reset split). In-repo precedent:
  `PinnedFn.Call` already guards its private VM with a CAS `inUse` flag and
  reuses via `ResetIncremental` (runtime/func.go:179-217) — this lifts the
  same shape to the shared `Call`/`Fn.Call` paths.
- The general path — meters, callbacks, eval-state contexts, tree-walker
  engines — is byte-for-byte today's path.
- `Stats()` accuracy, panic recovery at every public entry point, undefined-
  name reporting, and Lisp-2 resolution order are unchanged contracts on
  both paths.

## Impact

- Affected specs: `runtime-api` (Boundary call efficiency — per-call cost
  posture; Named boundary calls amortize resolution — lock-free hit path).
- Affected code: `runtime/eval.go` (`Call`, `callBoundary`, `applyOnVM`),
  `runtime/engine.go` (fast-flag maintenance, root snapshot), `core/env.go`
  (versioned cell read already exists — reuse), handle paths.
- Expected: Call 178 → ≤110ns before vm-call-frame-fast-path, ≤95ns with
  it (beats GopherLua's 97ns local); Callback and Rule inherit the same
  boundary cut; per-call allocations unchanged (already at
  argument/result representation).
- Risk: flag staleness (a callback registered mid-flight must be observed —
  the flag is read once per call from an atomic; registration takes the
  engine lock and stores the flag, so the next call sees it); the CAS slot
  must never double-lease a VM (`-race` + a concurrent-Call test pin it).

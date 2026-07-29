# vm-func-site-cache

## Why

Lisp-2 head resolution bypasses the chunk site cache entirely. `OpGetFunc`,
`OpFreezeNativeFunc`, and the fused-op `fo.Func` branch call
`env.GetFuncCanonical` directly (core/vm/vm.go:1045, 1053, 1388) — an RWMutex
read lock plus a string-keyed map walk on every single head resolution, while
the value namespace serves the identical shape from a versioned site snapshot
at three atomic loads (`resolveGlobalValue`, core/vm/vm.go:1521).

CL is the shipped default dialect, so every consumer function call pays this.
On the 2026-07-29 fib(15) CL profile (post lean-boundary merge, this box):
`GetFuncCanonical` is ~50% cumulative — `mapaccess2_faststr` 12.3% cum,
`aeshashbody` 2.7%, `memeqbody` 2.6%, and the dominant share of the 34% flat
`atomic.Int32.Add` block, which is the RWMutex RLock/RUnlock pairs. A fib
recursion resolves six function-namespace heads (`<`, `+`, two `fib`, two
`-`); none of them changed since the previous instruction executed the same
site.

The rejected `vm-global-call-inline-cache`
(openspec/changes/archive/2026-07-29-*/design.md) does not cover this: it
fused dispatch for the *value* namespace, whose reads were already
site-cached, and lost to its own dispatch restructure. Here the baseline has
no cache at all, and the mechanism being extended is the proven in-loop
`resolveGlobalValue` shape, not a new dispatch form.

## What Changes

- `buildSites` additionally scans `OpGetFunc` and `OpFreezeNativeFunc`,
  assigning entries keyed per (constant index, namespace) — the same symbol
  read in both namespaces in one chunk gets distinct entries. Site tables
  stay chunk-owned, rebuilt by `CopyTreeFreshSites`, built only via
  `EnsureSites` (run-once chunks keep paying nothing).
- A `resolveFuncValue(site, env, sym)` resolver mirroring
  `resolveGlobalValue`'s decision tree exactly: serve on
  {env identity, NameGen, cell version} match; version mismatch → locked
  `ReadCell` of the remembered cell without republish; NameGen mismatch →
  full re-resolve and republish; publish only live cells owned by the root
  env's function map; nil site or non-root env → today's
  `GetFuncCanonical` walk.
- A locked local probe on Env for the function map (`FuncCellLocal`,
  mirroring `CellLocal`): no lazy-layer consult, no parent walk — the miss
  path still goes through `GetFuncCanonical`, which owns materialization.
- The three call sites swap the direct `GetFuncCanonical` call for the
  resolver. No new opcodes, no dispatch restructure, no two-phase anything —
  hard constraint carried from the inline-cache rejection.

## Impact

- Affected specs: `bytecode-vm` (Resolved global bindings — extended to the
  function namespace).
- Affected code: `core/vm/chunk.go` (buildSites, site keying),
  `core/vm/vm.go` (resolver + three call sites), `core/env.go`
  (`FuncCellLocal`).
- Expected: FibonacciCL −20% or better (acceptance floor −10%, else reject
  per the inline-cache precedent); every CL-dialect call-dense shape
  improves; goldset (Clojure dialect) and Accumulate rows are untouched
  controls and must stay flat.
- Risk: stale function resolution on defun rebind / canonical flip /
  tombstone+Rebuild+rebind — the invalidation enumeration in design.md is
  the correctness argument; each event is pinned by a test. Dispatch-loop
  codegen sensitivity (the Accumulate regression from the same-chunk frame
  sync, 2026-07-29) — guarded by interleaved control rows.

# Design — vm-global-call-inline-cache (REJECTED at perf gate)

Status: **rejected after full implementation**. The change was built, verified
semantically, and then failed its own acceptance gate (fib −4%) by a wide
margin. Archived with `--skip-specs`; main specs unchanged. This document
records the negative result so a future attempt starts from evidence, not
from the same hypothesis.

## Hypothesis

Each fib recursion resolves the global `fib` twice through
`resolveGlobalValue` (6.3% flat / 11.0% cum CPU at baseline) and dispatches a
generic `CALL`. Caching the callee per site behind the existing versioned
snapshot should save both. Expected: fib −4-8%.

## What was built (and measured)

Two-phase fused opcode `OpFusedGlobalCall`: a head-phase instruction freezes
the callee into `VM.globalCallStack` before argument evaluation (preserving
head-freeze order), a finish-phase instruction pops the record and, on a
snapshot hit with `*Closure`, enters the closure frame directly
(`enterClosure`) — skipping the value-stack callee push and the apply type
switch. Misses fall through to the exact unfused path. Guard = existing
`{env, gen, ver}` site snapshot. Hit rate on fib: **99.9%** (1971/1973).

Semantics were fully pinned and green: 6 crossval pins (redefinition,
head-freeze order, tombstone→redefine, non-closure fallback, emission,
value/function namespace separation), `TestVMVsTreeWalker`, chunk validation,
hot-reload/`UnloadPlugin` fused-site tests, `-race`.

## Gate results (same-session interleaved A/B vs master, GOMAXPROCS=24, n=10)

| bench | master | branch | Δ | p |
|---|---|---|---|---|
| Fibonacci_VM (gate ≤ −4%) | 322.7µ | 336.1µ | **+4.15%** | 0.023 |
| FunctionCall_VM | 6.45µ | 6.70µ | +3.82% | 0.000 |
| TailCall_VM | 248.4µ | 242.8µ | −2.25% | 0.000 |
| goldset-vm geomean | 9.02µ | 8.99µ | −0.26% | — |

Call-dense goldset rows regressed (counter-closure +4.74%, queue-promote
+2.13%); eval-mode control (untouched code) moved −1.65%, so the VM
regressions are real, not drift. Two optimization rounds (head phase inlined
into the dispatch switch, lazy symbol assertion, deduplicated arity check,
inline scratch for the freeze stack) recovered ~40% of the initial +6.88%
regression but never flipped the sign.

## Root cause

The premise failed, not the implementation. On this VM the generic call path
was already cheap: `resolveGlobalValue` serves hits from the same site
snapshot the IC would consult, and `vm.call`'s type switch plus one
value-stack callee slot cost little. The two-phase freeze mechanism the
correct semantics require (head freeze before argument evaluation) adds a
second fused instruction, a 4-word record push/pop, and finish-phase
match/dispatch overhead — more than the residual it removes (~6ns/call on a
~90ns/call path). CPython/LuaJIT-style IC wins presuppose a far more
expensive generic lookup+call baseline; that baseline does not exist here.

## Incidental findings (durable knowledge)

- **Site-table namespace invariant (pre-existing):** before this change,
  chunk site caches held *value-cell* resolutions only; Lisp-2
  function-position reads (`OpGetFunc`, `OpFreezeNativeFunc`, `OpFusedNativeOp`
  with `Func`) bypass sites entirely. Any future cache that publishes
  function-cell resolutions into sites MUST key sites by `{symbol,
  namespace}` — a shared per-symbol site serves the wrong binding in both
  directions (value read gets function binding; call head gets value
  binding). The implementation here had exactly that bug, caught by review
  and fixed with namespace-keyed sites + a two-order crossval pin.
- **Guard audit conclusion:** every mutation path that can change what a
  global site resolves to already bumps the cell mutation version or
  invalidates the site — `Set`/`SetBoth` (env.go:381/307/314),
  `ReplaceCellWithContext` (:410), `SetFuncWithContext` (:650),
  `SetFuncCanonicalWithContext` (:670-688), `Delete` tombstones (:821/826),
  `Rebuild` (:889-894, via NameGen), `applyMergePlan` (:1033),
  `removePluginBindings`/`restoreRootEnv`/`ReloadPlugin`/`UnloadPlugin`
  (runtime/plugin.go). No invalidation hole exists for cell-version-guarded
  caches; the NameGen func-cell gap is safely covered by cell versions.
- **Bench-hygiene:** cross-session benchstat on this box carries ±5-10%
  machine drift; eval-mode rows (untouched code) are the drift control, and
  same-session interleaved A/B is the only trustworthy comparison shape.

## What would have to be true for a retry

- A freeze mechanism cheaper than the value-stack push it replaces (none
  found; the freeze is semantically mandatory where arguments can rebind the
  callee), or
- compiler-provable non-rebinding argument shapes (constants/locals/lambdas
  only) allowing a single-instruction encoding — does not cover fib (args
  are call forms), or
- the generic `CALL` path becoming expensive first (e.g. before
  `vm-register-dispatch` lands) — re-evaluate ordering against the
  s2-register-dispatch epic, whose dispatch redesign may change this
  calculus either way.

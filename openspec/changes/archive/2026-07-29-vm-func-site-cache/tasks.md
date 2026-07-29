# Tasks — vm-func-site-cache

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`, `-benchmem`):
      FibonacciCL, CallBytecodeCanonical, CallBytecode/Plain, goldset both
      modes, Accumulate1000_Bytecode. Profile shares on fib CL:
      `GetFuncCanonical` ~50% cum, `mapaccess2_faststr` 12.3% cum, RWMutex
      `Int32.Add` share of 34% flat (2026-07-29, post lean-boundary merge).
      Baseline captured from a master-built binary and held for the
      interleaved A/B in 6.2.

## 2. Core API

- [x] 2.1 `Env.FuncCellLocal(name)`: locked local probe of `e.funcs`, no
      parent walk. Already exists at core/env.go:565-581, mirroring
      `CellLocal` including its one `LookupAndMaterialize` consult plus
      re-probe. The layer installs into the same env before returning
      true, so the re-probe yields a root-env-local live cell and
      publishing it is exactly what the value namespace already does. No
      new primitive; reuse as-is.
- [x] 2.2 Confirm `ReadCell`/`ReadCellSnapshot` are cell-generic (they
      lock the receiver env, not a namespace map) and behave identically
      for function cells. Confirmed: both take a `*Cell` and lock only the
      receiver env (core/env.go:501, :509); the func-namespace pins in 5.x
      exercise them against function cells.

## 3. Site table

- [x] 3.1 Extend `buildSites` to scan `OpGetFunc` and `OpFreezeNativeFunc`,
      deduplicating entries per (constant index, namespace). A chunk
      reading the same symbol in both namespaces gets two entries; prove it
      with a compiled-form test whose two namespaces hold DIFFERENT values
      (equal values would pass under the collision bug).
- [x] 3.1a Remove the `c.Fused[a].Func` skip at core/vm/chunk.go:212 and
      route those instructions into the function-keyed bucket. This is
      required, not optional: `fuseNativeOp` sets `Func: dialect.IsLisp2()`
      (core/compiler/compiler.go:726), so under CL every two-operand
      arithmetic/comparison head in fib is a `Func=true` fused op that
      today gets `idx[ip] == -1` and no site at all. Update the
      `buildSites` doc comment, which currently states the opposite
      contract.
- [x] 3.2 `CopyTreeFreshSites`/`EnsureSites` inheritance: fresh per-engine
      tables include the func entries; run-once chunks still build no
      table; nil-site resolution falls back to the walk.

## 4. Resolver

- [x] 4.1 `resolveFuncValue(site, env, sym)` mirroring
      `resolveGlobalValue`'s decision tree exactly: serve on
      {env, gen, ver} match; ver mismatch → locked `ReadCell` of the
      remembered cell, no republish; gen mismatch with matching ver →
      re-resolve and republish; publish only live root-env-local func
      cells; everything else → `GetFuncCanonical`. Verified by normalized
      textual diff against `resolveGlobalValue`: identical structure
      modulo the documented substitutions.
- [x] 4.2 Swap the three call sites (`OpGetFunc` core/vm/vm.go:1045,
      `OpFreezeNativeFunc` :1053, `dispatchFusedNativeOp` `fo.Func`
      branch :1388). No new opcodes, no added branches in other dispatch
      cases — diff of the `run` switch outside these cases must be empty.

## 5. Invalidation pins

- [x] 5.1 Defun rebind: warmed site, `defun` rebinds the head (canonical →
      non-canonical) → next execution calls the new definition; fused-op
      site falls back to real call semantics. Both dialect-level (CL) and
      direct `SetFunc`.
- [x] 5.2 Canonical re-mark: `SetFuncCanonical` after a rebind (the
      `engine.Use`/applyVocabulary revert shape) → warmed site observes the
      flip; no stale canonical flag in either direction.
- [x] 5.3 Tombstone paths: enumerate every operation that nulls a function
      cell (delete, plugin unload, hot-reload) and pin: warmed site →
      tombstone → undefined error identical to the uncached read.
- [x] 5.4 Rebuild compaction: warm site → tombstone → `Rebuild` (cell
      dropped, NameGen bumped) → fresh `SetFunc` of the same name → next
      execution resolves the new cell. This is the NameGen-gap sequence;
      it must pass without any new NameGen bump on func-cell creation.
- [x] 5.5 Never publish non-local: a name resolved through a parent env or
      the lazy layer is served correctly and its site entry stays empty
      (publish requires a root-env-local live cell).
- [x] 5.6 Shared chunk across engines: `CopyTreeFreshSites` isolation — two
      engines, same source chunk, different `fib` definitions, no
      cross-engine leakage.
- [x] 5.7 Concurrent `SetFunc` storm while executing chunks reading the
      same head on one engine: old-or-new value only, `-race` green.
- [x] 5.8 Crossval: the Lisp-2 fixtures (fib shape, rebind-mid-run shape)
      agree with the tree-walker before and after warming.

## 6. Verify

- [x] 6.1 Full floor: build, vet, lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.
- [x] 6.2 Interleaved benchstat vs 1.1 using `runtime.BenchmarkEngine_FibonacciCL`
      (the existing CL-dialect harness; the core/vm fib benchmarks are
      Lisp-1 and will not move — flatness there is the control, not a
      failure): FibonacciCL −10% or better — hard gate, reject the change
      below it (inline-cache precedent);
      CallBytecodeCanonical improved or flat; controls flat: goldset both
      modes (Clojure dialect — untouched path), Accumulate1000_Bytecode,
      CallBytecode/Plain. Alloc counts unchanged on all rows.
  Result (2026-07-29, quiet box, interleaved, n=10, ±3-6%):
  **FibonacciCL 282.5µs → 196.5µs, −30.43% (p=0.000)** — gate cleared 3×.
  CallBytecode −10.24% (p=0.000). CallBytecodePlain and
  CallBytecodeCanonical flat (Lisp-1 dialect, untouched path).
  Accumulate1000_Bytecode flat, p=0.912 — the codegen-sensitivity control
  that caught the sibling frame-sync regression. Alloc counts unchanged
  on every row. Goldset VM mode geomean +0.97% with every row
  statistically flat; eval mode geomean +1.32% with one row
  (`twice-macro` +6.90%, p=0.026, n=6) on the tree-walker, a path this
  change does not touch — drift, not causation.
- [ ] 6.3 Release-runner gate for the cross-engine bars; local perfgate is
      not evaluable on this box.

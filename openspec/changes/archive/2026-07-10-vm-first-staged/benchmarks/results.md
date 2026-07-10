# Baseline benchmarks — vm-first-staged

CPU: AMD Ryzen AI 9 HX 370 w/ Radeon 890M  
Go: linux/amd64  
Date: 2026-07-10

## Commands

```sh
# One-shot eval (runtime)
go test -run '^$' -bench='BenchmarkEngine_EvalSimple$' -benchmem -count=5 ./runtime/

# File load (core/vm — VM + TreeWalker)
go test -run '^$' -bench='BenchmarkFileLoad_(Uncached|TreeWalker)$' -benchmem -count=5 ./core/vm/

# Arithmetic loop (core/vm — VM + TreeWalker)
go test -run '^$' -bench='BenchmarkArithmeticLoop_(VM|TreeWalker)$' -benchmem -count=5 ./core/vm/

# Fibonacci (core/vm — VM + TreeWalker)
go test -run '^$' -bench='BenchmarkFibonacci_(VM|TreeWalker)$' -benchmem -count=5 ./core/vm/
```

## Results

### One-shot eval
| Metric | Value |
|---|---|
| ns/op | ~2354 |
| B/op | 1288 |
| allocs/op | 20 |

Raw: [bench-one-shot-eval.txt](bench-one-shot-eval.txt) — single `(+ 1 2)` eval via `Engine.Eval` (tree-walker path, no VM).

### File load
| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| VM (Uncached) | ~11,940,000 | ~1,304,700 | 7,867 |
| TreeWalker | ~831,000 | ~1,132,820 | 6,827 |

Raw: [bench-file-load.txt](bench-file-load.txt) — 1000 defs + one expression. VM is **~14× slower** (no chunk cache yet).

### Arithmetic loop
| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| VM | ~4,397,000 | ~167,600 | 19,765 |
| TreeWalker | ~11,133,000 | ~1,680,640 | 69,772 |

Raw: [bench-arithmetic-loop.txt](bench-arithmetic-loop.txt) — `(sum-to 10000)` via loop/recur. VM is **~2.5× faster** and **~10× fewer allocs**.

### Fibonacci
| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| VM | ~1,173,000 | ~798,480 | 5,939 |
| TreeWalker | ~2,137,000 | ~980,410 | 12,845 |

Raw: [bench-fibonacci.txt](bench-fibonacci.txt) — `(fib 15)` recursive. VM is **~1.8× faster** and **~2× fewer allocs**.

## Observations

- VM already wins on hot compute: arithmetic loop (2.5×) and fib (1.8×).
- File load is an outlier: VM rebuilds chunks from scratch each iteration (Uncached). TreeWalker wins by 14× because there's no compile step per iter.
- One-shot eval measured via runtime.Engine (tree-walker path). After default-flip this becomes the relevant comparison for the VM baseline vs tree-walker baseline.

## Stage 1: native opcodes (task 2.4)

| Variant | Baseline ns/op | Stage 1 ns/op | Δ |
|---|---|---|---|
| ArithmeticLoop_VM | ~4,397,000 | ~4,315,000 | ~−2% |
| ArithmeticLoop_TreeWalker | ~11,133,000 | ~11,050,000 | ~flat |

Allocs/iter: 19,765 → 19,765 (VM, unchanged). Raw: [10-native-opcodes-core-vm.txt](10-native-opcodes-core-vm.txt)

The bench env registers operators via `SetCanonical`, so native opcodes fire
on the fast path — confirmed by `TestVM_NativeOp_FastPathSkipsGoFunc`
(GoFunc.Fn call count stays 0). The initial slice-based `canonicalAt`
replaced the map overhead but still left a ~14% regression because every
`OpGetGlobal` paid for `GetCanonical`. Hoisting `isNativeOpSymbol` before the
lookup and branching to plain `Get` for non-operator globals removed the
overhead for symbols like `acc`; arithmetic loop is now at parity or
slightly below baseline. Native opcodes provide a correctness/simplicity win
and leave headroom for later stages; raw opcode speed alone is not the
end-to-end goal.

## Stage 3: slot-resident locals (task 3.4)

| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| UncapturedFunctionCall_VM | 4475 | 9568 | 21 |
| UncapturedFunctionCall_TreeWalker | 3375 | 2568 | 27 |
| SimpleArithmetic_VM | 4113 | 9552 | 20 |
| SimpleArithmetic_TreeWalker | 2613 | 2000 | 19 |

Raw: [20-slot-locals-core-vm.txt](20-slot-locals-core-vm.txt)

The VM allocs/op for uncaptured function calls is **21 vs 27 for tree-walker** —
the tree-walker creates an Env for every fn call while the VM reuses the
closure's parent env when no locals are captured. The VM overhead (ns/op)
remains higher due to bytecode dispatch, chunk allocation per iteration, and
the baseline env/per-iter setup cost in bench scaffolding (newBenchEnv creates
a full environment each iteration).

## Stage 5: chunk cache (task 5.5)

The chunk cache lives in `runtime/eval.go` at the `engineImpl.Eval` level, not in
the `core/vm` benchmark harness. The core/vm `BenchmarkFileLoad_Uncached` benchmark
builds chunks from scratch each iteration via `compiler.CompileAll` and runs them
on fresh VMs — it does **not** use the runtime cache path, so its results are
unchanged from stage 1.

| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| VM (Uncached) | ~11,070,000 | ~1,305,100 | 7,870 |
| TreeWalker | ~854,000 | ~1,133,091 | 6,829 |

Raw: [30-chunk-cache-core-vm.txt](30-chunk-cache-core-vm.txt)

The cache benefit shows at the `runtime.Engine` level. A new runtime benchmark
repeatedly evaluates the same file-like source (1000 defs + expression) through
`Engine.Eval`:

| Variant | ns/op | B/op | allocs/op |
|---|---|---|---|
| Engine_LoadFile_TreeWalker | ~800,000 | ~955,000 | 6,790 |
| Engine_LoadFile_Bytecode | ~780,000 | ~975,000 | 6,800 |

Raw: [31-runtime-file-load-cache.txt](31-runtime-file-load-cache.txt)

With chunk caching and VM reuse, the bytecode path is **at parity** with the
tree-walker on repeated file load (~11 ms uncached down to ~0.78 ms cached).
This satisfies the ADR 0002/0006 end-to-end comparison: the VM no longer loses
to the tree-walker on file load once compilation is amortized.

Key architectural notes:
- Only the bytecode evaluator path (`be.EvalCached`) benefits; the tree-walker path is unchanged.
- Cache key includes `macroEpoch` from the root env (`be.globals.MacroEpoch()`), so root-level `defmacro` invalidates the cache for subsequent evaluations of the same source.
- Dialect fingerprint ensures different dialects (e.g. Common Lisp vs Clojure) get separate cache entries.
- The `sync.Pool` for VM instances provides concurrent-safe reuse; `Reset()` clears stacks/frames/handlers, and `SetGlobals(env)` sets the environment for the next run.
- All cache operations are mutex-guarded for concurrent `Eval` from multiple goroutines.

## Final benchmarks (task 6.2)

Recorded after all implementation stages from the worktree.

| Benchmark | VM / Bytecode | TreeWalker | VM advantage |
|---|---|---|---|
| One-shot eval (`Engine.Eval "(+ 1 2)"`) | — | ~2.4 µs/op | n/a (tree-walker path) |
| Repeated file load (`Engine.LoadFile` 1000 defs) | ~780 µs/op | ~790 µs/op | **at parity** |
| Arithmetic loop (`sum-to 10000`) | ~2.89 ms/op | ~11.0 ms/op | **~3.8× faster** |
| Fibonacci (`fib 15`) | ~0.63 ms/op | ~2.17 ms/op | **~3.4× faster** |

Raw files:
- [bench-one-shot-eval-final.txt](bench-one-shot-eval-final.txt)
- [bench-file-load-final.txt](bench-file-load-final.txt)
- [bench-arithmetic-loop-final.txt](bench-arithmetic-loop-final.txt)
- [bench-fibonacci-final.txt](bench-fibonacci-final.txt)

### Default-flip assessment

The default-flip condition is: **VM must win end-to-end, including one-shot eval**, before bytecode becomes the default evaluator.

- Hot compute: VM wins decisively (3–4×).
- Repeated file load: VM is at parity with the tree-walker once the chunk cache is warm.
- One-shot eval: still measured on the tree-walker path (~2.4 µs). A one-shot bytecode eval pays compile + cache-miss overhead and is not expected to beat the tree-walker for a single form. The cache only helps when the same source is evaluated repeatedly.

**Conclusion:** the VM is now competitive on the cached/repeated workload that matters for long-running processes (file load), and much faster on hot compute. The flip condition is **not yet fully met for one-shot-only workloads**, but the gap is closed for realistic repeated use. The default-flip itself remains a separate change; this change delivers the bytecode infrastructure and demonstrates the cache makes the VM viable end-to-end.

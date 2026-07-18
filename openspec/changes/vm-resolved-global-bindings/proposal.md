## Why

The article benchmark suite (go-lispico-bench, v0.7.0) shows VM fib(20) at 5.84 ms/op vs GopherLua 1.98 ms/op on the same machine — a ~3x gap. The CPU profile attributes **33% of all cycles to `Env.GetCanonical`** (64% of that in `runtime.mapaccess2_faststr`, 25% in `sync.RWMutex` atomics), plus additional `Env.Get` walks for user globals like `fib`. Every `OpGetGlobal` re-resolves a string name through a locked map chain on **every execution**, and the canonical-operator check repeats that walk for every `+`/`-`/`<` dispatch. The profile also shows stdlib `GoFunc` bodies (`orderingFunc`, `registerArithmetic`) on the hot path — the `canonicalAt` stack-marker machinery falls back to full `GoFunc` dispatch for a fraction of native-op executions even though the bindings are canonical.

Every fast Go interpreter resolves names before the hot loop runs: GopherLua reads locals/upvalues by compile-time register index and globals by precompiled constant string; goja bakes stack/stash indices into opcodes at compile time; tengo's symbol table maps every global to an integer index into a plain `[]Object` — the name map is gone by runtime; starlark-go's resolver classifies every name to an indexed slot. None of them holds a lock in the interpreter loop.

ADR 0008 explicitly deferred "resolved-binding cells" until "a failing gate cell or another measured consumer need". The article benchmark is that measured need.

## What Changes

- `core.Env` gains **binding cells**: a stable, per-name value holder (atomic load/store) with a canonical flag, created on first bind and written through by `Set`/`SetCanonical`/`Delete`. Reads through a cell are lock-free; ADR 0003 concurrency (concurrent Eval/Call on one Engine) is preserved by atomic publication.
- The VM resolves an `OpGetGlobal` call site to a cell **once** and caches the cell on the chunk's call-site table, guarded by the identity of the environment the chunk runs against (chunks are cached and shared; a chunk running against a different globals env re-resolves). Local shadowing rules are unchanged — the compiler already refuses the native path for locally shadowed operators.
- Canonical-operator dispatch (`OpAdd`..`OpEq`) checks the cell's canonical flag with one atomic load, replacing the per-execution `GetCanonical` chain walk and the `canonicalAt` per-push stack bookkeeping.
- Rebind semantics are unchanged and spec-covered: `Set` on a canonical name clears the flag through the cell, and subsequent native ops fall back to the ordinary call path, matching the tree-walker.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: new requirement — resolved global bindings (repeat execution of a compiled chunk does not re-resolve global names through a locked map walk; rebinds stay visible; races stay clean). Modified requirement — native arithmetic/comparison opcodes check canonical status through the binding cell, same observable semantics.
- `core-engine`: new requirement — binding-cell contract on `Env` (stable cell per name, write-through, atomic visibility).

## Impact

- Code: `core/env.go` (cells), `core/vm/vm.go` (`OpGetGlobal`, `dispatchNativeOp`, removal of `canonicalAt`), `core/vm/chunk.go` (call-site cell table), `core/compiler/compiler.go` (call-site table emission).
- Expected effect: removes the ~33% `GetCanonical` cycle share plus the intermittent native-op fallback; fib bytecode target ≥25% faster.
- Gate: ADR 0008 goldset must not regress; engine-sensitive hot cells should improve.
- Invariants: VM/tree-walker parity (crossval suite), ADR 0003 race-freedom (`-race` suite), rebind-falls-back semantics.
- Sequencing: independent; lands before `vm-dispatch-loop-tightening` shrinks its canonicalAt task to a no-op.

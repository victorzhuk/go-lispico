## Why

Engine startup with the bytecode evaluator costs 120 µs vs 74 µs on the tree-walker (GopherLua 70 µs, goja 2.5 µs): `Use(stdlib.New())` evaluates the stdlib's pure-Lisp definitions through macro-expansion and compilation **per engine**, because the chunk cache lives on the per-engine `bytecodeEvaluator`. The stdlib source is a compile-time constant — identical for every engine with the same dialect — so this work is repeated for no reason. With `engine-bytecode-default` flipping the default, per-engine recompilation would become a startup regression for every consumer; short-lived-engine embedders (engine per request/tenant) pay it constantly.

## What Changes

- Process-level reuse of compiled stdlib artifacts: the first engine with a given dialect fingerprint compiles the stdlib's pure-Lisp forms; subsequent engines with the same fingerprint reuse the compiled artifacts instead of recompiling. Keyed by (dialect fingerprint, source identity); bounded (a handful of dialects in practice — an entry ceiling guards pathological dialect churn).
- Cross-engine safety is part of the contract: shared artifacts carry no per-engine state. Chunk `Code`/`Constants`/`SubChunks` are immutable after `Validate`; the per-chunk global-read site table is per-engine (fresh sites on the reused artifact — a shallow chunk-tree copy — so two engines never ping-pong one site's published resolution).
- Definitions still execute per engine (each engine's root env gets its own bindings, macros, canonical flags); only read/expand/compile work is shared. Observable behavior — bindings, macro epoch semantics, `UnloadPlugin`, goldset/crossval results — is byte-identical.
- Profiling task first: confirm the split of the 120 µs (reader, macro-expand, compile, env population) and size the win before wiring; the reuse boundary (compiled chunks vs expanded forms) follows the profile.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Compiled-chunk cache` gains a process-level tier — identical plugin source under an identical dialect SHALL NOT be recompiled per engine, with explicit cross-engine isolation obligations.

## Impact

- Code: `runtime/eval.go`/`runtime/plugin.go` (cache tier + reuse path), `core/vm/chunk.go` (cheap per-engine re-site copy), stdlib plugin load path.
- Expected: engine startup ≤ ~40 µs under the bytecode default — ahead of GopherLua's 70 µs; goja's 2.5 µs lazy-init startup stays out of reach and out of scope.
- Risks: cross-engine leakage through shared structures — contained by the immutability invariant, fresh site tables, and isolation scenarios (a `def` in engine A never appears in engine B).
- Sequencing: same release as `engine-bytecode-default`; independent of the other changes.

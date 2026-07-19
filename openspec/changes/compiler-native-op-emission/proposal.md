## Why

The `bytecode-vm` requirement "Native arithmetic and comparison opcodes" is
unmet in real usage. In `compileList`, native opcodes (`OpAdd`, `OpSub`, …) are
emitted only inside `if isSpecial`, and under a configured dialect
`isSpecial = dialect.CanonicalName(op).ok` — which is **false** for
`+ - * / < > <= >= =` because those symbols are not in any shipped dialect's
vocabulary (only the 22 special forms are). So with the default CL dialect or
clojure, every compiled arithmetic/comparison operator emits `OpCall` and runs
as a `GoFunc`; the native opcodes, `dispatchNativeOp`, `execNativeFast`, and the
`OpGetGlobal` freeze machinery are dead code on the runtime path.

The compiler unit tests pass only because they use the nil-dialect `NewCompiler`
(where `isSpecial` defaults to `true`); the runtime always uses
`NewCompilerWithDialect`. Verified directly: `(+ 1 2)` compiled with
`clojure.Dialect()` emits `GET_GLOBAL, CONST, CONST, CALL` — no `OpAdd`.

Consequence: hot arithmetic loops and every host→script `Call` of an arithmetic
body pay full `GoFunc` dispatch. Measured on a `WithBytecode()` engine:
`(+ a b)` `Engine.Call` ≈ 660 ns / 3 allocs vs a GoFunc-free body ≈ 390 ns /
1 alloc; goldset `loop-sum` sits at 114 allocs. This is the dominant residual
that puts the `engine-call-fast-path` ~500 ns boundary target out of reach for
arithmetic bodies, and it silently violates the accepted native-opcode spec and
its "Hot loop avoids builtin dispatch" / "Recursive calls keep the native path"
scenarios.

## What Changes

- The dialect-aware compiler SHALL emit the native opcode for a list head that
  resolves to a canonical native-operator symbol, is not a special form, and is
  not locally shadowed — independent of whether the dialect's `CanonicalName`
  classifies it as special. Special-form precedence, dialect rename/removal, and
  local-shadow fallback are preserved.
- Rebind-safety is unchanged: the VM already freezes canonical eligibility at
  `OpGetGlobal` and `dispatchNativeOp` falls back to calling the operator value
  when the binding is not the canonical builtin, so emitting the opcode is safe
  even when `+` is later rebound.
- Add runtime-level coverage so the native-path scenarios are verified through
  `NewCompilerWithDialect` (the real path), not only the nil-dialect compiler.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: the "Native arithmetic and comparison opcodes" requirement is
  clarified to hold under a configured dialect (the shipped runtime path), not
  only the dialect-less compiler.

## Impact

- Code: `core/compiler/compiler.go` (`compileList` native-op gate), compiler and
  runtime tests, goldset/bench evidence.
- Parity: tree-walker/VM cross-validation must stay green — the native opcodes
  already match stdlib arithmetic semantics (int/float promotion, div-by-zero);
  this only makes them reachable under a dialect.
- Perf: unblocks the `engine-call-fast-path` ~500 ns / ≤4 alloc target for
  arithmetic bodies and cuts per-iteration allocations in arithmetic loops
  (ADR 0008 goldset gate).
- Risk: dialect operator renaming/removal, local shadowing, canonical rebind —
  each already has a mechanism; the change adds explicit dialect-aware parity
  tests so the gap cannot silently reopen.

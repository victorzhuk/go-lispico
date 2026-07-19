## 1. Dialect-aware native-op emission

- [x] 1.1 In `compileList`, emit the native opcode for a head that resolves to a canonical native-operator symbol and is not a special form and not locally shadowed, independent of `isSpecial`. Preserve special-form precedence (a form named like an operator stays special), dialect rename/removal handling, and the locally-shadowed fallback to `OpCall`.
- [x] 1.2 Tests at the dialect-aware level (`NewCompilerWithDialect`, CL and clojure): `(+ a b)` / `(< a b)` emit the native opcode (`OpAdd` / `OpLt`) with a dialect-appropriate head — `OpGetGlobal` under clojure (Lisp-1), `OpGetFunc` under CL (Lisp-2); a locally-shadowed operator falls back to `OpCall`; cross-validation (tree-walker vs VM) stays green.

## 2. Runtime coverage and perf evidence

- [x] 2.1 Runtime-level proof: a `WithBytecode()` engine's arithmetic loop body executes via native opcodes with no `GoFunc` dispatch (execution probe or alloc-drop assertion); goldset `loop-sum` allocs decrease and no cell regresses (ADR 0008).
- [x] 2.2 Re-measure the `engine-call-fast-path` boundary: `(+ a b)` `Engine.Call` on a `WithBytecode()` engine with a canonical stdlib `+` approaches ~500 ns and ≤ 4 allocs/op; record before/after alongside the GoFunc-free baseline.

## 3. Lisp-2 (CL) function-cell native safety

- [x] 3.1 `compileNativeOp` emits `OpGetFunc` for the operator head under a Lisp-2 dialect (function cell) and `OpGetGlobal` otherwise; arguments stay value-namespace under both.
- [x] 3.2 `core/env.go`: function-cell canonical tracking — `SetCanonicalFunc` and `GetFuncCanonical(name) (value, found, canonical)`; plain `SetFunc` (the `OpSetFunc` / `defun` / `MergeInto` rebind path) clears the canonical marker.
- [x] 3.3 `applyVocabulary`'s Lisp-2 bridge marks canonical value bindings canonical in the function cell (`GetCanonical` → `SetCanonicalFunc`); non-canonical bindings and `Engine.Bind` / `EvalWithBindings` stay `SetFunc`.
- [x] 3.4 VM `OpGetFunc` freezes the native op when the symbol is a native operator and the function-cell binding is canonical, mirroring the `OpGetGlobal` freeze; a rebound (non-canonical) function cell is not frozen and `dispatchNativeOp` falls back to calling the pushed value.
- [x] 3.5 Parity proof (crossval): under CL, `(defun + (a b) (- a b))` then `(+ 5 3)` gives identical tree-walker and VM results (`2`); a `defun`-rebound `-` / `<` likewise; a canonical CL `(+ a b)` takes the native path (alloc-drop / no `GoFunc` dispatch). Full `runtime` + `core/vm` suites and `-race` stay green.

## 1. Dialect-aware native-op emission

- [ ] 1.1 In `compileList`, emit the native opcode for a head that resolves to a canonical native-operator symbol and is not a special form and not locally shadowed, independent of `isSpecial`. Preserve special-form precedence (a form named like an operator stays special), dialect rename/removal handling, and the locally-shadowed fallback to `OpCall`.
- [ ] 1.2 Tests at the dialect-aware level (`NewCompilerWithDialect`, CL and clojure): `(+ a b)` / `(< a b)` emit `OpAdd` / `OpLt`; a locally-shadowed operator and a rebound operator still fall back to `OpCall` / the operator value; cross-validation (tree-walker vs VM) stays green.

## 2. Runtime coverage and perf evidence

- [ ] 2.1 Runtime-level proof: a `WithBytecode()` engine's arithmetic loop body executes via native opcodes with no `GoFunc` dispatch (execution probe or alloc-drop assertion); goldset `loop-sum` allocs decrease and no cell regresses (ADR 0008).
- [ ] 2.2 Re-measure the `engine-call-fast-path` boundary: `(+ a b)` `Engine.Call` on a `WithBytecode()` engine approaches ~500 ns and ≤ 4 allocs/op; record before/after alongside the GoFunc-free baseline.

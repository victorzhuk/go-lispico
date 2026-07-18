## 1. Chunk validation at load

- [ ] 1.1 Add `Chunk.Validate()` covering constant indices, symbol-constant types, jump/`OpLoop` targets in range, sub-chunk references; called at compile completion and cache insertion; typed `BytecodeError` on failure.
- [ ] 1.2 Convert hot-loop `GetConstant`/`GetSymbolConstant`/ip-range checks to direct indexing on validated chunks; keep a debug assertion build tag if useful.
- [ ] 1.3 Tests: existing malformed-chunk robustness tests assert rejection at load; hand-built invalid chunks (bad jump target, bad constant index, wrong symbol type) rejected without panic; fuzz harness moved to the load boundary.

## 2. Frame-local dispatch state

- [ ] 2.1 Cache chunk/code/ip/base/env in `Run` locals; sync on call, return, throw, handler unwind; truthiness hook resolved at frame entry.
- [ ] 2.2 Tests: full crossval + dialect + try/catch suites green (behavior unchanged); deep-recursion and tail-call paths exercised.
- [ ] 2.3 Compiler tracks each chunk's maximum operand-stack depth (`Chunk.MaxStack`, validated in 1.1); `Run` pre-grows the stack once per frame entry so `push` needs no growth check in the loop.

## 3. Preboxed small values

- [ ] 3.1 Package-level `Nil`/`True`/`False` singletons and preboxed `Int` range (−128..1023) with a `boxInt(int64) Value` helper; wire reader, VM native ops, and stdlib arithmetic/comparison through it.
- [ ] 3.2 Tests: `Equals`/hash/print semantics unchanged; `AllocsPerRun` on a small-int arithmetic loop shows zero boxing allocs in range; goldset alloc-count cells non-increasing.

## 4. Verify

- [ ] 4.1 `go test ./...`, `-race` suites green.
- [ ] 4.2 Goldset gate non-regressing; VM-bound cells improved.
- [ ] 4.3 Bench evidence recorded: fib bytecode ns/op delta and alloc delta; profile shows `GetConstant`/`GetSymbolConstant` gone from top functions.

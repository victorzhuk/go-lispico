## 1. Pinned regression (red first)

- [x] 1.1 Add a cross-mode test asserting `(def a 10) (let [a 1 b a] b)` evaluates to `10` under both execution modes, and `(def a 10) (let* [a 1 b a] b)` to `1` under both — VM `let` case red before the fix, everything else green.

## 2. Compiler fix

- [x] 2.1 Two-phase `compileLet`: phase one validates each binding name and compiles each init with `Emit(OpSetLocal, base+i)` using the precomputed slot index, without registering locals; phase two runs `addLocal` for every name. Instruction stream identical to before; `compileLetStar` untouched.

## 3. Verify

- [x] 3.1 Regression from 1.1 green under both modes; `go test ./core/... ./internal/goldset/ -count=1` and `go test -race ./... -count=1` clean; confirm emitted bytecode for a shadow-free `let` is unchanged (existing compiler/VM tests stay green without edits).
- [x] 3.2 Update the release-consumer-gate note: the VM correctness-floor defect recorded in its tasks.md 5.1 is fixed by this change.

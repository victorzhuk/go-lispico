## 1. Pin current behavior (red)

- [x] 1.1 Failing test: sequential `let` — `(let [x 1 y x] y)` evaluates to `1` with an outer `x` bound to `99`, on the tree-walker and the VM.
- [x] 1.2 Failing test: `(unless false 1)` fails as an unresolved symbol on both execution paths.
- [x] 1.3 Failing test: a `cond` clause headed by the bare symbol `else` does not act as an else clause; `:else` does, on both execution paths.
- [x] 1.4 Failing test: `(string/join "-" ["a" "b"])` evaluates to `"a-b"`.
- [x] 1.5 Failing test: `(try (throw {:code :denied}) (catch e (get e :code)))` evaluates to `:denied` on both execution paths; a non-`throw` engine error still binds its message string.

## 2. Implement the alignment (green)

- [x] 2.1 Sequential `let`: `evalLet` evaluates each init against the child scope; `compileLet` registers each local before compiling the next init (mirroring `compileLetStar`). `let*` remains registered with the same semantics.
- [x] 2.2 Remove `unless`: drop the kernel entry and `evalUnless`, the compiler's `case "unless"` and `compileUnless`; fix every test, doc, and gold-set fixture referencing `unless`.
- [x] 2.3 `:else`-only `cond`: drop the `Symbol` arm from `core.isCondElse` and the compiler's `isElse`, keeping the `Keyword` arm.
- [x] 2.4 `string/join` argument order: separator is `args[0]` (must be a string), collection is `args[1]`; update the stdlib test.
- [x] 2.5 Structured `catch`: `evalTry` binds `throwError.value` via `errors.As` and keeps the message-string fallback for non-`throw` errors; VM `OpThrow` passes the raw value to the handler and `coerceThrow` is deleted.

## 3. Refactor and verify

- [x] 3.1 Cross-validation corpus covers sequential `let` (`(def a 10) (let [a 1 b a] b)` ⇒ `1`) and structured `catch` parity; `TestVMVsTreeWalker_NonStringThrow` asserts `Int{42}` on both paths.
- [x] 3.2 `go test ./...` and `go test -race ./core/... ./plugins/...` green; `golangci-lint run` clean.

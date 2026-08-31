## 1. Pin evaluator ownership

- [x] 1.1 Add eager and lazy tests with instrumented environment evaluators and verify every bootstrap definition is evaluated by the installed owner, never a fresh identity Evaluator.
- [x] 1.2 Add Lisp-1, Lisp-2, and empty-base Dialect behavior goldens for every bootstrap name and verify eager/lazy publication lands in the correct cells without widening the user-visible Kernel table.
- [x] 1.3 Add a direct-plugin test with no evaluator installed and verify stdlib adopts one default Evaluator into the environment before defining source; add a second test proving an existing evaluator is never replaced.
- [x] 1.4 Add trust-boundary tests proving the capability is available to trusted host Go code but no Lisp binding, Special form, reflected value, or empty-base vocabulary entry exposes it.
- [x] 2.1 Add exported `core.BootstrapDefiner.DefineBootstrap(context.Context, string, *core.Env) (core.Value, error)` and compile-time assertions for `*core.engine` and `*runtime.bytecodeEvaluator`; reject an installed evaluator that lacks the capability.
- [x] 2.2 Implement the trusted full-reader/single-definition grammar, permitting exactly one top-level `defn` or `defmacro`, and verify CL-disabled bracket syntax still loads while multiple/non-definition forms fail typed.
- [x] 2.3 Route eager bootstrap loading through the owned operation, remove caller-side evaluator substitution, and verify all eager ownership/publication tests pass.
- [x] 2.4 Route lazy source materialization through the same operation, remove its fresh Evaluator, and verify concurrent first touch publishes each definition exactly once under `-race`.

## 3. Verify startup contracts

- [x] 3.1 Run focused stdlib bootstrap, runtime lazy-materialization, Dialect, unload/reload, and cache tests; verify eager and lazy observable behavior is unchanged.
- [x] 3.2 Run `go test ./...`, `go test -race ./core/... ./plugins/stdlib/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.

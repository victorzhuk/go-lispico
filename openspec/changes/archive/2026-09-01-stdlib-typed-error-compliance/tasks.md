## 0. Enforce prerequisites

- [x] 0.1 Verify `cl-collection-adapters` and `stdlib-nil-lookup-semantics` plus their resource/ownership prerequisites are implemented, validated, and archived; snapshot the final active stdlib and CL-adapter validation surface.

## 1. Inventory and characterize failures

- [x] 1.1 Materialize the frozen family table as an executable inventory of every error return reachable from active stdlib and CL-adapter GoFuncs, including factories and transitive helpers; classify each as arity, type, domain/evaluation, resource/context pass-through, callback pass-through, or immediate external-error conversion and verify no path is unclassified.
- [x] 1.2 Add direct table tests for representative exact/ranged/variadic arity, positional type, bounds/domain, callback, and Terminal failures; verify current messages are captured before constructor changes.
- [x] 1.3 Add Engine behavior goldens under Evaluator and VM for each class and verify `errors.As`, `Code`, equivalent diagnostic meaning, and Terminal non-catchability; do not assert source positions until the value model carries them.

## 2. Add typed construction support

- [x] 2.1 Add minimal unexported stdlib helpers for exact/ranged/variadic arity, positional type, and domain-message shapes using the existing `core.LispicoError` API; verify they return `*core.LispicoError` directly without arbitrary code strings or double wrapping.
- [x] 2.2 Add the narrow external-cause helper needed by parse/conversion paths; verify `errors.Is`/`errors.As` can still reach the cause while the outer stdlib code remains stable.

## 3. Migrate stdlib Builtins

- [x] 3.1 Convert arithmetic, comparison, and conversion validation/domain failures and verify their focused tests pass with unchanged success values.
- [x] 3.2 Convert collection, higher-order, predicate, and string validation/domain failures and verify callback and Terminal errors still pass through unchanged.
- [x] 3.3 Convert CL `nth`, `mapcar`, and `sort` adapter-local validation while preserving shared-kernel and callback errors; verify the exact option grammar keeps its specified classifications.
- [x] 3.4 Add package-wide static reachability checks that reject plain validation errors in stdlib/CL registered GoFuncs, factories, and transitive helpers, with reviewed exceptions only for immediate external-error conversion; verify fixtures catch both a closure violation and a helper-only violation.

## 4. Verify host error contracts

- [x] 4.1 Document stdlib error codes and the `errors.As` host pattern; verify examples cover arity, type, EvalError, and ResourceLimitError without promising exact message stability.
- [x] 4.2 Run focused direct/runtime/VM tests, `go test ./...`, `go test -race ./core/... ./plugins/stdlib/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.

## 0. Enforce prerequisites

- [x] 0.1 Verify `stdlib-nil-sequence-semantics` and its transitive lookup/CL/resource/ownership/typed-error prerequisites are implemented, validated, and archived; snapshot the final registered stdlib and CL-adapter surfaces.

## 1. Freeze executable inventories

- [x] 1.1 Materialize the design work table as test data and include every scalable phase in registered GoFuncs, factories, transitive helpers, and CL shared kernels; verify each phase has exactly one owner or reviewed trusted-host/bounded disposition.
- [x] 1.2 Materialize every successful return branch under the six result classes, including mixed string/persistent branches; verify every registered name and reachable return is classified before implementation edits.
- [x] 1.3 Add static fixtures proving the checks catch a missing registration, helper-only loop, opaque call, unflushed early return, duplicate callback charge, and unclassified result branch.

## 2. Migrate work by family

- [x] 2.1 Migrate numeric/comparison and core-owned equality phases; verify exact numeric behavior and host `Value` trust boundaries remain explicit.
- [x] 2.2 Migrate collection, lookup, construction-depth, and natural-sort phases; pass the active evaluator into resource helpers and verify low limits inside child Lambda environments under Evaluator and VM.
- [x] 2.3 Migrate higher-order and CL shared-kernel phases; verify callback execution has one owner while extraction/alignment/copy/result phases use `BuiltinWorkBudget` and flush every return.
- [x] 2.4 Migrate string/format/parse phases using interruptible kernels or deterministic bounds; verify Unicode, empty separator, formatting, parse, and trusted-host formatting cases.

## 3. Migrate result ownership

- [x] 3.1 Mark wholly borrowed and already callback-accounted returns with zero bytes; verify large stored/default/accumulator/callback values do not consume result allocation twice.
- [x] 3.2 Preserve fresh deep, fresh-container, incremental persistent, and mixed charges; verify tight allocation tests fail closed without recharging shared payload.
- [x] 3.3 Run family goldens under Evaluator, VM, re-entry, and direct Apply; verify values, typed errors, callback counts, Terminal precedence, and immutable inputs remain unchanged.

## 4. Verify completeness

- [x] 4.1 Run the registration/call-graph/result inventory checks and focused resource/deadline/allocation tests; verify no exception lacks a disposition and bound/trust rationale.
- [x] 4.2 Run `go test ./...`, `go test -race ./core/... ./plugins/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.

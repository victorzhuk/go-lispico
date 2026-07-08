# Tasks — Per-Engine dialect dispatch

## 1. Characterization (red first)

- [x] 1.1 Add characterization tests at the `runtime` Engine seam pinning current special-form behavior for a representative set (`if`, `def`, `defn`, `let`, `quote`, `cond`, `loop`/`recur`, `and`/`or`). Acceptance: they pass against the current global-map implementation, unchanged.
- [x] 1.2 Add a failing test that two Engines can be constructed with different dialects and their tables do not interfere. Acceptance: red — no `WithDialect` exists yet.
- [x] 1.3 Add a failing test that an empty-base dialect rejects a kernel form absent from its delta. Acceptance: red.

## 2. Kernel table + Dialect value

- [x] 2.1 Extract the special forms into a canonical Kernel table keyed by neutral names. Acceptance: 1.1 characterization tests stay green.
- [x] 2.2 Introduce the Dialect value: base selector (full | empty) plus rename/add/remove delta operations. Acceptance: unit tests resolve a delta to an effective table.
- [x] 2.3 Implement resolution: base + delta → effective `name → formFn` table. Acceptance: rename, add, remove, and empty-base fail-closed each covered by a unit test.

## 3. Per-Engine wiring

- [x] 3.1 Move dispatch to read the effective table from Engine-scoped state; remove the package global. Acceptance: 1.1 stays green; no `var specialForms` remains.
- [x] 3.2 Add `WithDialect(...)` to `runtime`, capturing the Dialect at `New`. Provide the identity dialect reproducing today's behavior as the resolved default. Acceptance: 1.2 goes green; `runtime.New()` behavior is unchanged.
- [x] 3.3 Confirm no Lisp-side path can change the running dialect. Acceptance: a test that evaluated code cannot alter the effective table.

## 4. Verify

- [x] 4.1 1.2 and 1.3 green; full existing suite green including `core/vm/crossval_test.go`. Acceptance: `go test ./...` passes.
- [x] 4.2 `openspec validate dialect-per-engine-dispatch --strict` passes.

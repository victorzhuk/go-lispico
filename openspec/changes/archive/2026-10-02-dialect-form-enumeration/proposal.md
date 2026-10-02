## Why

The change was opened because a Dialect could not list the special forms it dispatches. Embedders that check source statically had to hand-type the list: yagel kept one in `internal/cli/rules_check.go` and pinned it with `TestSpecialForms_ResolveInDialect` via `CanonicalName`, and zhk kept a hand-typed `Forms()` for its old empty-base policy dialect. A hand-typed list can accept a form the evaluator no longer knows, or miss one a newer kernel adds.

`Forms()` and its four scenario tests are now adopted at this base. The motivation above is kept as the history of the change.

## What Changes

- `func (d Dialect) Forms() []string` exists: it returns the sorted visible names of the Dialect's resolved special-form table, including the Lisp-2 intrinsics `funcall` and `function` when the axis is on.
- The answer comes from the frozen resolved table; the returned slice is a fresh copy the caller owns.
- The four scenario tests exist and pin that behavior.
- This plan starts from no outstanding prerequisites and changes no public API. The remaining work is the `CHANGELOG.md` `[Unreleased]` `### Added` release note.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **A Dialect enumerates its special forms**.

## Impact

`core/dialect.go`, test files `core/dialect_forms_test.go` (full-base and empty-base scenarios, package core), `cl/forms_test.go` (renames and Lisp-2 intrinsics scenario, package cl_test), `clojure/forms_test.go` (caller-mutation scenario, package clojure_test), CHANGELOG. The method and the tests are already adopted. No public API change remains. Follow-up outside this repo: yagel can derive its special-form group from `clojure.Dialect().Forms()` once released.

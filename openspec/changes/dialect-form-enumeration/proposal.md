## Why

A Dialect cannot list the special forms it dispatches. Embedders that check source statically have to hand-type the list: yagel keeps one in `internal/cli/rules_check.go` and pins it with `TestSpecialForms_ResolveInDialect` via `CanonicalName`, and zhk kept a hand-typed `Forms()` for its old empty-base policy dialect. A hand-typed list can accept a form the evaluator no longer knows, or miss one a newer kernel adds.

## What Changes

- Add `func (d Dialect) Forms() []string`: the sorted visible names of the Dialect's resolved special-form table, including the Lisp-2 intrinsics `funcall` and `function` when the axis is on. Applied after `dialect-declarative-spec`, so every Dialect value is already valid.
- The answer comes from the frozen resolved table; the returned slice is a fresh copy the caller owns.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **A Dialect enumerates its special forms**.

## Impact

`core/dialect.go`, `core/dialect_test.go`, CHANGELOG. Additive API. Follow-up outside this repo: yagel can derive its special-form group from `clojure.Dialect().Forms()` once released.

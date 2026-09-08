## Context

See `proposal.md` for the reproduced failure at review commit `3bcf9c1`. `minMaxFunc` stores every candidate in `float64` and converts the winner back to `int64` when no float argument occurred. Existing comparison code in `internal/collections/order.go` already distinguishes exact integer pairs from mixed float comparisons.

## Goals / Non-Goals

**Goals:** keep integer extrema exact without changing the shared builtin's validation, mixed numeric behavior, or cooperative work budget.

**Non-Goals:** arithmetic overflow changes, a numeric tower, exact mixed integer/float comparison, new VM opcodes, or changes to JSON decoding.

## Decisions

- Keep the integer candidate in `int64` until the first `core.Float`; then promote and retain the existing float comparison path. This avoids a second argument traversal and preserves the rule that any float operand makes the result a float, even when an integer wins.
- Keep the existing `BuiltinWorkBudget` ownership and `finishBuiltin` returns. If control-flow edits change inventoried phases or return branches, update `internal/inventory/work_data.go` and `internal/inventory/result_data.go` rather than weakening completeness checks.
- Do not cast a rounded float back to recover an integer result. Adjacent integers can already have collapsed to one float at that point.

## Verification

Testing mode: existing-service-strict. Add failing regressions before changing the implementation. Extend `TestArithmetic_MinMax` and exercise actual `Engine.Call` and source evaluation under `WithTreeWalker()` and `WithBytecode()`.

Cover singleton endpoints `-9223372036854775808` and `9223372036854775807`; adjacent positive and negative integers around `9007199254740992`; reversed input order; repeated extrema; and full-range mixed signs. Assert `core.Int` and its exact value. Mixed cases such as `(max 2 1.5)` and `(min 1 1.5)` must still return `core.Float`.

Retain `TestNumeric_ShortCallsKeepExactValuesAndErrors`, the existing numeric traversal tests for cancellation, expired deadlines, and reduction limits, and executable inventory checks. Run resource-limited project checks through `make test` with a worker cap in `GOTESTFLAGS`; no benchmark change is required.

## Risks / Trade-offs

- Float promotion can still round large integers in mixed calls → preserve and document that existing policy rather than changing it in this fix.
- A new result branch can escape accounting inventory → keep static inventory validation in the verification floor.

## Migration Plan

No predecessor or data migration. Update the existing changelog for corrected integer results. Reverting the implementation restores the known precision defect; no persisted format changes are involved.

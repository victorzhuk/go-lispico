## Plan

### Codemap

Sites the change touches, one per task.

**Task 1.1** — add red regression asserting `estimateFormatAllocBytes` ≥
`len(fmt.Sprintf(...))` for verb/operand mismatches across numeric, boolean and
character verbs with `core.String` operands of a few KiB.

- File: `plugins/stdlib/strings_budget_test.go`
- Symbol: new test `TestFormatEstimatorBoundsMismatchedVerbAndOperand` in the
  format-budget group.
- Change: derives every expectation from `fmt.Sprintf` at runtime; checks
  `estimateFormatAllocBytes` is at least the render length for the verb/operand
  pairs where `fmt` cannot render the operand directly and falls into the
  `%!verb(<type>=...)` diagnostic. Task 1.1 covers numeric, boolean and
  character verbs over a `core.String` operand: the verbs are `%d`/`%b`/`%o`/`%O`/`U` (numeric), `%t` (boolean) and `%c` (character). `%x/%X` is well-formed for `core.String` (renders hex) and not part of task 1.1.
  Across operand types, the mismatched arms extend as: against `core.Bool` the
  numeric verbs `%d/%b/%o/%O/%U` and the character verb `%c` (and `%x/%X`)
  all mismatch; against `core.Float` the numeric verbs `%d/%b/%o/%O/%U` and
  the boolean verb `%t` and the character verb `%c` (and `%x/%X`) all
  mismatch; against `core.Int` the boolean verb `%t` and the float verbs
  `%e/%E/%f/%F/%g/%G` mismatch. Each `core.Float` operand also trips `%t`.

**Task 1.2** — extend the same regression to an explicit argument index
repeated across several directives, so one operand feeding many directives is
pinned.

- File: `plugins/stdlib/strings_budget_test.go`
- Symbol: same test, sub-table `repeatedDirective`.
- Change: covers `%[1]d%[1]d%[1]d%[1]d` and `%[1]c%[1]c%[1]c%[1]c` over a 4
  KiB `core.String` and asserts the estimate is at least 4× the diagnostic
  length.

**Task 1.3** — pin the precision arm at or above the no-precision arm for
large-magnitude floats.

- File: `plugins/stdlib/strings_budget_test.go`
- Symbol: same test, sub-table `precisionOnLargeMagnitudeFloat`.
- Change: `%.2f` and `%.0f` against `1e200` and `1e308` with the result derived
  from `fmt.Sprintf` at runtime; asserts `estimate ≥ render` and
  `precisionArm ≥ noPrecisionArm`.

**Task 2.1** — size a mismatched directive against the operand `fmt` will
render into the diagnostic.

- File: `plugins/stdlib/strings.go`
- Symbol: `estimateFormatValueBytes` (lines 524–600).
- Change: introduce a `mismatchedVerbDiagnosticBytes(verb byte, v core.Value)`
  helper that returns `len(diagnosticPrefix) + len(operandRender) +
  len(diagnosticSuffix)` where:
  - `diagnosticPrefix = "%!" + verb`
  - `diagnosticSuffix = "(<typename>=...)"` rendered via `fmt.Sprintf("%T", v)`
    and `len(operand)` from `formatStringBytes(v)` (already cached for
    `core.String`; `core.ValueDeepBytes` for others is the path
    `core-value-walk-sharing-bound` owns — the diagnostic only needs the
    *rendered* length, which for `core.String` is exactly `len(v.V)`);
  - dispatch through this helper in the existing verb-specific branches when
    the operand's Go type is one the verb cannot satisfy. The mismatched
    table the helper covers: `%d/%b/%o/%O/%U/%c` against `core.String`;
    `%d/%b/%o/%O/%U/%c/%x/%X` against `core.Bool`; `%d/%b/%o/%O/%U/%t/%c/%x/%X`
    against `core.Float`; `%t/%e/%E/%f/%F/%g/%G` against `core.Int`. The
    dispatch site is the per-verb case in `estimateFormatValueBytes` (a
    Go type-switch on `v` reaches the helper in O(1)); a runtime
    `fmt.Sprintf("%T", v)` lookup is unnecessary.
- Saturating arithmetic reuses `addFormatEstimate` so a 1 GiB operand cannot
  wrap the estimate.

**Task 2.2** — keep the float precision arm at or above the no-precision arm.

- File: `plugins/stdlib/strings.go`
- Symbol: `estimateFormatValueBytes` cases `f`/`F`/`g` (lines 577–584) and
  `e`/`E` (lines 585–590).
- Change: when `hasPrecision`, size the arm as
  `max(noPrecisionEstimate, 1 + integerDigits + 1 + precision)`, where
  `integerDigits` is bounded by the precision-less `maxDefaultFloatFormatBytes`
  default; this is at least what `%f`/`%e`/`%g` renders for any float whose
  integer part exceeds the precision. The constant
  `maxDefaultFloatFormatBytes = 512` already names the no-precision ceiling,
  so the precision arm keeps that as its floor.

**Task 2.3** — verify the four pinned format-charge tests and the pre-charge
guard test still pass.

- Files: existing tests `TestStrings_FormatEstimateTracksLiteralWidthRender`,
  `TestStrings_FormatEstimateTracksExplicitIndexRefusal`,
  `TestStrings_FormatRefusalCases`, `TestStrings_FormatEstimatorWalkIsUnboundedTracked`,
  plus the budget tests in `strings_budget_test.go` and
  `runtime.TestStrings_FormatRejectsBeforeAllocating`.
- Change: no edits to these tests; the runner of the change runs them once.

**Task 3.1** — extend the `format` `render assembly` row's proof.

- File: `internal/inventory/work_data.go`
- Symbol: row at lines 1295–1320.
- Change: reword the proof to name both causes and both owning changes
  (`core-value-walk-sharing-bound` and `format-mismatched-verb-bound`) on
  equal footing; add `format-mismatched-verb-bound` to the
  `invTrackedChanges` allowlist in
  `plugins/stdlib/inventory_registration_test.go` (already present at line 21;
  no edit required) — verify with the registration test.

**Task 3.2** — re-run the registration and source reconcilers; verify the row
still satisfies the unbounded-tracked rules.

- File: `plugins/stdlib/inventory_registration_test.go`
- Symbol: `TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork` (and the
  sibling `*FormatEstimator*UnboundedTracked` test).
- Change: no edit; the runner runs them.

**Task 4.1** — full verification floor.

- Commands (literal):
  - `go -C <worktree> build ./...`
  - `go -C <worktree> test -timeout 2m ./plugins/stdlib/... ./internal/inventory/...`
  - `go -C <worktree> test -race -timeout 10m -count=1 ./core/... ./plugins/... ./runtime/...`
  - `go -C <worktree> vet ./...`
  - `cd <worktree> && golangci-lint run ./plugins/stdlib/... ./internal/inventory/...`

### Patterns

- The estimator already uses `addFormatEstimate` for saturating addition; the
  new helper reuses it.
- The diagnostic-prefix overhead is small and constant; the helper lives in
  `strings.go` next to `badIndexFieldBytes`.
- The `%q` factor (4) is already a documented constant in the same file
  (`parseQuoteFactor`); the new constant `mismatchVerbPrefixBytes` follows
  the same shape.
- Test style matches `TestStrings_FormatEstimateTracksLiteralWidthRender` —
  table-driven, render derived from `fmt.Sprintf`, assertion is
  `require.GreaterOrEqualf` on `len(render)`.

### Test harness

Existing helpers used by the new test:

- `setupEnv(t)` — registered env from `strings_test.go`
- `sbStrings(n, s)` — `[]core.Value` of `core.String{V: s}` (1 KiB and 4 KiB
  operands built directly with `core.String{V: strings.Repeat("A", N)}`)
- `formatGoFunc(t, env)` — `core.GoFunc` for `format`
- `sbStringsFile`, `sbUnitCount` — file/loop constants
- `core.StringShallowBytes`, `core.MeterStringHeaderBytes`, `core.MeterScalarBytes`
  — byte-budget constants

### Risks

- **Over-charge:** the new path may inflate the estimate for a small
  `core.String` (e.g. 1-byte) below the current `n := int64(64)`. The
  `minFormatValueStringScalar = 24` constant at line 360 already handles that
  for `%s`; the new helper honours it too. No regression in the existing
  `TestStrings_FormatEstimateTracksLiteralWidthRender` cases.
- **Float integer-digit accounting:** the precision arm's
  `1 + integerDigits + 1 + precision` derivation must stay at or above the
  no-precision arm's `maxDefaultFloatFormatBytes = 512` for large magnitudes;
  the no-precision estimate stays the floor.
- **Allocation ledger pressure:** tightening the estimate may surface new
  pre-charge failures in tests that already pass because the renderer was
  previously charged through the shortfall. The Phase 5 floor catches it; if
  any test that previously passed starts failing because of the new
  accounting, the fix is documented as scope drift and routed through
  `zarchitect`.
- **Inventory row reorg:** the new proof text replaces the existing
  paragraph at lines 1302–1319. The format of the proof (Go string
  concatenation in a `+`-chain) must be preserved verbatim to satisfy the
  registration test's owner-token assertion.

### Verification commands

- `go test -timeout 2m ./plugins/stdlib/... ./internal/inventory/...` —
  narrow slice covering all touched packages.
- `go test -race -timeout 10m -count=1 ./core/... ./plugins/... ./runtime/...`
  — full floor (only on the Phase 5 join).
- `go vet ./...` — every package.
- `golangci-lint run ./plugins/stdlib/... ./internal/inventory/...` —
  lint scope.

### Blockers

None.

## Seam summary

NO-RED-WAIVER: false. NO-TESTER-WAIVER: false. Every task has an observable
contract.

## Implementation chunks

A single chunk covers tasks 1.1–1.3 (one new test), 2.1–2.2 (the estimator
fix in `strings.go`), 2.3 (re-run the four pinned tests as part of the
coder's verify), 3.1–3.2 (the inventory row proof extension + tracked-changes
verify) and 4.1 (the floor). One `go-test-writer` stage for the new tests,
then one `go-coder` stage for the production code, then one `go-tester`
stage to re-run the pinned tests against the new estimate.
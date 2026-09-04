## Why

`format` charges an estimate of its output before `fmt.Sprintf` runs, so that a
render too large for the remaining budget never happens. That guarantee holds
only while the estimate is an upper bound on what `fmt` will produce.

It is not one when the verb and the operand's type disagree. `fmt` renders a
mismatch by printing the whole operand inside a diagnostic — `%!d(string=…)` —
while the estimator returns a constant that does not depend on the operand at
all. The gap grows with the operand and with the number of directives, and an
explicit argument index lets one operand feed every directive.

Measured against the current estimator, a `core.String` operand and a `[1]`
index reused per directive:

| format | operand | estimate | rendered | ratio |
| --- | --- | --- | --- | --- |
| `%[1]d` | 1 KiB | 80 | 1036 | 12.9x |
| `%[1]d` | 4 KiB | 80 | 4108 | 51.4x |
| `%[1]c` | 4 KiB | 20 | 4108 | 205.4x |
| `%[1]d%[1]d%[1]d%[1]d` | 4 KiB | 272 | 16432 | 60.4x |

The ratio is linear in the operand size because the estimate is constant, so it
is bounded only by the budget the caller was given. The shortfall charge that
follows `Sprintf` keeps the *ledger* honest; it cannot unmake the allocation
that already happened.

This predates `stdlib-builtin-resource-migration` and is unchanged by it — the
figures above are byte-identical before and after that change. What that change
added is the row that declares this phase unbounded, and that row currently
attributes the unboundedness to a value walk over shared substructure. This
vector involves no sharing and no walk, so it needs an owner of its own.

## What Changes

- Make the format pre-charge an upper bound for a verb the operand's type
  cannot satisfy, sizing the directive as `fmt` will actually render it.
- Cover the same shape for a precision on a large-magnitude float, where the
  precision arm drops the default that covers float64's integer digits.
- Correct the `render assembly` inventory row so its proof names this cause and
  this change, rather than resting on the sharing walk alone.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: require the format pre-charge to bound a mismatched verb.

## Impact

- Affects `plugins/stdlib/strings.go` (`estimateFormatValueBytes`,
  `estimateFormatAllocBytes`) and the `format` row in
  `internal/inventory/work_data.go`.
- Changes what a given allocation limit admits: a format string that pairs a
  verb with an operand it cannot render will be charged for the diagnostic it
  actually produces, so a call near a limit can be refused where it previously
  rendered first and settled the ledger afterwards. No well-formed format string
  changes its result, its error text, or its charged total.
- Does not change any rendering. `fmt`'s output for a mismatch is unchanged;
  only the estimate that precedes it moves.

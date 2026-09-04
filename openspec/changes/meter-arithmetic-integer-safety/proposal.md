## Why

`stdlib-builtin-resource-migration` closed an integer-overflow bypass in the
allocation ledger: `core/metering.go` added a charge to its counter before
comparing against the limit, so a refused near-`MaxInt64` charge left the
counter at the ceiling and the next charge of any size wrapped it negative and
was accepted. An audit of that fix found two more places where the
resource-accounting arithmetic does not match what it models. Neither is
reachable as a limit bypass today, and both are byte-identical at the commit
that change branched from, so both are pre-existing and were left out of its
scope deliberately.

**The VM carries a second ledger with the same shape.** `core/vm/vm.go:386` does
`vm.reductions += n` and then compares against `vm.maxReductions`; `:431` does
the same for `vm.allocBytes`. No overflow guard, exactly the pattern that was
just removed from `core`. It is used only when `vm.meter` is invalid — the
`SetResourceLimits` path — where the counters start at zero and are never seeded
from a snapshot, which is why the attack that worked against `core` does not
reach it. That is a property of the current call graph, not of the arithmetic.

**The format estimator's argument index desynchronises from `fmt`.**
`parseFormatArgIndex` accumulates `index*10+d` with no ceiling, unlike
`parseFormatInt`, which now mirrors `fmt`'s own refusal. Because `10ⁿ ≡ 0 (mod
2⁶⁴)` for `n ≥ 64`, the wrap is fully attacker-controlled: the decimal digits of
`2⁶⁴+t` select argument `t-1` in the estimator while `fmt` refuses the literal
and renders `%!(BADINDEX)`. The estimator's argument pointer then disagrees with
`fmt`'s for the rest of the format string, and the pre-charge can be computed
against an argument far smaller than the one actually rendered.

Measured: `(format "%[18446744073709551618]s%s" 7 <1 MB string>)` estimates 41
bytes against a 1,048,589-byte render — a 1,048,548-byte shortfall, with
1,114,149 the worst across the sweep. The shortfall is bounded by the sum of the
argument sizes, all of them already charged, and `chargeFormatShortfall` settles
the difference after the render, so the ledger never under-charges and the limit
still trips. What it defeats is the narrower guarantee the pre-charge exists to
provide, and which `plugins/stdlib/format_test.go` pins: that `fmt.Sprintf` does
not run at all when its output will not fit.

## What Changes

- Guard the VM's own reduction and allocation counters against overflow, to the
  same standard `core`'s meter now holds: fail closed, and stay closed.
- Make `parseFormatArgIndex` mirror `fmt`'s refusal of an out-of-range argument
  index, as `parseFormatInt` already mirrors its refusal of an oversized width,
  so the estimator and `fmt` cannot disagree about which argument a directive
  refers to.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: the VM's resource counters cannot be wrapped past their limit.
- `stdlib-plugin`: the format pre-charge is computed against the arguments
  `fmt` will actually render.

## Impact

- `core/vm/vm.go` and `plugins/stdlib/strings.go`.
- No behaviour change for well-formed input: both defects require a literal that
  `fmt` itself refuses, or a counter driven past the int64 ceiling.
- A format string carrying an out-of-range explicit argument index currently
  renders `%!(BADINDEX)` and will continue to; only the pre-charge changes.

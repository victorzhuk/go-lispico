## 1. Pin the gap

- [ ] 1.1 Add a regression asserting `estimateFormatAllocBytes` is at least the rendered length for a verb the operand's type cannot satisfy, across the numeric, boolean and character verbs, over a `core.String` operand of a few KiB. Derive every expectation from `fmt.Sprintf`'s real output, not from a chosen number.
- [ ] 1.2 Extend it to an explicit argument index repeated across several directives, so one operand feeding many directives is pinned.
- [ ] 1.3 Add a regression for a precision on a large-magnitude float, asserting the estimate does not fall below the same verb without a precision.

## 2. Bound the estimate

- [ ] 2.1 Size a mismatched directive against the operand `fmt` will render into the diagnostic, keeping the existing saturating arithmetic so a large operand cannot wrap the estimate.
- [ ] 2.2 Keep the float precision arm at or above the no-precision arm.
- [ ] 2.3 Verify the four pinned format-charge tests and the pre-charge guard test still pass unchanged, and that no well-formed format string changes its result, its error text, or its charged total.

## 3. Correct the inventory

- [ ] 3.1 Extend the `format` `render assembly` row's proof to name this cause and this change alongside the existing sharing-walk reason, and add this change to the reconciler's tracked-change list.
- [ ] 3.2 Re-run the registration and source reconcilers; verify the row still satisfies the unbounded-tracked rules.

## 4. Verify

- [ ] 4.1 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.

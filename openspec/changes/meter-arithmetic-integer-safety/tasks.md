## 1. Pin both defects

- [ ] 1.1 Add a regression proving the VM's own counters can be driven past their limit and wrap: charge the reduction and allocation counters near the int64 ceiling through the `SetResourceLimits` path, and require the charge after a refusal to still be refused.
- [ ] 1.2 Add a regression proving the estimator and `fmt` agree on which argument a directive names: an out-of-range explicit index must not let the pre-charge be computed against a smaller argument than the one rendered. Derive the expectation from `fmt.Sprintf`'s real output, not from a chosen number.

## 2. Guard the arithmetic

- [ ] 2.1 Guard the VM's reduction and allocation counters so a refused charge leaves them still refusing and neither can go negative; verify the fast path stays a single add, as `core`'s meter does, and benchmark the contended path against the parent.
- [ ] 2.2 Make `parseFormatArgIndex` mirror `fmt`'s refusal of an out-of-range argument index; verify the four pinned format-charge tests and the pre-charge guard test still pass unchanged.

## 3. Verify

- [ ] 3.1 Verify no well-formed format string or evaluation changes its result, its error text, or its charged total.
- [ ] 3.2 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.

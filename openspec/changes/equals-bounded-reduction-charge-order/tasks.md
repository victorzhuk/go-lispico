## 0. Baseline

- [ ] 0.1 Record the defect as evidence at this change's base commit: for n = 9, 100 and 1000, the reduction units `equalsBounded` charges comparing two unequal builder-form maps, over at least three repeats each, showing the spread. Confirm the spread appears within a single process, and confirm both the equal-maps case and the trie-form receiver case are already stable, so the fix is scoped to the arm that actually moves.

## 1. Failing contract

- [ ] 1.1 Use existing-service-strict testing. Add `TestEqualsBounded_ReductionChargeIgnoresIterationOrder` in `core`: build two unequal maps above `hashMapSmallLimit` with the receiver left in builder form (`large.m != nil`, `large.root == nil`), compare them under a `BuiltinWorkBudget` repeatedly, and require every charged reduction count to be identical. Assert exact equality, not a bound. Verify it fails at base, and report how many repeats expose the spread reliably at each size so the committed test is not itself flaky.

## 2. Reproducible reduction charge

- [ ] 2.1 Make the charged count a function of the compared pair alone. Choose between a data-derived visiting order and charging from the collection's size on measured evidence, and record why the other was rejected — the two differ in whether the early exit survives, which is a cost worth measuring before choosing. Verify the boolean answer of every existing equality test is unchanged.
- [ ] 2.2 Verify budget exhaustion still behaves as before: a comparison that would exceed its budget SHALL still return the budget error rather than a boolean, and the depth cap SHALL still refuse a node past `DefaultMaxStructuralDepth` without charging for it.

## 3. Documentation and verification

- [ ] 3.1 State the reduction-side reproducibility property in ADR 0011 next to the allocation-side one, and record the changed reduction count in `CHANGELOG.md` under `[Unreleased]`.
- [ ] 3.2 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`, then `make build`, `make lint` and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass evidence.
- [ ] 3.3 Confirm no ADR 0008 threshold moves and run `openspec validate equals-bounded-reduction-charge-order --strict --json`.

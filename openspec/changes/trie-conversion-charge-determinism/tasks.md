## 0. Baseline

- [ ] 0.1 Record the defect as evidence at this change's base commit: for n = 9, 100 and 1000, the charge one `Assoc` against a retained `Set`-built receiver reports, over at least three repeats each, showing the spread. Confirm the same spread appears within a single process, since Go re-randomises map iteration per range, and confirm `eachRaw`'s other Go-map walk (`core/types.go:1040`) reaches no charged-by-traversal-order caller — its callers are `equals_bounded.go:69`, `depth.go:230` and `sortedEntries`.

## 1. Failing contract

- [ ] 1.1 Use existing-service-strict testing. Add `TestHashMap_ConversionChargeIsReproducible` in `core`: build one builder-form map above `hashMapSmallLimit`, take the conversion repeatedly against that same retained receiver, and require every reported charge to be identical; add `TestHashMap_ConversionChargeIgnoresBuildOrder`, which builds two maps of equal contents by inserting pairs in different orders and requires their conversion charges to match. Assert exact equality of charges, not a bound — reproducibility is the contract. Verify both fail at base, and report how many repeats are needed to expose the spread reliably at each size, so the committed test is not itself flaky.

## 2. Reproducible conversion

- [ ] 2.1 Make the conversion's charge a function of the map's contents alone. Choose the mechanism on the evidence gathered in the design stage — a data-derived insertion order, a single order-independent build, or charging the finished structure — and record why the other two were rejected. Verify the resulting trie is unchanged in shape and contents, that `Len`, `getByHashKey`, iteration order, equality and printing do not move, and that the tests of 1.1 go green.
- [ ] 2.2 Verify the charge stayed honest: the storage the conversion actually obtains is still accounted, not only what the finished trie retains, and state the measured before and after so the size of the move is on the record.

## 3. Documentation and verification

- [ ] 3.1 State the reproducibility property in ADR 0011 alongside the charge terms, note in ADR 0008 that the gate's allocation axis depends on it, and record the charge change in `CHANGELOG.md` under `[Unreleased]`; verify no unit is published that no ADR table row owns.
- [ ] 3.2 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`, then `make build`, `make lint` and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass evidence.
- [ ] 3.3 Compare `BenchmarkGoldsetParse` and `TestGoldsetVMAllocations` against the pre-change baseline in both evaluator modes, confirm no ADR 0008 threshold moves, and run `openspec validate trie-conversion-charge-determinism --strict --json`. Report allocation evidence separately from timing; latency deltas under this machine's measurement floor are not a verdict.

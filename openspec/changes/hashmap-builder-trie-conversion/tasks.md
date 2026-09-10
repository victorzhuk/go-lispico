## 0. Prerequisites and baseline

- [ ] 0.1 Confirm `reader-map-promotion-parity` is archived and its requirements are present, and re-record the pre-change fan-out figures the design quotes — charge, allocs/op and B/op for one `Assoc` against a retained receiver at n = 9, 100 and 1000, builder-form and trie-form arms — off a run at this change's base commit, so the CHANGELOG delta is a measured number rather than a quoted one.

## 1. Failing contracts

- [ ] 1.1 Use existing-service-strict testing. Add `BenchmarkHashMapFanOutAssoc` to `core` covering both arms at n = 9, 100 and 1000, and a test binding the new scenario "Repeated updates on a bulk-built map do not re-pay its conversion": k successive `Assoc` calls against one bulk-built receiver charge per update on the same order as the trie arm, asserted on charged bytes and `testing.AllocsPerRun`, never on timing; verify it fails today at n = 100 and n = 1000 and passes for the trie arm.
- [ ] 1.2 Add the cross-meter test the design's first-toucher-pays decision needs — one map value updated under two separate meters, exactly one of which is charged the conversion — and amend any exact-charge expectation that updates twice against one receiver and today sees the conversion charged both times; verify each amended expectation fails before section 2.

## 2. Conversion memo

- [ ] 2.1 Add the memo slot to `largeMap` and have `trieFromBuildMap` publish into it with a compare-and-swap, a lost race discarding its own copy; verify `Assoc` and `Dissoc` read it, that `large.root` and `large.count` are untouched, and that `getByHashKey`, `Len`, iteration order, equality and printing are unchanged for both large forms.
- [ ] 2.2 Clear the memo in `HashMap.Set` so a bulk write after a memoised update cannot leave a stale trie behind; verify a `Set`-then-`Assoc` sequence returns the same map a conversion-free path would.
- [ ] 2.3 Charge the conversion once per value rather than once per update; verify the wide-map cases still admit before allocating, that a refused conversion leaves no memo published, and that `-race` is clean over the concurrent-update path.

## 3. Documentation and verification

- [ ] 3.1 State in ADR 0011 that the trie conversion is a per-value charge settled by the first update, with the cross-evaluation attribution that implies, and record the charge change in `CHANGELOG.md` under `[Unreleased]` with the measured before and after from task 0.1; state the derived-state carve-out in ADR 0003 so the immutability invariant keeps its meaning; verify no unit is published that no ADR table row owns.
- [ ] 3.2 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`; record pass evidence.
- [ ] 3.3 Run `make build`, `make lint`, and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass/failure evidence.
- [ ] 3.4 Compare `BenchmarkGoldsetParse` and `TestGoldsetVMAllocations` against the pre-change baseline in both evaluator modes and report the fan-out benchmark's own before/after; report allocation evidence separately from timing and confirm no ADR 0008 threshold moves. Latency deltas under this machine's measurement floor are not a verdict — say so rather than claiming one.
- [ ] 3.5 Run `openspec validate hashmap-builder-trie-conversion --strict --json` and inspect final code/spec/doc consistency before archive.

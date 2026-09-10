## 0. Prerequisites and baseline

- [ ] 0.1 Confirm `reader-budget-enforcement` is archived and its requirements are present; record the current admitted totals for map literals at 1, 8, 9, 33 and 128 keys as the baseline this change moves.
- [ ] 0.2 Use existing-service-strict testing. Capture the divergence as a failing characterization: a map literal above `hashMapSmallLimit` read through `core.Read` holds `largeMap.root`, while the same contents through `HashMap.Set` hold `largeMap.m`.

## 1. Failing contracts

- [ ] 1.1 Extend `core/reader_map_parity_test.go` with representation assertions per fixture — promoted maps agree on storage form, flat and shared-tail lists agree across `listFlatThreshold`, and the collision fixture still reaches its collision arm; verify each assertion fails today for the promoted map sizes and passes for the small ones.
- [ ] 1.2 Amend the sealed construction-charge expectations in `core/reader_budget_accounting_test.go` to the entry-buffer schedule for promoted maps, keeping every `>=` floor and the self-derived allowance-boundary cases intact; verify the amended expectations fail before the code changes.

## 2. Reader map construction

- [ ] 2.1 Promote reader-built maps into the builder form in `Parser.mapSet`, matching `HashMap.Set`'s threshold and storage exactly; verify duplicate keys, mixed key types, collisions and `ReaderStats` are unchanged.
- [ ] 2.2 Charge the promoted entry storage on the deterministic growth schedule the small-form entry buffer already uses, admitting before allocating; verify the wide-map rejection still precedes allocation and that pooled and cold reads charge the same logical schedule.
- [ ] 2.3 Delete `newGuardedTrie`, `assocGuarded`, `assocCollisionGuarded` and `mergeEntriesGuarded` once no caller remains; verify `assoc`, `mergeEntries` and the collision paths they duplicated still carry every case, and account for each deleted symbol.

## 3. Documentation and verification

- [ ] 3.1 Amend ADR 0011's T6 construction row to state the entry-buffer term for reader map construction in place of the per-HAMT-node term, and add an unreleased `CHANGELOG.md` entry for the charge change; verify no unit is published that no ADR table row owns.
- [ ] 3.2 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`; record pass evidence.
- [ ] 3.3 Run `make build`, `make lint`, and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass/failure evidence.
- [ ] 3.4 Compare `BenchmarkGoldsetParse` and `TestGoldsetVMAllocations` against the pre-change baseline in both evaluator modes; report allocation evidence separately from timing, and confirm no ADR 0008 threshold moves. Latency deltas under this machine's measurement floor are not a verdict — say so rather than claiming one.
- [ ] 3.5 Run `openspec validate reader-map-promotion-parity --strict --json` and inspect final code/spec/doc consistency before archive.

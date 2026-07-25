## 1. Establish what today measures

- [ ] 1.1 Capture the current cells as the reference point, both modes,
      `GOMAXPROCS=2`, `BENCHTIME=200ms`, `count=10`, quiet machine
      (`uptime`; an unrelated corpus API server bursts on this box). `TMPDIR`
      outside `/tmp` — a quota failure at the link step reads like a test
      failure but is not one.
- [ ] 1.2 Confirm the parse share the profiling baseline reported (~36-38%
      CPU, ~75% allocation) still holds, so the expected drop in 3.1 has a
      number to be checked against.

## 2. Split the cells

Settled at 2.1: no public entry point evaluates pre-parsed forms — every
`runtime.Engine` method takes a source string, `EvalCached` is unexported on
an unexported type, and `internal/goldset` is a separate package. The
evaluation cells are therefore NOT split. Do not add public API and do not
bypass the Engine.

- [ ] 2.1 Add `BenchmarkGoldsetParse/<fixture>` measuring the reader alone
      over each fixture's source, via `core.Read` as `core/vm/bench_test.go`
      already does.
- [ ] 2.2 `BenchmarkGoldset/<fixture>` untouched — same name, same body.
      `internal/perfgate/tiers.json` keys on those names.
- [ ] 2.3 `Fixtures()` file I/O stays outside the timed loop.
- [ ] 2.4 `TestGoldset` untouched.

## 3. Quantify the parse share

- [ ] 3.1 Per fixture, report parse cost as a share of that fixture's
      existing evaluation cell, under both modes. This is the deliverable:
      the dilution stops being a whole-corpus estimate from a profile and
      becomes a measured per-cell number.
- [ ] 3.2 Record those shares in `docs/profiling-baseline.md`, replacing the
      ~36-38% corpus-wide estimate, and note that the evaluation cells
      remain mixed parse+eval measurements.
- [ ] 3.3 Parse cells show no eval-vs-vm difference; reader cost cannot vary
      by execution mode. A difference is a finding — report it rather than
      averaging it away.

## 4. Tiers

- [ ] 4.1 Assign a tier to every new parse cell in
      `internal/perfgate/tiers.json`, before any candidate results are used
      for a verdict.
- [ ] 4.2 Justify the choice against `tiers.json`'s own stated rule
      ("control-flow and dispatch-dominated" vs "collection/string-building"
      vs "startup-shaped"). Say plainly if none fits well rather than
      forcing one — inventing a fourth tier is a bigger decision than this
      change should take alone.
- [ ] 4.3 Existing eval-cell tiers unchanged.

## 5. Verify

- [ ] 5.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint` clean.
- [ ] 5.2 `go test ./... -count=1` — expect 2420 passed, 0 failed.
      `go test ./... -race` with `TMPDIR` set;
      `TestDecodeHashMap_Scaling` is a known pre-existing wall-clock flake
      under full-suite race load, filed separately — confirm it passes in
      isolation rather than treating it as yours.
- [ ] 5.3 `go test ./internal/goldset/ -count=1` — 27 passed, unchanged.
      Crossval `TestVMVsTreeWalker` 218 passed.
- [ ] 5.4 Capture a fresh paired baseline after the split and record it as
      the new starting point. Every prior baseline is already invalid from
      the `GOMAXPROCS`/`BENCHTIME` fix, so there is no non-regression
      comparison to make — say so rather than implying one passed.
- [ ] 5.5 Report the added wall-clock cost of doubling the cell count. If it
      is material against the paired run's budget, say so.

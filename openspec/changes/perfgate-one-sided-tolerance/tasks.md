## 1. Establish what the contract actually requires

- [x] 1.1 Read ADR 0008 before changing the gate, since the constant's own
      comment claimed the two-sidedness came from there. It does not: the
      section's governing sentence is "No cell may **regress** beyond its
      tier's budget", and the ADR rejects a standing improvement gate because
      it "punishes Evaluator improvements".
- [x] 1.2 Note the asymmetry inside one ADR line — "within 5% latency, bytes and
      allocation count non-increasing". Bytes and allocs were stated one-sided
      and implemented one-sided; only latency was read as a band.
- [x] 1.3 Confirm the spec does not enumerate the thresholds: it defers to
      "ADR 0008's thresholds", so the ambiguity lives in the ADR and the fix
      belongs there as well as in the code.

## 2. Find where two-sided is correct, before removing it

- [x] 2.1 `tiers.json` records a deliberate argument for two-sidedness on the
      `GoldsetParse/*` cells: their cost is mode-invariant by construction. That
      argument holds when the paired runs are the two evaluators of one commit —
      first authorization — and not when they are two releases.
- [x] 2.2 The concurrent tier's timed figure may be a throughput measure, where
      larger is better; the existing code comment says so. A one-sided check
      there would invert the meaning, so it stays two-sided. No cell is
      currently classified concurrent, so nothing is lost.
- [x] 2.3 Check no existing test pinned the two-sided behaviour, so the fix does
      not require inverting a test that meant something.

## 3. Implement

- [x] 3.1 `evaluateNonRegression` — latency regression only; bytes and allocs
      unchanged, still non-increasing.
- [x] 3.2 Route by mode: engine-sensitive and data-dominated use it in
      non-regression mode; data-dominated keeps the two-sided check under first
      authorization; concurrent keeps it in both.
- [x] 3.3 `evaluateStartup` takes the mode and applies its percentage arm
      one-sided against a previous release, keeping the absolute-overhead
      alternative.

## 4. Pin both directions in both modes

- [x] 4.1 A non-regression improvement passes, for engine-sensitive and
      data-dominated.
- [x] 4.2 A non-regression regression still fails.
- [x] 4.3 A faster candidate that allocates more still fails, on the byte check
      — the latency change must not relax it.
- [x] 4.4 A first-authorization data-dominated cell that improves beyond the
      bound still fails, so the mode-invariance argument stays enforced.
- [x] 4.5 A startup improvement passes under non-regression.
- [x] 4.6 `TestEvaluate_Fail_DataDominated` unchanged and still failing on its
      +8% regression, including the substring its assertion checks.

## 5. Record the direction so it cannot be re-inferred

- [x] 5.1 ADR 0008 states that "within 5%" bounds regression, names the two
      two-sided exceptions, and points at its own rejection of a standing
      improvement gate as the reason.
- [x] 5.2 `tiers.json`'s parse-cell rationale narrowed to first authorization.
- [x] 5.3 `nonRegressionTolerancePct`'s comment no longer claims ADR 0008
      mandates a two-sided bound.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean — 0 issues.
- [x] 6.2 `go test ./... -count=1` 2451 passed.
- [x] 6.3 `tiers.json` parses; its comment edit did not break the JSON.
- [x] 6.4 Re-judged today's four candidate runs with the fixed gate:
      `cache-hit-skips-expansion` 20 PASS / 6 FAIL → **26 PASS / 0 FAIL**;
      `idempotent-macro-rebind` 4 → 2 failures;
      `compile-toplevel-defmacro` 4 → 2; `incremental-depth-check` 7 → 7,
      unchanged because every one of its failures was already a positive delta.
      No verdict moved from FAIL to PASS on a regression.
- [x] 6.5 What remains is positive-delta noise from comparing separately
      captured files, which `Paired release run` already forbids by requiring
      one interleaved job. Every such failure investigated today cleared under a
      paired re-measure at `-count=12`. The earlier claim that the
      `GoldsetParse/*` cells are too noisy for the gate is **withdrawn** — the
      cause was the comparison method, not cell size. No absolute-delta floor
      added: it would mask real regressions on fast cells to compensate for a
      measurement practice, and the startup tier's existing absolute arm is the
      only place ADR 0008 grants that escape.

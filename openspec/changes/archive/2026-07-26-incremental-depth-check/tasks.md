## 1. Establish whether the defect is still live

- [x] 1.1 Stages B and D added narrow depth guards, so the first question was
      whether anything superlinear survived them. Benchmark consing a
      collection onto an accumulator against the scalar case as a control, at
      n=200/400/800, `GOMAXPROCS=2`, `-benchtime=200ms`, `TMPDIR` outside
      `/tmp`.
- [x] 1.2 It is live. Allocations grow ~1.95× per doubling in both columns;
      collection-element *time* grows 2.68× then 3.50×, and at n=800 costs
      10.8× the scalar case for 2× the allocations. Linear allocations with
      superlinear time isolates the cost to the walk rather than the work.

## 2. Choose the fix on the evidence

- [x] 2.1 The performance program planned per-node `depth`/`deepBytes`
      annotation. Rejected: it grows `listNode` 32 → 48 bytes, and
      `persistent-map-structure` measured that exactly this 16-byte inline
      growth is visible to the gold set's byte check. Every list would pay
      memory for a check most lists never fail.
- [x] 2.2 Take the inductive route instead. `depth(C+e) = max(depth(C),
      1 + depth(e))` and `depth(C)` was checked when `C` was built, so the
      check can be bounded by `e`. Costs no memory.
- [x] 2.3 Confirm this is not a new assumption: the scalar carve-out already
      trusts the extended collection's validity, since it skips the check
      entirely. The change makes `cons` behave the same way whatever the
      element's type, rather than only when the element happens to be a scalar.

## 3. Implement

- [x] 3.1 `core.CheckNestedElementDepth` checks a value as an element one level
      inside a container; both entry points route through one shared
      limit-resolution helper so the configured limit cannot diverge.
- [x] 3.2 `chargeConsResult` uses it per added collection element.
- [x] 3.3 Fresh builders keep the full walk — they have no already-validated
      container to induct from.
- [x] 3.4 Comment the WHY at both sites, including the induction, since the
      reasoning is what makes the narrowing safe.

## 4. Prove the bound still holds

- [x] 4.1 `TestCollections_ConsDepthEscalationStillCaught` — a loop wrapping
      its accumulator via `cons`, and via `conj`, both still fail with
      `ResourceLimitError: structural depth limit 1024 exceeded`. This is the
      shape where the new element *is* the accumulator, so checking the element
      is exactly checking the result.
- [x] 4.2 `TestCollections_ConstructionDepthLimitNotCatchable` unmodified and
      green — escalation through the fresh builder `list` is untouched by this
      change.
- [x] 4.3 State plainly what is no longer re-detected: a too-deep structure
      already inside the extended collection. It can only exist unvalidated,
      and the scalar carve-out already admits exactly that value. Recorded in
      the proposal rather than left for someone to find.

## 5. Measure

- [x] 5.1 Same parameters as 1.1: n=200 −51%, n=400 −67%, n=800 **−81%**
      (1583.1µs → 305.1µs). Growth per doubling 2.68×/3.50× → 1.81×/2.04×.
- [x] 5.2 Allocations unchanged (886/1687/3287 against 886/1687/3289), so the
      walk moved and nothing else did.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean — 0 issues.
- [x] 6.2 `go test ./... -count=1` 2446 passed; `-race` 2448 passed.
- [x] 6.3 Crossval `TestVMVsTreeWalker` 218 passed.
- [x] 6.4 `go test ./internal/goldset/ -count=1` 27 passed in both modes.
- [x] 6.5 `cmd/perfgate`: allocations identical on every cell, geomean +0.00%,
      as predicted — no fixture accumulates collections. The gate reported 19
      PASS / 7 FAIL on latency, and unlike previous runs the deltas skewed
      consistently *positive* (+7% to +10.8%) across cells that all build
      collections, which is a plausible-regression pattern rather than the
      usual bidirectional noise. Investigated rather than dismissed: the
      hypothesis was that turning `CheckConstructionDepth` into a wrapper added
      a call frame to a hot path. `go build -gcflags='-m'` shows both wrappers
      inline, leaving the same single call to `checkDepthAt`, and a focused
      paired capture at `-count=12` cleared it — `counter-closure` p=0.266,
      `merge-config` p=0.932, `queue-promote` p=0.977, and `loop-sum` actually
      **−13.27%** (p=0.000). The gate's figures came from comparing separately
      captured files.

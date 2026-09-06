## Why

`release-gate-baseline-comparability` made the gate refuse a latency conclusion
across a change of runner and keep allocation counts and allocated bytes
enforced, on the stated reasoning that those two "are properties of the program,
not of the machine" (`openspec/specs/consumer-release-gate/spec.md:383-384`).
The first dispatch of the fixed gate refutes the second half of that sentence.

**Run 33985766146**, `workflow_dispatch` on `d938e93`, resolved non-regression
against `v0.12.0` and reported 26 latency cells inconclusive naming both runners
— the intended behaviour, working. It then failed one cell:

```
Goldset/queue-promote-2: FAIL (bytes increased by 0.05% (+8 B/op against a 8 B/op allowance))
```

The measured evidence says that failure is not a code change:

- `allocs/op` reads **174 on all twenty samples**, both arms. The exact axis
  moved by nothing.
- Iterations per sample differ by **2.8x**: the Intel baseline ran ~12,300, the
  AMD candidate ~4,400, at the same `BENCHTIME=200ms`.
- Baseline median 16590.5, candidate median 16599 — a delta of 8.5 against a
  stated allowance of 8.

`B/op` is total bytes divided by iterations. A fixed per-run allocation therefore
amortises about 2.8x heavier over 4,400 iterations than over 12,300, and the
denominator is machine speed — a property of the machine. Allocation counts carry
no such term and stayed exactly equal, which is what makes them genuinely
runner-portable.

The per-cell allowances compound this. Each was sized from **within-run** spread
on a single 200ms profile (`internal/perfgate/tiers.json`), which measures
sampling wobble on one machine at one iteration count. Nothing in that derivation
covers a cross-runner change in the denominator, so the allowances are structurally
unable to bound the effect they are now being asked to absorb.

This is not a case for widening an allowance. `release-gate-baseline-comparability`
established that an allowance SHALL NOT be widened to admit a measured regression,
and widening here would also be sizing a number against noise rather than evidence.
The bound is not too tight; the axis is not comparable.

**Consequence while this stands.** `Store VM baseline on the authorized release`
is guarded by the implicit `success()` on its `if:`, so a failing gate stores no
baseline. `v0.13.0` would therefore publish nothing, and `v0.14.0` would again be
compared against `v0.12.0`'s Intel data — the same cross-runner pair, the same
cell, indefinitely. The gate cannot advance its own baseline until the bytes axis
stops failing on a difference it cannot attribute.

## What Changes

- Report allocated bytes as **inconclusive** across differing runner identities,
  exactly as latency already is, naming both runners. Allocation counts stay
  enforced unconditionally — they are exact per-op integers with no iteration-count
  term.
- Keep allocated bytes fully enforced whenever the identities match, against the
  same per-cell allowances, unchanged. This change narrows when the bytes axis is
  consulted, never how strictly.
- Amend the spec sentence that asserts allocated bytes are a property of the
  program, and ADR 0008's companion note, to record which axis survives a change
  of runner and why.
- Record in `CHANGELOG.md` that a cross-runner release is now gated on allocation
  counts rather than on allocated bytes.

## Impact

- Affected specs: `consumer-release-gate`
- Affected code: `cmd/perfgate/main.go` (the cross-runner branch), `internal/perfgate`
  (only if the axis decision moves below the entry point), `docs/adr/0008-consumer-performance-gate.md`,
  `CHANGELOG.md`
- Affected evidence: none. No committed corpus, no `verdict.txt`, and no allowance
  value changes.
- Release: `v0.13.0` is held untagged until the gate passes, so that the release it
  authorizes actually stores a baseline.

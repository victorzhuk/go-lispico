## Context

Retained-state accounting (ADR 0012) tracks per-cell backing bytes and an env
aggregate that gates `MaxRetainedBytesPerEnv`. `MergeInto` is the hot-reload
commit path: a freshly-evaluated child env is merged into `rootEnv`. The merge
releases old backing through a meter, which is why it drops the lock mid-way —
`ReleaseRetained` is an external call the author did not want to hold the lock
across. That lock gap is the race.

## Goals / Non-Goals

- Goal: merge is atomic with respect to concurrent `Set`/`Bind`/`def` on the
  target; no silent lost update.
- Goal: the env retained aggregate stays exactly equal to the sum of live cell
  `retainedBytes` across arbitrary overwrite sequences.
- Goal: multi-meter settlement is all-or-nothing and deterministic.
- Non-Goal: redesigning the meter interface or ADR 0012's accounting model.

## Decisions

### Merge atomicity — version-revalidate over long lock

Two viable options:

1. **Hold `target.mu` for the whole merge**, moving the `ReleaseRetained` calls
   to after the committed section (collect releases under lock, apply commits
   under the same lock, then release backing after unlocking). Simple, but holds
   the lock across the full merge.
2. **Optimistic revalidation:** keep the compute/unlock/relock shape but, under
   the final lock, re-check each target cell's `version` against the version
   captured when the commit was computed; if it changed, skip that cell's blind
   overwrite (the concurrent writer won) or recompute it. No lost update.

Choose (2) if the lock-hold time of (1) measurably harms concurrent throughput
on the shared engine; otherwise (1) is simpler and less error-prone. Either way,
the doc comment must match the chosen guarantee. Recommendation: start with (1)
(correctness-simple) and fall back to (2) only if a concurrency benchmark shows
contention.

### Aggregate consistency

In the overwrite branch, adjust `e.retainedBytes += (src.retainedBytes −
cell.retainedBytes)` and the analogous slot delta, so the aggregate tracks the
true per-cell sum. Add an invariant-check test helper that asserts
`e.retainedBytes == Σ live cell.retainedBytes` after a merge sequence.

### Settlement order + rollback

`settleRetained` sorts its per-meter groups by a stable key before charging.
On a charge failure at group N, it releases the charges applied to groups
1..N−1 (a `ReleaseRetained` of the amount just charged to each) before returning
the error, and does not partially run the per-cell finalization loop — either all
cells get their `retainedMeter` set or none do.

## Risks / Trade-offs

- Option (1)'s longer lock hold is the main trade-off; the `-race` suite and a
  concurrent-merge-vs-Bind test are the gate. The existing shared-engine
  concurrency characterization pins must still hold.
- Rollback assumes `ReleaseRetained` of a just-charged amount is exact; verify
  the meter contract supports symmetric release.

## Migration

None. Corrects internal accounting and a race; no API or observable-value change
for correct single-threaded use.

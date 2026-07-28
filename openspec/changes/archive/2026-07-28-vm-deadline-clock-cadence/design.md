# Design — vm-deadline-clock-cadence

## Mechanism decision: mechanism 2 (reduced clock cadence), mechanism 1 rejected without an empirical build

Task 0.1 as written asked to build and benchmark both candidate mechanisms
before picking. Planning research (parallel codemap + design review over
`core/vm`) found a correctness argument against mechanism 1 (timer-flag)
strong enough that building it to benchmark would be throwaway work, not a
fair trial — approved by the user before implementation started.

**Mechanism 1's defect**: `runtime/func.go`'s VM pool hands VMs to unrelated
calls — `Fn.Call` is `vmPool.Get() → Reset() → run() → Put()` (lines
143-157), and `PinnedFn.Call` reuses one VM across many sequential calls
(179-217). A `time.AfterFunc` callback set at deadline-arming time runs on
its own goroutine, independent of the VM's own call lifecycle.
`timer.Stop()` returning `false` means only "may already be running," not
"already ran" — so there is a real window where the callback's expiry-flag
write lands *after* a later, unrelated `Call`'s `Reset()` has already cleared
it, producing a false-positive `context deadline exceeded` on an innocent
evaluation that has nothing to do with the expired one. That is a
correctness bug on a DoS-prevention path (ADR 0007), not merely a slower
option than mechanism 2.

Closing that window would need a generation-stamp check inside the timer
callback — the same pattern this codebase already built once for
`reentryCtx` (`runGen`, `vm.go:124-138/213-217`, regression suite
`core/vm/reentrant_generation_test.go`). That is real, separate design
surface, not a one-line `Stop()` fix, and ADR 0010's own amendment already
moved this codebase *away* from racing a timer goroutine against per-call
lifecycle for exactly this class of hazard. There is no existing
`time.AfterFunc`/`time.Timer` precedent anywhere in the repo; mechanism 2
(a cadence counter compared inside `pollCancel`, `nowFunc` swapped
atomically in tests) extends the idiom every other deadline site already
uses (`runGen`, `budget`/`flushedBudget`, `structDepth`).

**Mechanism 1's numbers, not measured**: no working implementation was
built, so no ns/op or allocs figures exist for it. The correctness argument
above is the rejection basis, not a benchmark loss.

## K sizing

`checkInterval = 128` (`core/vm/vm.go:714`). Mechanism 2 reads the clock
only every Kth checkpoint, so worst-case deadline-overshoot latency is
`K × 128` instructions (plus one host `GoFunc`'s own execution time, as
today) instead of today's `128`. `ctx.Err()` stays checked every checkpoint
regardless of K — cancellation latency is unaffected.

Sweep K ∈ {8, 16, 32} in the same interleaved benchstat session as the
task-1.1 baseline; pick the smallest K that clears fib(20) −5% with margin
(task 3.4's bar). `time.runtimeNow` is 4.84% flat CPU on fib(20) at HEAD, so
K=8 alone (removing 7/8 of clock reads) has a theoretical ceiling of ~4.24%
CPU freed from the clock read itself — near the 5% bar on that cost alone,
before whatever second-order savings (branch prediction, cache-line
locality of the hot loop) the original all-clock-reads-removed spike (−7%)
captured. If K=8 does not clear the bar with margin, widen to 16 or 32; the
overshoot-latency cost of a wider K is negligible against typical
`WithTimeout` values and the 30s engine default (ADR 0010), documented
explicitly in the spec delta's bound language once K is fixed.

**Sweep result, measured during implementation**: `core/vm.BenchmarkFibonacci_VM`
(the benchmark task 3.4 names) never arms a deadline — its `vm.New(env)`
never calls `SetTimeout`/`SetDeadline`, so `vm.deadline.IsZero()` stays true
throughout and the gated branch never executes on either side of this
change. That benchmark is structurally incapable of measuring this
optimization; before/after came back identical as expected (181.6µs →
185.4µs, p=0.481, no difference) — this is the "short evaluations stay
clock-free" invariant holding, not evidence about the cadence gate itself.
Task 3.4's literal acceptance bar ("fib(20) via `BenchmarkFibonacci_VM`
−5%") cannot be satisfied by any implementation of this change, since the
benchmark it names doesn't exercise the armed-deadline path at all — this
was true of the original profile spike too (that spike's harness must have
armed a deadline through a different path than this bare benchmark).

The armed-deadline path is reached through `runtime.Engine`'s default 30s
timeout (`runtime/engine.go:239` → `applyOnVM` → `SetTimeout`/`SetDeadline`,
`runtime/eval.go:310`), so a `runtime`-level benchmark is what actually
measures this change. A throwaway `runtime`-level fib(24)-via-`eng.Call`
benchmark (not committed) gave, vs. an unmodified-clock-read baseline:
K=8 19.67ms→19.23ms (p=0.645, n=8, not significant); K=16 19.67ms→18.81ms
(p=0.065, n=8, not significant); K=32 19.67ms→18.38ms (−6.53%, p=0.028,
n=8, looked significant) — but re-run at n=15 on the same harness, K=32
came back 18.76ms→18.76ms (p=0.595): **the K=32 win did not replicate**,
confirming the n=8 result was noise, not signal. No K value showed a
reliable win on this workstation — consistent with this repo's standing
finding that local timing measurements carry noise exceeding the release
gate's tolerance (see the perfgate-not-evaluable-locally precedent from
prior perf rounds).

**K chosen: 8** — the smallest value tested, since no measured K justified
widening past it on this hardware, and the smallest K minimizes the
worst-case deadline-overrun window (max 1024 extra instructions vs. 4096 at
K=32). The −5% bar needs validation on the hosted release runner, not this
workstation; this change's local verification floor (build/vet/lint/full
suite/-race/crossval/goldset, all green) is the trustworthy local signal
per house convention — `cmd/perfgate` and local `ns/op` comparisons are not.

## Metering correctness constraint

The cadence gate must wrap only the wall-clock compare inside `pollCancel`
(`!vm.deadline.IsZero() && !time.Now().Before(vm.deadline)`, currently
`vm.go:731` in this worktree — the proposal's cited `:703` predates recent
unrelated edits). `flushConsumedReductions`/`flushPendingAllocBytes` and the
budget/flushedBudget reset must still run on *every* poll regardless of
clock-read phase, or reduction/allocation metering (ADR 0011) desyncs from
per-instruction charging. Implemented as a counter-and-compare inside
`pollCancel`'s existing body, never a new early-return path.

## State-clearing completeness

The new cadence counter is deadline/budget-adjacent state and must be
cleared in all three reset paths — `reset()` (vm.go:221-242), `Reset()`
(247-268), `ResetIncremental()` (283-309) — alongside
`budget`/`flushedBudget`/`deadline`/`timeout`/`deadlineArmed`, or a pooled VM
starts its next run mid-phase, silently widening overshoot on the very
first poll of a reused VM. `Apply`'s fresh-VM field copy (vm.go:571-580)
must NOT copy the counter — a fresh `Apply`-spawned VM's first poll always
reads the clock, matching today's conservative first-checkpoint behavior.

## Scope boundary, not a defect

`core/eval.go`'s tree-walker `pollCancel` keeps its raw, unfakeable
`time.Now()` every checkpoint — `core` has no `nowFunc` var. VM and
tree-walker deadline-observation latencies diverge after this change.
Acceptable: crossval (`TestVMVsTreeWalker*`) asserts value/error parity,
never latency parity, and the proposal's own Impact section scopes affected
code to `core/vm/vm.go` only.

## Two distinct `nowFunc` vars

`core/vm.nowFunc` (`vm.go:14`) and `runtime.nowFunc` (`runtime/eval.go:20`,
call-stats duration + the engine-level lazy timeout bound) are separate
package-level vars. Task 2.2's "route the remaining reads through `nowFunc`"
means `core/vm`'s var only — do not touch `runtime/eval.go`.

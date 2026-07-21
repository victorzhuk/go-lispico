# Measured and rejected — vm-fused-native-ops

Status: **blocked** (burden of proof not met). The implementation is complete
and green; the measurement gate rejected adoption. Raw benchstat output:
`benchstat-1s.txt` (first pair, `-benchtime=1s -count=10`) and
`benchstat-2s.txt` (inconclusive-rerun pair, `-benchtime=2s -count=10`).
Measured implementation is preserved unmerged on branch
`zapply/vm-fused-native-ops`.

## Gate results

| Criterion | Result |
|---|---|
| `go test ./...`, `-race`, crossval green | PASS |
| `GOLDSET_MODE=vm` B/op non-increasing | PASS (2s run: all cells `~`; the 1s run's +0.01–0.03% was one-time `freezeStack` backing-store amortization) |
| `GOLDSET_MODE=vm` allocs/op non-increasing | PASS (all cells equal) |
| Significant win on `Goldset/loop-sum` (engine-sensitive) | **FAIL** — run 1: −3.0%, p=0.645; run 2 (doubled benchtime): −0.5%, p=0.739. Two inconclusive runs → rejected per ADR 0008 burden-of-proof. |

Secondary signals did not rescue the case: run 1 showed wins on pipeline
(−17%), text-render (−19%), safe-parse (−8%), route-decision (−6%), but only
text-render (−9.1%, p=0.000) and merge-config (−6.3%, p=0.001) replicated at
2s — both data-dominated cells, not the engine-sensitive tier the change
targets. rule-load regressed +4.8% (p=0.019) at 2s, inside the startup tier's
5% tolerance but directionally unfavorable.

## Why the expected win did not materialize

The design estimated fib −5–15% from removing 4 operator `OpGetGlobal`
dispatches + push/pop pairs per call. Measured loop-sum delta is within noise
on both runs, so the eliminated dispatches cost less than estimated at the
current master baseline — plausibly because the site-cache + versioned-reads
work (env-cell-versioned-reads) already made the operator head read cheap,
shrinking the head resolution to near-dispatch cost before this change.

## Implementation notes (for any re-attempt)

- The plan's per-depth marker keying (`nativeOp[depth]`, `opFallback[depth]`)
  is unsound and was replaced during implementation: the first argument push
  lands on the freeze depth (push-time clearing erases the marker), and
  nested operators in argument position freeze at the SAME depth
  (`(+ (* a b) c)`: both `+` and `*` freeze at depth d). As-built mechanism:
  LIFO `freezeStack []freezeRec{depth, op, val}` — pops at fused dispatch,
  try-handlers snapshot `freezeDepth` for unwind. All rebind crossval
  scenarios, the Lisp-2 function-cell scenario, and the hand-built NativeOp*
  tests pass against it.
- Hand-built test chunks previously feeding fused ops with `OpGetGlobal` were
  converted to `OpFreezeNative` shape (17 Encode-form sites + 2 Emit-form
  sites in `core/vm/vm_test.go`); compiler opcode-sequence tests updated.

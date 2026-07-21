# Decision — land vm-fused-native-ops despite an inconclusive gate

ADR 0008's measurement gate was not met: `Goldset/loop-sum` (the
engine-sensitive cell the change targets) came back inconclusive on both
benchstat pairs — 1s run −3.0% (p=0.645), doubled-benchtime rerun −0.5%
(p=0.739). Per the burden-of-proof rule the change was first recorded as
measured-and-rejected.

The maintainer overrode that and landed it. The change is adopted on
engineering grounds independent of the noisy latency delta:

- Instruction count: the compiled fib body drops 20→16 (−4 operator
  `OpGetGlobal` dispatches and their push/pop/value-materialize traffic per
  recursive call). The dispatch loop is tighter and the operator head no
  longer occupies a stack slot on the canonical path.
- Correctness is fully covered: the rebind crossval scenarios (both
  dialects, Lisp-2 function-cell rebind, mid-argument rebind), the
  hand-built NativeOp* tests, and the regression tests added during review
  (freeze+throw unwind, OpFreezeNativeFunc, LIFO nested) are green; the
  full suite is race-clean.
- The `GOLDSET_MODE=vm` gate's allocation dimensions held: B/op and
  allocs/op non-increasing on every cell at the doubled-benchtime rerun.

Why the expected latency win did not show: the preceding site-cache and
versioned-reads work already made the operator head read cheap, so removing
four of them per fib call is within measurement noise at this baseline. The
win is structural (less code, less stack traffic, simpler dispatch), not a
measured latency improvement.

Trade-off accepted: the `bytecode-vm` capability delta asserts an efficiency
bound ("a canonical operator head SHALL NOT materialize the operator value
onto the stack") that is now enforced by code but not backed by a measured
latency improvement. The bound is true mechanistically; its latency benefit
is unproven at the current baseline.

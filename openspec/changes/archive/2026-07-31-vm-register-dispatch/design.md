## Context

Task 0's gate is still open: its verdict (`tasks.md`) records that the local
prong is not decidable on this box, pending a reference-runner measurement.
Tasks 1.1-1.3 are exempt from that uncertainty because the prototypes measure
candidate encodings against the landed stack form **in the same process**, so
machine state and toolchain cancel in the A/B. This document records the 1.3
decision from those prototypes. Section 2 remains gate-blocked and is not
designed here.

The proposal carried two candidate architectures:

- **A — register form.** Function-body instructions address a per-frame
  register window with three-address operands (Lua 5.0 model).
- **B — pre-decoded stream.** Per-site pre-decoded/specialized instruction
  streams (`[]func`-shaped, the Vitess/goja shape), buying operand
  pre-decoding without a full register rewrite.

Both were hand-compiled for the fib body and measured. All prototypes live in
`core/vm/dispatch_proto_test.go`; the only other change is a constant pin in
`core/vm/opcode_test.go`. No production file was touched.

### What was measured

Neither candidate can ride `(*VM).Run`. `Instruction` is a `uint32` — 8-bit
opcode plus **one** 24-bit operand (`core/vm/chunk.go:12-23`) — with no room
for a second or third operand field, and the dispatch loop has no case that
would interpret register operands. Adding either is task 2.1/2.2. So each
prototype defines its own instruction type and its own dispatch loop entirely
inside the test file.

That creates a confound: a bespoke ~10-opcode loop handling exactly one
function was being compared against production's general ~45-opcode
interpreter, which additionally carries error paths, try/catch, closure
machinery, and generation checks. The experiment was therefore built as a
**2×2 factorial** over the two axes being conflated — operand addressing and
dispatch mechanism — plus the production baseline:

| arm | addressing | dispatch |
| --- | --- | --- |
| `stack` | stack | switch, general (production `vm.Run`) |
| `stack-specialized` | stack | switch |
| `stack-specialized-predecoded` | stack | closure array |
| `register` | register | switch |
| `predecoded` | register | closure array |

Every prototype is derived mechanically from the real compiled chunk through a
shared `parseFibBody`, asserting each live opcode and pulling constants and
symbols from the chunk's own pools, so the register and stack IRs cannot
disagree about what the compiler emitted, and a fusion change breaks the
pinned disassembly test loudly.

**Interleaving.** `-count=N` does **not** re-invoke a parent benchmark: the
parent is walked once to discover its `b.Run` leaves, and `-count` then
repeats each leaf N times back-to-back. A parent that registers five arms and
is run under `-count=20` therefore produces five contiguous blocks of twenty,
never an interleaving — which leaves any monotonic thermal or frequency drift
fully confounded with arm identity. The harness instead performs the
repetition **inside** the parent, reshuffling arm order on each of 20 reps and
running with `-count=1`, so every arm is visited once per rep in a fresh
random position. Reps are pooled for analysis with
`benchstat -col /arm -ignore /rep` — the leading slash is required to name a
benchmark-name key; `-ignore rep` silently leaves every rep its own
single-sample row. Each decomposed pairwise delta below needs its own
`-filter` scoping, since benchstat picks a single base column per
invocation.

`GOMAXPROCS=2`, 20 interleaved reps, go1.26.5:

| arm | sec/op |
| --- | --- |
| `stack` (production) | 1.948m ± 2% |
| `predecoded` | 1.214m ± 2% |
| `register` | 1.237m ± 1% |
| `stack-specialized` | 1.269m ± 3% |
| `stack-specialized-predecoded` | 1.293m ± 2% |

Decomposed. Each row states how much slower the *worse* arm is, taking the
faster arm as base, so magnitudes are comparable across rows:

| effect | holding constant | slower arm | penalty | p |
| --- | --- | --- | --- | --- |
| addressing | switch dispatch | stack | 2.55% | 0.000 |
| addressing | closure dispatch | stack | 6.51% | 0.000 |
| dispatch | register addressing | switch | 1.90% | 0.002 |
| dispatch | stack addressing | — | not detected | 0.081 |
| specialization | — | general loop | ~35-38% | 0.000 |

(Reading the same comparison from the other base gives a smaller percentage —
the closure-dispatch addressing row is −6.11% with the stack arm as base.
Quote one convention or the other, not a mix.)

Minimum detectable effect at n=20, Welch SE, α=0.05 two-sided, 80% power:
**1.79%** on the register-addressed dispatch pairing and **2.39%** on the
stack-addressed one. The observed 1.90% resolves; the stack pairing's ~1.6%
sits below its floor. Read the p=0.081 row as "no effect larger than ~2.4%
was detectable," not as demonstrated absence.

## Goals / Non-Goals

**Goals:**

- Record which candidate architecture survives measurement, and on what
  evidence.
- Separate the encoding question from the confounds initially bundled into it.
- Leave the losing candidate's numbers in the record.

**Non-Goals:**

- Designing the production register compiler, dispatch loop, or validation.
  That is section 2, gated on task 0.
- Re-opening or closing task 0's gate. These prototypes are an internal A/B and
  say nothing about the cross-engine comparison.
- Any conclusion about shapes other than fib — no loops, closures, or
  collection operations were measured.

## Decisions

### 1. Both effects are real, both are small, and neither justifies section 2

Register addressing wins consistently in both dispatch modes — 2.55% under
switch, 6.51% under closure, both p=0.000. Pre-decoded dispatch wins 1.90%
under register addressing (p=0.002) and is not detectable under stack
addressing (p=0.081). The best combination measured, register addressing with
closure dispatch, is 4.5% ahead of the stack-addressed switch-dispatched
baseline.

Set against what the proposal projected — fib −20-30% — and what section 2
costs (a wire-format break, a compiler register allocator, a `Validate`
rewrite, a second dispatch loop, and every test asserting today's disassembly
shape), a single-digit-percent architectural ceiling on the shape most
favourable to the measurement is a clear signal not to proceed.

This is a "do not build on this evidence" conclusion, not a refutation of
register bytecode in general. The effects are real; they are just an order of
magnitude smaller than the change was premised on.

### 2. Candidate B (pre-decoded stream) is rejected as a standalone mechanism

Pre-decoding pays only when combined with register addressing (1.90%), and is
undetectable on its own (p=0.081 under stack addressing). Its appeal was that
it could be adopted **without** the register rewrite — keeping today's
`Instruction` encoding and stack addressing, needing no wire-format break, no
register allocator, no `Validate` rewrite. That cell,
`stack-specialized-predecoded`, is the **slowest** of the four specialized
arms at 1.293m.

So the cheap version of B buys nothing, and the version of B that does pay
requires exactly the expensive rewrite it was meant to avoid.

### 3. Three measurement defects reversed this conclusion twice — that is itself a finding

An earlier round reported register beating stack by 14.0% and 17.1%, both
p=0.000, replicated across two sessions. A later round, after one fix, reported
a flat null. Neither was correct. Three defects were found by adversarial
review of the benchmark, each conclusion-changing:

1. **`pushFrame` idiom asymmetry.** `regInterp.pushFrame` appended a literal
   and patched fields in place; `stkInterp.pushFrame` built a zeroed local,
   filled it, then copied the whole ~80-byte struct into the slice — an extra
   full-struct copy per call, ~21.9k times per fib(20) op, paid only by the
   stack-addressed arms.
2. **Fixed arm ordering.** All five arms ran in the same order every cycle,
   with the register-addressed arm earlier in both addressing comparisons.
3. **The interleaving never existed.** The harness was built on the assumption
   that `-count` re-invokes the parent and interleaves its sub-arms. It does
   not (see Context). Every round prior to the last measured contiguous
   per-arm blocks, leaving drift confounded with arm identity — including the
   round that produced the 14-17% figure and the round that produced the null.

Attributing the movement, tracked on `stack-specialized`: 1.449m with both
defects live, 1.232m once `pushFrame` was normalized but still block-ordered,
1.269m with genuine interleaving. So the `pushFrame` asymmetry accounts for
most of the shift and the block-position artifact for roughly three points on
top — and the intermediate reading that looked like a clean null was itself
distorted, in the opposite direction from the original bug. Both fixes were
necessary; neither alone explains the movement.

Lessons worth carrying past this change:

- **Session replication does not establish validity.** The 14-17% result
  replicated across two sessions and was still an artifact. Replication
  controls for machine drift, not for asymmetric work between arms.
- **A `p=0.000` is a statement about within-run consistency,** which a
  systematic per-arm bias satisfies perfectly.
- **Verify the harness does what you think.** The interleaving assumption was
  never checked against `testing`'s actual behaviour until the third review
  round; one glance at the raw output's arm ordering would have caught it
  immediately, and that check now belongs in any A/B done here.
- Any future A/B in this repo comparing two hand-written execution paths must
  equalize allocation and copy idioms between them, randomize arm order, and
  confirm interleaving from the raw output before its numbers are quoted.

### 4. The proposal's stated mechanism is falsified independently of the timing result

`instr/op` is **identical at 175.1k across every arm**, because each prototype
is a 1:1 transliteration. That is the finding, not an artifact.

The proposal argued the remaining fib gap after fusion is "itemizable as
stack-shuffle dispatches (push/pop/GET_LOCAL traffic) that fusion cannot fully
remove," grounded in the literature's >47% instruction elimination (Shi,
Casey, Ertl, Gregg, ACM TACO 2008). The pinned disassembly refutes this: the
13-instruction fib body contains **exactly one `GET_LOCAL`** and **zero
separate constant-load instructions**. All three of `(< n 2)`, `(- n 1)`,
`(- n 2)` are single `OpFusedNativeOp` instructions reading the local straight
off the frame and the constant straight from the pool, never pushing either
operand first.

`compiler-branch-arith-fusion` already harvested the instruction-count axis.
There is no shuffle traffic left for a register form to eliminate, which is
why the surviving effect is a small per-instruction constant factor rather
than the projected instruction-count collapse. The proposal's "fib −20-30%"
Impact claim is not supported, and its justification needs rewriting before
section 2 is reconsidered.

### 5. The only large effect measured is generality, and it is not an architecture choice

Production at 1.948m against 1.214-1.293m for the specialized arms is roughly
36% — an order of magnitude above either architectural axis. That is the cost
of the general interpreter's breadth: the ~45-case switch, error paths,
try/catch, generic `core.Value` apply/call dispatch, closure and capture
machinery, plus (uncontrolled here) the difference between the production site
cache and the prototypes' `GetCanonical` path.

This is not a recommendation to specialize — correctness and generality are
why that machinery exists — and the figure is an upper bound, not a target. It
is a calibration: anyone reading a single production-vs-prototype number out
of this file would attribute to encoding what is almost entirely generality.

## Risks / Trade-offs

- **This box's numbers move between sessions** → Across rounds, the same
  `register` vs `predecoded` comparison read −4.50% (p=0.000), then p=0.512,
  then p=0.369, then −1.90% (p=0.002). Only the last was measured on a
  genuinely interleaved harness. This corroborates task 0's "not decidable on
  this box" verdict, and is why the effects above are stated with their
  detection floor rather than as point estimates to be quoted elsewhere.

- **Prototypes are not a clean isolation against the production baseline** →
  All four specialized arms resolve globals through `env.GetCanonical()`
  (mutex RLock plus map lookup) because the real site-cache path is unexported
  and unreachable from `vm_test`. This is *controlled* across the four
  specialized arms, so the addressing and dispatch comparisons are unaffected;
  it is *not* controlled against `arm=stack`, so the ~36% specialization
  figure bundles the resolution-path difference. That shared serialization —
  roughly 32.8k RLock plus lookup fences against a ~1.2ms budget — also
  plausibly compresses the dispatch axis, so 1.90% is a lower bound on what
  dispatch mechanism might be worth without it.

- **Tax parity is asserted, not verified against production** → `vm.VM`'s
  `budget`, `pendingAlloc`, and `flushedBudget` counters are unexported, so
  each prototype's poll cadence and allocation charges are checked against each
  other and against an analytical formula derived from the pinned disassembly,
  not against the live baseline VM's counters. `checkInterval` is pinned from
  an internal test (`core/vm/opcode_test.go`) so the cadence constant cannot
  drift silently.

- **fib-only, one shape** → fib is call-and-arithmetic heavy and already
  heavily fused. It is the shape where a register form has the *least* to gain
  on instruction count and a pre-decoded stream the least operand traffic to
  pre-resolve. A small effect here is weak evidence about loops, closures, or
  collection-heavy code, in either direction.

- **Bespoke opcodes are not a general instruction set** → `rOpCallFib`,
  `rOpGetFib`, and constant-folded `rOpSub`/`rOpLT` exist only for this
  function. A general register form's dispatch switch would be as wide as
  production's, reintroducing branch-prediction and I-cache pressure the
  prototypes never paid — so 2.55-6.51% is a ceiling, not a forecast.

- **The prototypes are a maintenance surface with no production consumer** →
  The pinned disassembly test fails if compiler fusion changes. That is
  intentional — silent drift would make these numbers unreproducible — but a
  future fusion change must update this file or delete it. If the gate closes
  as not-needed, this file should be deleted with the change.

## Open Questions

- Do the effects grow on shapes fusion has not flattened (loops, closures,
  collection traffic)? A few percent on fib does not settle the question for
  code with real operand-shuffle traffic, and section 2 should not be
  reconsidered on fib either way.
- Would the dispatch axis separate further without the shared `GetCanonical`
  tax? Answering it needs a production hook or an internal-package prototype.
- Task 0's gate still needs a reference-runner measurement, which
  `release-gate-activation` exists to stand up. Nothing here changes that.

# Tasks — vm-register-dispatch

## 0. Gate (blocking — nothing below starts until this fires)

- [x] 0.1 After vm-batched-ledger-charging, vm-deadline-clock-cadence,
      compiler-branch-arith-fusion, vm-global-call-inline-cache,
      vm-call-frame-fast-path, and engine-lean-call-boundary have all
      landed: interleaved five-row harness run (`-count=10`, one session)
      vs GopherLua and goja. Gate fires iff fib(20) bytecode still trails
      GopherLua on the reference runner, or by >5% locally. Record the
      verdict here either way; if the gate does not fire, close this
      change as not-needed with the numbers.

  **Verdict (2026-07-30): the local prong is NOT DECIDABLE on this box.
  The gate stays open pending a reference-runner measurement.**

  Preconditions are met — all six companion changes are resolved — but two
  of the six produced no fib win, so the proposal's projected "~1.6-2.0ms
  band" never had the inputs it assumed. Four landed (batched ledger,
  clock cadence, branch/arith fusion, lean call boundary);
  `vm-global-call-inline-cache` was archived **rejected at the perf
  gate**; `vm-call-frame-fast-path` kept only depth elision and the reset
  split — its same-chunk frame fast paths (2.1/2.2) were built and dropped
  on measurement with fib ~flat.

  Harness: `go-lispico-bench` (out-of-repo, `replace` onto this working
  tree), five rows × {lispico, GopherLua, goja}, one session per run,
  `-benchmem`, benchstat over the counts. Box: AMD Ryzen AI 9 HX 370,
  24 threads, `powersave` governor.

  Five-row run, `-count=10`, Go 1.26.5, sec/op:

  | Row | lispico | GopherLua | goja |
  | --- | --- | --- | --- |
  | Startup | 24.06µ ± 4% | 108.6µ ± 3% | 3.909µ ± 1% |
  | Call | 268.1n ± 1% | 141.0n ± 4% | 304.8n ± 6% |
  | Fib(20) bytecode | 3.587m ± 3% | 3.520m ± 6% | 3.859m ± 2% |
  | Callback | 376.6n ± 3% | 232.6n ± 8% | 1.222µ ± 2% |
  | Rule | 724.9n ± 3% | 1.270µ ± 11% | 1.376µ ± 6% |

  fib tree-walker 61.86m ± 4%. Confirmation, fib rows only, `-count=20`:
  lispico 3.574m ± 1%, GopherLua 3.640m ± 3%, goja 3.832m ± 1%.

  ### Why the prong is not decidable here

  **1. The fib(20) ratio is unstable across sessions on this box by far
  more than the 5% threshold** — every reading below is Go 1.26.5 on the
  same machine:

  | Session | lispico | GopherLua | ratio |
  | --- | --- | --- | --- |
  | 2026-07-22 (`results/averages.txt`) | 2.282m | 1.845m | 1.24 |
  | 2026-07-27 (figures quoted in this proposal) | 2.77m | ~1.95m | ~1.42 |
  | 2026-07-30 five-row, `-count=10` | 3.587m | 3.520m | 1.02 |
  | 2026-07-30 fib-only, `-count=20` | 3.574m | 3.640m | 0.98 |

  A prong thresholded at 5% cannot be read off a quantity that moves 44
  points between sessions.

  **2. The box is running at roughly two thirds of its 2026-07-22 speed,
  and the slowdown is engine-dependent** — so it distorts ratios, not just
  absolutes. Every row of the July 22 baseline (same box, same Go 1.26.5,
  `results/averages.txt`) against today: lispico rows 1.24-1.59× slower,
  goja 1.49-1.68×, GopherLua 1.52-1.91×. A uniform clock change would
  cancel in the ratio; this does not. `powersave` governor and thermal
  state are the likely cause, and part of lispico's smaller slowdown is
  genuine progress from the companion program (master moved from
  `92a1519` to `80bbf3e`) — the two cannot be separated without a stable
  machine. This matches the standing note that `cmd/perfgate` is not
  decidable on this box.

  ### Toolchain: decided — the gate reads on the harness environment

  Go 1.26 costs GopherLua ~7.7% on fib and leaves lispico flat, so the
  toolchain changes the sign of the comparison. Isolating the fib pair
  into a lispico+GopherLua module that builds under both (the full
  harness cannot: `go 1.26`, and goja requires ≥1.25), same bodies, same
  box, alternating toolchain order across three blocks of `-count=5`
  each — 15 pooled per toolchain:

  | Toolchain | lispico | GopherLua | lispico/GopherLua |
  | --- | --- | --- | --- |
  | Go 1.24.6 | 3.597m ± 4% | 3.257m ± 4% | 1.10 |
  | Go 1.26.5 | 3.561m ± 8% | 3.508m ± 5% | 1.02 |

  lispico −1.0% (n.s.) between the two; GopherLua +7.7%. Replicated by a
  separate sequential run (+7.3% / −0.4%), so it is not measurement
  order.

  **Decision (2026-07-30): the gate is measured on whatever the harness
  requires — Go 1.26+.** That matches the published article environment
  (`article.md` documents Go 1.26.5 for the July 22 figures) and keeps
  all three engines in one harness. The Go 1.24 column above is recorded
  as a documented toolchain sensitivity, not the gate's number.

  Follow-up this creates: CI pins Go **1.24** (`.github/workflows/ci.yml`,
  ×3) and `go.mod` declares `go 1.24.0`, so the gate and CI now measure
  different toolchains by design. Either move CI's pin or accept the
  divergence explicitly — out of scope here, not yet decided.

  ### What unblocks the gate

  A reference-runner measurement — which is what `release-gate-activation`
  exists to stand up. On the decided Go 1.26+ toolchain today's reading is
  ratio 1.02, i.e. the >5% prong would not fire; but that reading comes
  off a box at roughly two thirds of its July 22 speed whose ratio has
  moved 44 points across three sessions, so it does not support closing
  the change either. The toolchain decision does not rescue the local
  prong — the instability above is entirely within Go 1.26.5.

  Tasks 1.1-1.3 are exempt from this uncertainty: the prototypes measure
  the candidate encodings against the landed stack form in the same
  process, so machine state and toolchain cancel in the A/B. They are
  safe to run before the reference-runner number exists; section 2 is not.

  ### Amendment (2026-07-30): no planned work can satisfy this gate

  "What unblocks the gate" above names `release-gate-activation` as the
  change that stands up the reference runner. It does not.
  `.github/workflows/release.yml` benches `./internal/goldset/` only —
  thirteen `.lisp` fixtures over `Goldset/*` and `GoldsetParse/*` — and
  that change's task 2.4 decided the cross-engine bar is dropped: no
  external harness is stood up and neither GopherLua nor goja enters
  `go.mod`. No hosted run produces the GopherLua comparison this gate is
  thresholded against, and no pending change scopes one. The same applies
  to `design.md`'s closing Open Question, which repeats the claim.

  Settling the gate therefore needs a re-scoping decision above this
  change: either stand up a reference runner for the out-of-repo five-row
  harness, or restate the gate against a bar this repo can measure.

  This does not stand alone as the obstacle to section 2. Task 1.3
  withheld authorization on measured evidence, and `design.md` Decision 4
  falsified the proposal's stated mechanism, so a reference-runner number
  would not by itself authorize section 2.

  Toolchain note: `release.yml` pins Go 1.24 while the decision above
  reads the gate on Go 1.26+. That is moot for the consumer gate, which
  compares baseline and candidate on one toolchain; it would matter only
  for the cross-engine prong.

## 1. Design decision (prototype-backed, before any production code)

- [x] 1.1 Vertical-slice prototype A — register form: hand-compile the fib
      body to a three-address register encoding over a frame register
      window; measure against the landed stack form. No compiler work,
      chunks built in test code.
- [x] 1.2 Vertical-slice prototype B — pre-decoded stream: the same fib
      body as a `[]func`-style pre-decoded instruction stream with
      operands resolved at compile time (Vitess/goja shape); same
      measurement.
- [x] 1.3 Decision in design.md: pick by measured fib delta, projected
      implementation surface, and validation complexity. The loser's
      numbers stay in the doc.

  **Decision (2026-07-30): both effects are real and both are small —
  single-digit percent, against a proposal that projected −20-30%. B
  (pre-decoded stream) is rejected as a standalone mechanism; A (register
  form) is not refuted but does not earn its cost. NOT authorization for
  section 2.** Full record in `design.md`.

  A three-arm A/B could not attribute its −34% headline: the prototypes are
  bespoke ~10-opcode loops and `arm=stack` is production's general ~45-opcode
  interpreter, so encoding and specialization were conflated. Extended to a
  2×2 factorial (addressing × dispatch) plus the production baseline.
  `GOMAXPROCS=2`, 20 interleaved reps, go1.26.5:

  | arm | addressing | dispatch | sec/op |
  | --- | --- | --- | --- |
  | `stack` (production) | stack | switch, general | 1.948m ± 2% |
  | `predecoded` | register | closure | 1.214m ± 2% |
  | `register` | register | switch | 1.237m ± 1% |
  | `stack-specialized` | stack | switch | 1.269m ± 3% |
  | `stack-specialized-predecoded` | stack | closure | 1.293m ± 2% |

  - Addressing (register vs stack): **2.55%** under switch (p=0.000),
    **6.51%** under closure (p=0.000).
  - Dispatch (closure vs switch): **1.90%** under register (p=0.002), not
    detected under stack (p=0.081). The cheapest-to-build cell — pre-decoding
    today's stack encoding, no wire-format break — is the *slowest*
    specialized arm, so the cheap version of B buys nothing and the version
    that pays needs the expensive rewrite it was meant to avoid.
  - Specialization (bespoke vs general loop): **~36%**, an order of magnitude
    above either architectural axis, and not an architecture choice.
  - Detection floor ≈2% at this sample size; read the p=0.081 null as "no
    effect >~2% detectable," not as absence.

  **Three measurement defects were found by adversarial review, each
  conclusion-changing.** An earlier round reported a 14-17% addressing win at
  p=0.000 that had already replicated across two sessions; a later round
  reported a flat null. Neither was correct. (1) `stkInterp.pushFrame` copied
  a full ~80-byte frame per call where `regInterp.pushFrame` patched in place
  — ~21.9k extra copies per op, paid only by stack arms. (2) Fixed `b.Run`
  ordering put the register arm earlier in both addressing comparisons.
  (3) **The interleaving never existed**: `-count` does not re-invoke a
  parent benchmark — it repeats each `b.Run` leaf back-to-back, so every
  round before the last measured five contiguous per-arm blocks with drift
  confounded against arm identity. Repetition now happens inside the parent,
  reshuffling arm order across 20 reps at `-count=1`; reps pool by stripping
  the `rep=N/` name component before `benchstat -col /arm`.

  Carry forward: session replication controls for machine drift, not for
  asymmetric work between arms, and a `p=0.000` only states within-run
  consistency — which a systematic per-arm bias satisfies perfectly. Confirm
  interleaving from the raw output before quoting any A/B here.

  `instr/op` is identical at 175.1k across every arm. The pinned disassembly
  shows the 13-instruction fib body holds exactly one `GET_LOCAL` and zero
  separate constant loads — `compiler-branch-arith-fusion` already ate the
  shuffle traffic, so the proposal's "itemizable as stack-shuffle dispatches"
  premise and the literature's >47% instruction-elimination figure do not
  apply to this VM. This falsifies the proposal's stated mechanism
  independently of the timing result.

  Prototypes: `core/vm/dispatch_proto_test.go`; `checkInterval` pinned from
  `core/vm/opcode_test.go`. Zero production files touched. Limitations
  (unexported site-cache path substituted by `env.GetCanonical()`, which also
  compresses the dispatch axis; tax parity asserted analytically, not against
  the live VM's unexported counters) are recorded in `design.md`.

## 2. Implementation (scoped after 1.3 — outline, to be expanded)

- [ ] 2.1 Compiler: register allocation for function bodies (locals →
      window slots, temporaries → scratch slots), caller-window argument
      overlap; stack-form emission remains for uncovered shapes and the
      tree-walker fallback boundary is unchanged.
- [ ] 2.2 VM: dispatch for the new form; per-form coexistence with the
      stack loop (a chunk declares its form; mixed programs run mixed).
- [ ] 2.3 Validation: every register index, window bound, and jump target
      checked at load; the validate/hot-loop invariant review from
      vm-dispatch-loop-tightening re-run in full.
- [ ] 2.4 Ledger, cancellation checkpoints, canonicality freeze, re-entrant
      state: wired identically; crossval `TestVMVsTreeWalker` is the
      acceptance bar, goldset both modes non-increasing.
- [ ] 2.5 Chunk-cache byte bounds re-measured (+~25% code size expected)
      against the consumer-gate tiers; disassembler/tests updated.

## 3. Verify

- [ ] 3.1 Full floor + `-race` + crossval + goldset; adversarial review of
      the new dispatch loop's validated-operand invariant.
- [ ] 3.2 Interleaved five-row harness: fib decisively past GopherLua
      (target ≥10% margin); no other row regresses.

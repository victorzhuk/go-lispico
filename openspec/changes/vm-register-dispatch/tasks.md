# Tasks — vm-register-dispatch

## 0. Gate (blocking — nothing below starts until this fires)

- [ ] 0.1 After vm-batched-ledger-charging, vm-deadline-clock-cadence,
      compiler-branch-arith-fusion, vm-global-call-inline-cache,
      vm-call-frame-fast-path, and engine-lean-call-boundary have all
      landed: interleaved five-row harness run (`-count=10`, one session)
      vs GopherLua and goja. Gate fires iff fib(20) bytecode still trails
      GopherLua on the reference runner, or by >5% locally. Record the
      verdict here either way; if the gate does not fire, close this
      change as not-needed with the numbers.

## 1. Design decision (prototype-backed, before any production code)

- [ ] 1.1 Vertical-slice prototype A — register form: hand-compile the fib
      body to a three-address register encoding over a frame register
      window; measure against the landed stack form. No compiler work,
      chunks built in test code.
- [ ] 1.2 Vertical-slice prototype B — pre-decoded stream: the same fib
      body as a `[]func`-style pre-decoded instruction stream with
      operands resolved at compile time (Vitess/goja shape); same
      measurement.
- [ ] 1.3 Decision in design.md: pick by measured fib delta, projected
      implementation surface, and validation complexity. The loser's
      numbers stay in the doc.

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

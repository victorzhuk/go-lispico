# vm-register-dispatch

> **Closed as not-needed (2026-07-31). Not implemented.** The Impact section's
> "fib −20-30%" and the Why section's stack-shuffle premise were both falsified
> by this change's own prototypes — see `tasks.md` Disposition and `design.md`
> Decisions 1 and 4. Read what follows as the original proposal, not as a
> description of the VM.

## Why

This is the program's gated endgame, authored now so the decision criteria
are fixed before the cheaper changes land — implementation begins ONLY if
task 0's gate fires.

After every prior program (rounds 1-5, stages A-H), fib(20) bytecode stands
at 2.77ms vs GopherLua 1.82-2.07ms on this box. The six companion changes
(batched ledger, clock cadence, branch/arith fusion, call IC, frame fast
path, lean boundary) project fib into the ~1.6-2.0ms band — around the
GopherLua bar, not decisively past it. The literature ceiling for going
further is architectural: register bytecode eliminates >47% of executed
instructions at ~25% larger code, worth ~26.5-32.3% wall time (Shi, Casey,
Ertl, Gregg, ACM TACO 2008); Lua itself made this exact transition in 5.0.
GopherLua inherits Lua's register design, and the remaining fib gap after
fusion is itemizable as stack-shuffle dispatches (push/pop/GET_LOCAL
traffic) that fusion cannot fully remove.

Held honestly against it: Tengo — a Go *stack* VM — beats GopherLua on
recursive fib in its own published benchmarks, so Go-side engineering can
dominate the architectural choice; and lispico's post-tax profile is
dispatch-overhead-heavy (39% flat), which fewer-but-fatter instructions
attack only partially. Hence the gate, and hence a second candidate
architecture carried into the design phase: per-site pre-decoded/specialized
instruction streams (the Vitess/PlanetScale closure-compilation model, also
goja's shape) which buys operand pre-decoding without a full register
rewrite.

## What Changes

- Task 0 gate: after the six companion changes land, re-measure the
  five-row harness interleaved. Implement ONLY if fib(20) still trails
  GopherLua on the reference runner (or trails by >5% locally).
- If gated in: function-body chunks compile to a register-form instruction
  set (three-address operands addressing a per-frame register window over
  the existing value stack); the stack-form opcodes remain for shapes the
  register allocator does not cover and as the validated fallback.
- Frame layout: caller-window overlap for argument passing (Lua 5.0 model)
  so a call moves zero values in the common case.
- Every invariant of the current VM carries over unchanged: validated-chunk
  (register indices validated at load), canonicality freeze semantics,
  batched cancellation, ledger charging, re-entrant state, tree-walker
  fallback for unsupported forms, crossval parity.
- If the design phase concludes the pre-decoded-stream candidate wins on
  measured prototype evidence, the register encoding is dropped and the
  change re-scopes to that mechanism before implementation — either way the
  decision lands in design.md with prototype numbers.

## Impact

- Affected specs: `bytecode-vm` (Bytecode VM execution — instruction-set
  posture; scenarios pin parity, not encoding).
- Affected code: `core/compiler/compiler.go` (register allocation/emission),
  `core/vm/opcode.go`, `core/vm/vm.go` (second dispatch loop or unified
  loop), `core/vm/chunk.go` (validation), disassembly/test tooling.
- Expected if gated in: fib −20-30% beyond the companion program; Rule/Call
  body execution proportionally; boundary costs unaffected (companion
  changes own those).
- Risk: the largest change in the VM's history — prototype-first (fib-only
  vertical slice measured before full opcode coverage), staged behind the
  existing per-form fallback so partial coverage ships dark, and the
  crossval suite is the correctness net. Bytecode size +~25% expected
  (chunk cache byte bounds re-checked against ADR 0008 tiers).

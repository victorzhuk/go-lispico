# Design — compiler-branch-arith-fusion

## Design correction: fuse the operator+operands, not the branch

The originally authored proposal/spec described "one fused compare-and-branch
instruction" — a single instruction that resolves the operator, computes the
comparison, *and* decides the branch. Research before implementation
(three parallel Explore agents over `core/vm`, `core/compiler`, and the
test/bench/goldset infra, cross-checked with two advisor consultations) found
this unsound:

- `Instruction` is a fixed `uint32` — 8-bit opcode + one 24-bit operand
  (`core/vm/chunk.go:12-23`). `PatchJump`/`PatchJumpTo` rewrite that whole
  operand from the opcode alone; there is no room to also carry two operand
  descriptors and a canonicality-site index in the same word as a jump
  target.
- Decisively: a rebind of `<`/`-`/etc. to a VM-compiled `*Closure`
  (`core/vm/vm.go:1764`, `case *Closure:`) pushes a new call frame and
  *returns* — the callee's own instructions run on later iterations of
  `run()`'s outer dispatch loop, not synchronously inside the current `case`
  block. (Rebind to a `core.GoFunc` or a tree-walker `core.Lambda` *is*
  synchronous — `vm.go:1688`, `:1727` — so this hazard is easy to miss if the
  only rebind test used a GoFunc.) A single instruction that both computes a
  comparison and branches on its result cannot correctly handle a callee
  whose result doesn't exist until several outer-loop iterations later.

The corrected design fuses only `FREEZE_NATIVE(_FUNC) + operand + operand +
<native op>` into one instruction that pushes its single result — bit-for-bit
what the unfused sequence would have left on the stack. The trailing
`JUMP_IF_FALSE` (for `if`/`when`/`unless`/`cond`) or whatever consumes an
arithmetic result stays a real, ordinary, unmodified instruction. This is not
a smaller win: the `JUMP_IF_FALSE` was never part of the instruction count the
fusion removes (proposal's own accounting: "4 are FREEZE_NATIVE", "4 form the
unfused compare-branch GET_LOCAL, CONST, LT, JUMP_IF_FALSE" — the fusion
target is FREEZE_NATIVE+GET_LOCAL+CONST+LT, 4→1, with JUMP_IF_FALSE
untouched either way). It is, however, categorically safer: correctness does
not depend on frame-timing at all, since the real trailing instruction
naturally runs whenever the outer loop reaches it, synchronously or after
however many frames a `*Closure` deopt needed.

`openspec/changes/compiler-branch-arith-fusion/specs/bytecode-vm/spec.md` and
`tasks.md` were updated in place to describe this mechanism before
implementation began. User-confirmed via AskUserQuestion (2026-07-28,
"corrected design" recommended option) before writing any code.

## One mechanism, staged rollout

Sections 2 ("compare-branch fusion") and 3 ("fused local/const arithmetic")
of the original proposal are the *same* mechanism —
`FREEZE_NATIVE(_FUNC) + operand + operand + <one of the 9 native ops>` —
differing only in which opcode computes the result and what the caller does
with the pushed value. One new opcode, `OpFusedNativeOp`, covers both; the
two-stage rollout is staged *eligibility* inside `compileNativeOp` (comparison
ops first, gated on a measured win, then arithmetic ops), not two separate
code paths.

## Scope: local/const operands only (not just an encoding limit)

A local-slot read or a constant load never executes arbitrary code — this is
what makes the fusion's canonicality freeze atomic within one dispatch. The
existing `OpFreezeNative` + separate op needs `vm.freezeStack` because the
compiler emits a real argument-evaluation window between freeze and dispatch
(`c.Compile(arg)` per argument), and an arbitrary sub-expression there could
itself rebind the operator mid-evaluation. Local/const operands can't do
that, so resolve+check+read+compute can happen in one `case` block with no
`freezeStack` interaction at all. An arbitrary stack-based operand
("stack×stack" in the original proposal) reintroduces exactly that
side-effecting window and is explicitly **out of scope** for this change —
the spec delta's own "shapes not covered compile exactly as before" clause
already permits this.

## Dialect scope: the Lisp-2 site-cache gap is pre-existing, not introduced

`buildSites` (`core/vm/chunk.go:169-190`) indexes only `OpGetGlobal` and
`OpFreezeNative`; `OpFreezeNativeFunc` (the Lisp-2/CL function-cell path)
keeps no site and re-walks `GetFuncCanonical` every dispatch already, before
this change. `OpFusedNativeOp` with `Func: true` inherits that same
per-dispatch cost, unchanged. Adding a func-cell site cache would be a real,
separate improvement — explicitly **out of scope** here, left as a follow-up.

Every existing fib/Call/Callback/goldset benchmark and fixture uses
`clojure.Dialect()` (Lisp-1) — confirmed by grep across
`runtime/bench_test.go`, `core/vm/bench_test.go`, and
`internal/goldset/goldset.go`. The shipped default `runtime.New()` engine is
CL (Lisp-2). Without a Lisp-2 timed cell, this change's own gate could go
green on a path the shipped engine never takes — task 1.2 adds that
coverage.

## Verification numbers

Fib body: `(def fib (fn [n] (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2))))))`.
Instruction counts below are from direct disassembly of this exact compiled
chunk (not an estimate) — measured independently against a throwaway
worktree pinned at the true unmodified baseline, after two implementing
agents reported disagreeing intermediate figures (17 vs 19 for the
comparison-only stage); this re-measurement is what resolved the
disagreement and is what's recorded here.

- **True baseline (master, unmodified): 22 instructions/call.** The
  proposal's informal count ("20 instructions") did not match a fresh
  disassembly at the point this change was implemented — recorded as a
  correction, not a discrepancy introduced by this change.
- **After comparison-op fusion only (`< > <= >= =`): 19 instructions/call**
  (22 − 3: `FREEZE_NATIVE, GET_LOCAL, CONST, LT` → 1 `FUSED_NATIVE`).
- **After arithmetic-op fusion too (`+ - * /`, this change's final state):
  13 instructions/call** (19 − 6: both `(- n 1)` and `(- n 2)` each collapse
  4 instructions → 1). The top-level `+` combining the two recursive calls'
  results stays unfused — its operands are call results (stack×stack), the
  explicitly out-of-scope shape.
- **CL-dialect (Lisp-2) fib cell added**: `BenchmarkEngine_FibonacciCL` in
  `runtime/bench_test.go` — same instruction-count reduction applies (only
  `FREEZE_NATIVE`↔`FREEZE_NATIVE_FUNC` / `GET_GLOBAL`↔`GET_FUNC` differ
  between dialects).
- **Benchstat (this machine, GOMAXPROCS=2, count=10, interleaved)**: fib
  showed no statistically significant delta at this sample size — the
  before/after distributions overlap almost completely (e.g. `Fibonacci_VM`
  baseline 227–282µs, after 219–281µs, allocs/bytes bit-identical at
  14176 B / 41 allocs throughout). This matches
  `lispico-perfgate-not-local`'s documented finding that this box's noise
  floor exceeds what a single-digit-percent dispatch-count win can clear at
  `count=10`. Per the pre-agreed policy (tasks.md task 2.5/3.2), this is
  recorded as a **local near-miss, not a regression** — allocation counts
  are unchanged (no new allocations introduced), correctness is fully
  verified (crossval, including the discriminating rebind-to-`*Closure`
  case, and goldset), and the causal mechanism (41% fewer instructions
  dispatched per recursive call) is independent of wall-clock noise. The
  authoritative verdict is deferred to the release runner's `cmd/perfgate`,
  per ADR 0008 and the standing local-perfgate-unreliability finding.
- Goldset: all 27 fixtures pass in both modes (correctness); raw timing
  showed session-level noise (multiple build/test/benchmark runs interleaved
  during measurement), not attributed to this change — no goldset fixture
  exercises a fusable local/const comparison or arithmetic shape inside a
  hot loop in a way fusion should regress (fusion only removes instructions
  and adds no new allocation).

## Rejected alternatives

- **Literal single-instruction compare-and-branch fusion.** Unsound under a
  `*Closure` rebind, per above. Rejected outright, not merely deferred.
- **A `finalize()`-time peephole + code-shrink/renumbering pass** (fuse after
  the fact once `Captured` is final, rewriting jump/loop/handler targets for
  the shrunk instruction stream). Considered because capture status is only
  final after the whole function body compiles, and a naive emission-time
  decision seemed to require knowing that in advance. Rejected once the VM
  dispatch case was designed to transparently unwrap a `*cellBox` at operand
  read time (`readFusedOperand`): this makes the fused instruction correct
  regardless of whether its operand's local is ever captured, so no
  finalize-time rewrite or renumbering is needed at all — fusion is decided
  once, at original emission time, in `compileNativeOp`.
- **A func-cell site cache as part of this change.** Real improvement, but a
  distinct one from fusion; scoped out to keep this change's footprint to
  what fusion itself requires.

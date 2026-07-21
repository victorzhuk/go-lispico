# Design — vm-fused-native-ops

## Current shape (fib body, hot path)

```
GET_GLOBAL <        ; site check + ReadCell + push + freezeNativeOp
GET_LOCAL n
CONST 2
LT 2                ; nativeOp hit → drop operator slot, compute
```

Per canonical operator: one extra dispatch, one push, one value
materialization, one `nativeOp` store + zero, one pop-and-discard. Four
operators per fib body → 4 dispatches + 4 push/pop pairs + 4 cell-value reads
per call that serve no computation.

## Fused shape

```
FREEZE_NATIVE <     ; site check, no stack effect
GET_LOCAL n
CONST 2
LT 2                ; frozen-canonical → compute over 2 arg slots
```

## Freeze bookkeeping without a stack anchor

Today `nativeOp` is keyed by the operator's stack index (`fnIdx`). With no
operator slot, per-depth keying breaks twice: the first argument push lands
on the freeze depth (push-time clearing would erase the marker), and nested
operators in argument position freeze at the SAME depth (`(+ (* a b) c)`:
both `+` and `*` freeze at depth d — nothing is pushed between them). As
built, the markers form a LIFO stack instead:

- `freezeStack []freezeRec`, `freezeRec{depth, op, val}` — one record per
  freeze, appended at head-resolution time.
- canonical native → `{depth, op, val}`; non-canonical → `{depth, OpConst, val}`
  with `val` the head-time-resolved operator value. Canonical records keep
  `val` too, so an opcode mismatch (hand-built chunks) can still fall back
  to calling the head-time value.

`FREEZE_NATIVE` resolves through the same site/cell path `OpGetGlobal` uses
(value cell for Lisp-1, function cell for Lisp-2 — `compileNativeOp` picks the
emission today and keeps doing so), then pushes one record. Argument pushes
never touch the stack; nested freezes complete (their fused op pops) before
the outer fused op, so the top record at dispatch is always this op's freeze.
Try-handler setup snapshots `freezeDepth` so an unwind drops records from
aborted computations while preserving outer pending freezes.

## Fused execution

At `OpAdd`…`OpEq` with `argc`: `d := len(stack) - argc`.

- top record has `depth == d` and `op == opcode` → pop, `execNativeFastFused`
  over `stack[d:]`, truncate to `d`, push result. No operator slot involved.
- top record has `depth == d` but `op != opcode` (or `op == OpConst`) → pop,
  grow stack by one, `copy(stack[d+1:], stack[d:])`, `stack[d] = rec.val`,
  then `vm.call(ctx, argc, false)` — the layout `[operator, args...]`
  vm.call expects. The splice is ≤argc word copies on a path that only runs
  when a canonical operator was rebound; today's fallback costs a full
  `OpGetGlobal` push anyway.
- neither marker set (hand-built chunk executing the opcode without a
  preceding freeze) → validation rejects it: `Chunk.Validate` requires every
  fused opcode to be dominated by a `FREEZE_NATIVE`... — NO. Static dominance
  analysis is overreach for Validate. Instead the neither-set case falls back
  to resolving the operator symbol carried by the fused op's constant operand
  (same recovery the current `dispatchNativeOp` performs via the stack value,
  sourced from the site instead). Validate only checks the constant-index
  operand is in range, consistent with existing cases.

## Evaluation-order parity argument

The tree-walker resolves the head, then evaluates arguments, then applies the
head-time value. `FREEZE_NATIVE` sits at the exact code position the operator
`OpGetGlobal` occupies today — before argument code — and captures both the
canonical decision and (when non-canonical) the value. A rebind during
argument evaluation affects neither, matching the tree-walker and today's VM.
The existing crossval scenarios (`Rebound operator falls back`, `Lisp-2
function-cell rebind falls back`, mid-argument rebind cells in the parity
suite) are the regression net.

## MaxStack accounting

Canonical path peaks one slot LOWER than today (no operator slot). Fallback
peaks one slot HIGHER than canonical (the splice appends the operator before
`vm.call`). `stackDelta` for the fused opcodes is `-argc` (unchanged), so
`computeMaxStack` sizes the stack to the canonical peak; the fallback splice's
transient +1 is absorbed by `append` growing capacity on the rare rebound path.
No accounting change is needed — the splice never indexes out of range because
it grows the slice before the `copy`.

## Measurement outcome

Benchstat at the post-versioned-reads baseline (raw output: `benchstat-1s.txt`
and `benchstat-2s.txt`): `GOLDSET_MODE=vm` B/op and allocs/op non-increasing on
all cells at the doubled-benchtime rerun; `loop-sum` (the engine-sensitive cell
the change targets) inconclusive on both pairs (1s p=0.645, 2s p=0.739), with
scattered wins on data-dominated cells that did not replicate consistently.
Per ADR 0008's burden-of-proof rule the gate was not met; the maintainer
landed the change anyway on instruction-count and dispatch-traffic grounds
(fib body 20→16 instructions, −4 operator head dispatches per call) — see
DECISION.md. The site-cache + versioned-reads work already made the operator
head read cheap enough that removing it is noise-level at this baseline.

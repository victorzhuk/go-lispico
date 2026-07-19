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
operator slot, the key becomes the stack depth at freeze time — which equals
the base of the upcoming arguments, and equals `len(stack) - argc` at the
fused op. Nested operators compose exactly as today because each freeze keys a
distinct depth (`(+ (* a b) c)`: `+` freezes at depth d, `*` at d+1).

Two parallel per-VM scratch arrays indexed by stack depth (both already exist
in shape via `nativeOp`):

- `nativeOp[d] = op` — frozen-canonical marker (existing array, unchanged).
- `opFallback[d] = value` — head-time-resolved operator value, set only when
  the freeze finds the binding non-canonical.

`FREEZE_NATIVE` resolves through the same site/cell path `OpGetGlobal` uses
(value cell for Lisp-1, function cell for Lisp-2 — `compileNativeOp` picks the
emission today and keeps doing so), then stores one of the two markers.
Consumed markers are zeroed exactly as `nativeOp` is zeroed today, including
the existing push-time clearing that guards stale entries.

## Fused execution

At `OpAdd`…`OpEq` with `argc`: `d := len(stack) - argc`.

- `nativeOp[d] == op` → `execNativeFast` over `stack[d:]`, truncate to `d`,
  push result. No operator slot involved.
- else (`opFallback[d]` set) → grow stack by one, `copy(stack[d+1:], stack[d:])`,
  `stack[d] = opFallback[d]`, then `vm.call(ctx, argc, false)` — the layout
  `[operator, args...]` vm.call expects. The splice is ≤argc word copies on a
  path that only runs when a canonical operator was rebound; today's fallback
  costs a full `OpGetGlobal` push anyway.
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
peaks at today's level (operator spliced in). Compiler keeps charging the
operator slot in `MaxStack` — conservative, correct for both paths, no
accounting change needed.

## Measurement gate

Prototype first on the fib/goldset shapes; adopt only if benchstat (≥6 runs)
shows a real win at the post-poll/post-versioned-reads baseline and the
`GOLDSET_MODE=vm` gate is non-increasing. If the win is under noise, archive
the change as measured-and-rejected with the numbers.

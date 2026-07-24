## Context

`MaxStructuralDepth` (default 1024) already bounds the depth of a **literal form
being evaluated** in the tree-walker, but not (a) an already-constructed runtime
value later walked by `String`/`Equals`/`ValueDeepBytes`/`ValueNodeCount`, nor
(b) a macro-expanded form walked by the compiler. Values reach unbounded depth
via flat iterative construction (`loop`/`recur` + `list`), which grows no Go
stack and pays only the byte ledger — never a depth ledger. The unguarded Go
recursion in the walks is the crash surface.

## Goals / Non-Goals

- Goal: no value-tree walk, construction, or compile step can produce an
  uncatchable Go stack overflow.
- Goal: breaches are a normal terminal `ResourceLimitError`, catchable by the
  host as an error (not a fatal crash), non-catchable by in-script `try`.
- Non-Goal: bounding value *breadth* (a flat million-element list is fine — it
  does not recurse).
- Non-Goal: a new configuration knob; reuse `MaxStructuralDepth`.

## Decisions

### Bound at construction, and defensively in the walks

Two layers:

1. **Construction gate (primary).** Every nesting-capable construction point —
   VM `OpMakeList`/`OpMakeVector`/`OpMakeMap`, stdlib `list`/`cons`/`vector`/
   `conj`/`assoc`/`merge`, and `json/decode` — checks the resulting nesting
   depth against `MaxStructuralDepth` and returns `CodeResourceLimit` on breach.
   The check is a bounded scan: descend at most `MaxStructuralDepth + 1` levels
   to confirm-or-reject, so the check itself cannot overflow. For most builtins
   the new value adds one level on top of an already-validated input, so the
   check is O(1) amortized (inspect the wrapped value's cached/known depth), not
   a full re-walk — record depth as values are built where practical.

2. **Walk gate (defense in depth).** `String`/`Equals`/`ValueDeepBytes`/
   `ValueNodeCount` carry an internal depth counter and stop with a bounded
   sentinel (truncation marker for `String`; a defined result for `Equals`;
   the capped count for the byte/node walks) once `MaxStructuralDepth` is
   exceeded, so a value that somehow escaped the construction gate still cannot
   crash a caller. These methods have no `ctx`, so they cannot return an error —
   they degrade safely rather than panic.

### Compiler depth

`Compile` already increments `compileDepth`; add `if c.compileDepth >
maxCompileDepth { return CodeResourceLimit }` at the increment site, default
1024. Apply the same to `literalDepth()`.

### Error class: terminal

Depth breaches are terminal `ResourceLimitError` (`CodeResourceLimit`),
consistent with the existing structural-depth-exceeded error and unlike the
catchable call-depth `EvalError`. A script cannot `try`/`catch` its way past the
construction gate.

## Risks / Trade-offs

- The construction gate must be cheap on the hot path. Prefer tracking a value's
  depth as it is built (one integer alongside the collection) over re-scanning;
  where a full bounded scan is unavoidable, cap it at `MaxStructuralDepth + 1`.
- `String`/`Equals` degrading (truncation / defined-on-overflow result) is a
  visible behavior change for pathological values only; document it. Ordinary
  values are unaffected.
- `json/decode` already deep-charges its result; the depth gate is complementary
  and must run before or during construction, not only after.

## Migration

None for ordinary values. Pathological deep values that previously crashed now
return a terminal error (or a truncated string), which is the intended fix.

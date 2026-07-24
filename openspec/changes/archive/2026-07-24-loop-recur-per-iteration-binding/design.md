## Context

`loop`/`recur` iterates without growing the stack by reusing one binding
environment and mutating the loop variables in place — correct for the common
case where nothing captures them, but wrong when a closure in the body captures
a loop variable. The flat-closures work added `markCaptures`, so the compiler
already knows which locals are captured by a nested closure; that analysis is
the lever that lets this fix stay cheap.

## Goals / Non-Goals

- Goal: closures created in a loop body observe their own iteration's loop-var
  values (Clojure-conformant per-iteration binding).
- Goal: no allocation regression for loops that do not capture their loop
  variables (preserve the `loop-sum` allocation-gate posture).
- Goal: tree-walker and bytecode crossval parity.
- Non-Goal: changing `recur`'s tail-position / stack-flat iteration property.
- Non-Goal: giving every `let` iteration fresh cells unconditionally.

## Decisions

### Fresh cell only for captured loop slots

Split `recur`'s rebind by whether the target slot is captured (known from
`markCaptures`):

- **Captured slot:** emit a fresh-cell bind (`OpBindCell`, the same op a fresh
  `let` binding uses) so each iteration's closure holds a distinct cell. The
  next iteration's `recur` binds a new cell; the previous cell — now owned only
  by the escaped closure — retains that iteration's value.
- **Non-captured slot:** keep `OpSetLocal` / write-through (stack slot or shared
  cell). No behavior change, no new allocation.

Tree-walker mirror: `evalLoop` installs a fresh `*Cell` for a captured loop slot
each iteration instead of `Set`-overwriting the existing one; non-captured slots
keep the in-place overwrite.

### Why capture-gated rather than always-fresh

Always-fresh would regress the allocation gate for hot counting loops that never
capture (the `loop-sum` cell mirrors locals every iteration). Capture is a
compile-time-known property, so the fresh-cell cost lands only where the
semantics demand it. This keeps the fix invisible to the perf gate while
correcting the observable bug.

### Parity

The capture set and the fresh-vs-writethrough decision must be identical in both
evaluators. The tree-walker does not run `markCaptures`, so it needs an
equivalent "is this loop var captured by a body closure" check (a lightweight
scan of the loop body for a closure referencing the slot, or reuse of the
compiler's capture analysis). Crossval cases pin `(0 1 2)` for both.

## Risks / Trade-offs

- Tree-walker capture detection is the delicate part — if it disagrees with the
  compiler's `markCaptures`, crossval breaks. Prefer sharing one analysis over
  two hand-rolled scans.
- A loop variable captured *and* mutated by an explicit `set!` in the body: the
  `set!` must still mutate the current iteration's cell (ordinary write-through
  visibility), while `recur` starts a fresh cell. Cover this interaction with a
  test so `set!` semantics are not collateral damage.

## Migration

Behavior fix only. Code relying on the buggy `(3 3 3)` outcome is
vanishingly unlikely and was never correct; no migration path provided.

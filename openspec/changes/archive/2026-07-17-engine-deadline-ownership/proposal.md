## Why

The Engine wraps every `Eval` in its configured timeout (30 s default) even when the caller's context already carries an earlier deadline — a redundant timer that can never fire and a hidden second limit an embedder cannot reason about. ADR 0010 resolves this: a fully covering embedder may own its evaluation deadlines; YAGEL disables the Engine default with `WithTimeout(0)` once its Rule-load and handler-dispatch deadlines cover every path, while ordinary embedders keep the safe default (PRD stories 23, 24).

## What changes

- The Engine keeps its safe 30-second default and the existing `WithTimeout(0)` disable path.
- The Engine no longer creates its own timer when the caller's context already has a deadline at or earlier than the Engine's; the caller's deadline governs. A caller deadline later than the Engine's still gets the Engine's tighter bound.
- The `WithTimeout` contract — default, disable, and redundant-timer skip — is documented at the Engine surface so an embedder can own deadlines deliberately, not accidentally.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: gains an evaluation-deadline requirement covering the default, caller-owned deadlines, and explicit disablement.

## Impact

- ADRs: implements ADR 0010; keeps ADR 0007's separation between deadlines (cooperative, wall-clock) and resource limits (hard ceilings).
- Invariants preserved: cooperative cancellation semantics; no behavior change for embedders that pass plain contexts.
- Out of scope: YAGEL's own Rule-load and handler-dispatch deadlines (YAGEL ADR 0042, YAGEL repo); any instruction-budget mechanism (rejected by ADR 0008 absent a failing gate cell).

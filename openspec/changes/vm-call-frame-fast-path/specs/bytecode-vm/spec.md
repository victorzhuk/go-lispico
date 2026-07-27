# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Bytecode VM execution

The bytecode VM SHALL execute validated chunks with a dispatch loop that
keeps per-frame execution state in loop-local storage, synchronizing with the
frame stack only at control-flow transitions. A transition between frames
running the same chunk MAY restore only the state that can differ (instruction
pointer, stack base, environment); a cross-chunk transition SHALL restore the
full dispatch state. Depth accounting SHALL use shared atomic counters only
when the counter is actually shared with an evaluation state or a re-entrant
context; a VM-private counter MAY be a plain field. VM reuse across boundary
calls MAY skip re-initializing state a clean prior run provably left in the
required condition; any error or panic exit SHALL restore the full reset
before reuse. None of this SHALL change observable evaluation semantics:
results, error shapes, resource-limit enforcement, re-entrant budget sharing,
and race-detector cleanliness are unchanged.

#### Scenario: Self-recursive execution stays correct

- **WHEN** a self-recursive compiled function executes deep call/return chains within one chunk
- **THEN** results SHALL be identical to the tree-walker, and throw/catch unwinding across the fast-path frames SHALL behave exactly as before

#### Scenario: Shared depth counters still enforce limits

- **WHEN** a host `GoFunc` re-enters the evaluator so the call-depth counter is shared across the boundary
- **THEN** combined nesting SHALL still trip the configured depth limit, and `go test -race` SHALL report no data race

#### Scenario: Reused VM behaves like a fresh one

- **WHEN** a boundary call reuses a VM whose previous run exited cleanly, and separately one whose previous run exited with a terminal error
- **THEN** the next call SHALL observe fully initialized state in both cases, with no leakage of stack, frames, handlers, deadline, meter, or budget from the prior run

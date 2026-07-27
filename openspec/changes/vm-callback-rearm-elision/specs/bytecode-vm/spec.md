# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Lazy re-entrant evaluation state

Dispatching a host `GoFunc` from the VM SHALL NOT materialize an
evaluation-state value unless the host function actually requests it (by
re-entering the evaluator or reading the state from its context). When
requested, the state SHALL be materialized at most once per VM run and SHALL
carry the enclosing evaluation's structural-depth and deadline budget, so
re-entrant calls enforce the same resource limits as today. The context
wrapper handed to host functions SHALL be reusable VM-owned storage: a
subsequent run on the same VM with the same outer context SHALL reuse the
wrapper after re-arming its per-evaluation fields, rather than allocating a
new one, and a run with a different outer context SHALL build a fresh
wrapper. The wrapper's deadline SHALL be computed lazily at first
observation, not at dispatch, so host functions that never observe a
deadline trigger no wall-clock read. The context a `GoFunc` receives SHALL
delegate cancellation, deadline, and unrelated values to the caller's context
unchanged. State handed to a host function SHALL be generation-guarded:
retaining the context past the call SHALL NOT expose a later run's budget,
deadline, or internals — a stale-generation access SHALL behave as a context
carrying no evaluation state, delegating to the outer context and adopting a
fresh budget on re-entry.

Re-arming SHALL be proportional to what changed: when a rearm carries the
same configuration the wrapper was last armed with — same limits, timeout,
and meter posture — the wrapper MAY refresh only its generation stamp and
any per-run seeds that differ, rather than rewriting every field. Any
configuration difference SHALL take the full rearm. The observable contract
is unchanged in either case: the host function and any re-entry see exactly
the values a full rearm would have installed.

#### Scenario: Non-re-entrant host pays no state allocation

- **WHEN** a compiled body repeatedly dispatches a `GoFunc` that never re-enters the evaluator
- **THEN** no evaluation-state value SHALL be allocated for those dispatches

#### Scenario: Wrapper reused across runs with one outer context

- **WHEN** the same VM executes many top-level calls under one outer context, each dispatching a `GoFunc`
- **THEN** the context wrapper SHALL be allocated at most once and re-armed per run, and per-call wrapper allocations SHALL be zero at steady state

#### Scenario: Re-entry shares the enclosing budget

- **WHEN** a dispatched `GoFunc` re-enters the evaluator with the context it received
- **THEN** the re-entrant evaluation SHALL count structural depth against the enclosing run's budget and honor the enclosing deadline, identical to eager state adoption

#### Scenario: Caller context semantics pass through

- **WHEN** the caller's context is cancelled while a dispatched `GoFunc` is waiting on it
- **THEN** the `GoFunc` SHALL observe cancellation through the context it received exactly as it would through the caller's context

#### Scenario: Retained context is generation-guarded

- **WHEN** a `GoFunc` stores the context it received and reads state or re-enters the evaluator after its call returned and the VM has moved to a later run
- **THEN** the stale context SHALL NOT expose the later run's budget, deadline, or internals; its accesses SHALL behave as a context carrying no evaluation state

#### Scenario: Changed configuration is fully re-armed

- **WHEN** the engine's limits, timeout, or meter posture change between two calls that reuse one wrapper
- **THEN** the next dispatch SHALL observe the new configuration exactly as a freshly built wrapper would

#### Scenario: Same-configuration rearm is observably identical

- **WHEN** two adjacent calls with identical configuration each dispatch a re-entering `GoFunc`
- **THEN** the second call's re-entry SHALL observe a fresh budget and correct seeds exactly as under a full rearm

# runtime-api — delta

## ADDED Requirements

### Requirement: Named boundary calls amortize resolution

Repeated `Call(ctx, name, ...)` of one name on one engine SHALL NOT repeat
full name resolution per invocation: after a first resolution, subsequent
calls SHALL reach the bound function through a cached resolved cell, so
the steady-state cost of `Call(name)` converges to the handle call's cost.

`Call` resolves a callee name the way the evaluator resolves a form's head:
under Lisp-2 the function cell SHALL be consulted first and the value cell
SHALL be used as a fallback; under Lisp-1 the single value namespace applies.
A name bound by a function-defining form under Lisp-2 SHALL therefore be
reachable from `Call`. `Func(name)` keeps its narrower function-cell-only
resolution under Lisp-2, so `Call`'s reachable set is a superset of `Func`'s.

Under Lisp-2 a resolution that reached the value cell by fallback SHALL NOT be
cached, because acquiring a function cell for a name is not observable through
the environment's name generation: caching a fallback would let a later
function binding go unobserved. Resolution correctness outranks reuse, so
these names re-resolve per call.
Caching SHALL NOT change observable semantics relative to per-call
resolution: redefinition SHALL be observed by the next call; deleting a
binding SHALL make the next call fail as undefined; a deleted-then-redefined
name SHALL resolve to the new definition; `UnloadPlugin` and hot-reload SHALL
invalidate affected entries. Where invalidation cannot be decided, the cache
SHALL drop the entry and re-resolve — resolution correctness outranks reuse.
The cache SHALL be bounded over the engine's lifetime, and `Stats()`
attribution SHALL remain exact.

#### Scenario: Steady-state named call matches the handle path

- **WHEN** `Call(ctx, "route", task)` runs repeatedly on a bytecode engine
- **THEN** per-call name-resolution work SHALL be amortized away, and the call SHALL behave identically to `Func("route")` followed by `Fn.Call`

#### Scenario: Lisp-2 resolves the function cell before the value cell

- **WHEN** a name is bound by a function-defining form under a Lisp-2 dialect, and separately when one name carries both a value binding and a function binding
- **THEN** `Call` SHALL reach the function binding, matching what the same name in head position resolves to, and a value-only binding SHALL still be reachable

#### Scenario: Redefinition is observed immediately

- **WHEN** a function is redefined between two `Call`s of its name
- **THEN** the second `Call` SHALL invoke the new definition

#### Scenario: Delete then redefine resolves the new binding

- **WHEN** a name is called, its binding deleted, the name redefined, and the name called again
- **THEN** the final call SHALL invoke the new definition, exactly as on an engine that resolves per call

#### Scenario: A later function binding is observed

- **WHEN** a name holds only a value binding under Lisp-2, is called, and is then given a function binding
- **THEN** the next `Call` SHALL invoke the function binding, matching head position

#### Scenario: A tombstoned function binding falls back

- **WHEN** a name's function binding is called, then deleted, while a value binding for that name exists
- **THEN** the next `Call` SHALL invoke the value binding rather than reporting the name undefined

#### Scenario: Unload and reload invalidate

- **WHEN** `UnloadPlugin` removes a cached name, or a hot-reload replaces the engine's definitions
- **THEN** subsequent `Call`s SHALL behave exactly as on an engine that resolves per call

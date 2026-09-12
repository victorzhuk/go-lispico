## ADDED Requirements

### Requirement: Failed plugin operations settle retained ownership once

When `Use` or `ReloadPlugin` returns an initialization, vocabulary, capacity, or retained-settlement error, retained charges for cells the failed operation created and rollback removed SHALL NOT remain charged to any meter. Charges backing bindings that survive rollback SHALL remain charged. No charge SHALL be released twice, and every charge a denied settlement already applied SHALL be released with its exact amount. Meter calls SHALL NOT run while environment or lazy-state locks are held. The operation's error SHALL be returned and unused compute lease SHALL be returned.

#### Scenario: Failed initialization leaves no retained charge

- **WHEN** a plugin binds new names and initialization then fails, with no concurrent host writes
- **THEN** every metered retained charge made for those names SHALL be released and the meter's net retained usage SHALL equal its value before the operation

#### Scenario: Settlement denial releases exactly once

- **WHEN** retained settlement for a plugin operation is denied by a later meter or by the host meter after earlier charges succeeded
- **THEN** each earlier charge SHALL be released exactly once with its charged amount, the error SHALL be returned, and the operation's bindings SHALL be rolled back

#### Scenario: Surviving host binding keeps its charge

- **WHEN** a host binds a new name concurrently with a plugin operation that later fails
- **THEN** the host binding's retained charge SHALL remain charged after rollback

#### Scenario: Capacity rejection mid-operation

- **WHEN** a plugin write is rejected by a per-env retained capacity limit during initialization
- **THEN** a `ResourceLimitError`-coded error SHALL be returned and no retained charge for the operation's removed cells SHALL remain

## MODIFIED Requirements

### Requirement: Retained state is charged by owned capacity

`ResourceLimits` SHALL carry `MaxRetainedBytesPerEnv` and
`MaxRetainedSlotsPerEnv` fields, defaulting to 32 MiB and 100,000 when left
at zero. Every env on the engine path SHALL carry an owned-capacity counter
(bytes + slots) with limit configuration inherited from its parent chain.
Binding a new name SHALL charge the fixed-table backing cost plus the value's
shallow size and one slot, and SHALL raise a
`*core.LispicoError{Code: "ResourceLimitError"}` — leaving prior bindings
intact — if the new total would exceed either ceiling. Rebinding through an
existing binding and reviving a tombstoned binding SHALL NOT charge. Deleting
a binding SHALL tombstone the slot without releasing backing or decrementing
counters. The runtime SHALL provide `(*Env).RetainedUsage() (bytes, slots
int64)` and `(*Env).Rebuild()`; `Rebuild` SHALL compact in place — same
`*Env` identity, live `*Cell` pointers preserved, tombstoned cells dropped,
counters recomputed, name generation bumped — and besides registration abort
is the only path that releases dead backing. Registration abort of a failed
plugin operation SHALL release dead backing for exactly the journal-owned
cells the operation created and the rollback removes: counters refunded by
the reserved amount, settled retained charges released once with their exact
amounts, charges and capacity backing surviving bindings — restored,
foreign-touched, host-created — untouched, and no meter call executed while
an environment or lazy-state lock is held. The runtime SHALL provide
`Engine.LoadScope(ctx, source, bindings) (core.Value, *core.Env, error)`
returning the retained child scope with `EvalWithBindings` evaluation
semantics. Capturing an env through a closure SHALL NOT transfer or
double-count ownership; values charge shallow backing only.

#### Scenario: Slot ceiling fails closed

- **WHEN** an env with `MaxRetainedSlotsPerEnv: 5` receives a sixth new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"` and the prior five bindings SHALL remain intact

#### Scenario: Byte ceiling fails closed

- **WHEN** an env's retained bytes would exceed `MaxRetainedBytesPerEnv` on a new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Rebind does not charge

- **WHEN** an existing binding is rebound to a new value
- **THEN** the env's slot and byte counters SHALL NOT increase

#### Scenario: Delete tombstones but does not release

- **WHEN** a binding is deleted and `RetainedUsage` is then read
- **THEN** the counters SHALL be unchanged from before the delete

#### Scenario: Rebuild releases dead capacity and preserves live cells

- **WHEN** an env has had many bindings added, most deleted, and `Rebuild` is called while a caller holds a `*Cell` for a live binding
- **THEN** `RetainedUsage` SHALL equal the live binding set's backing, the old maps SHALL be garbage-collectable, and the held cell SHALL still serve the live binding

#### Scenario: LoadScope returns the retained scope

- **WHEN** a host calls `LoadScope` with source that defines handler closures
- **THEN** the returned `*core.Env` SHALL be the scope those closures captured, and `RetainedUsage` on it SHALL report the load's retained backing

#### Scenario: Closure capture does not double-count

- **WHEN** a `Lambda` captures an env and the env's counters are later inspected
- **THEN** the captured env's counters SHALL be the same as before the capture

#### Scenario: Failed plugin operation releases removed owned backing

- **WHEN** a plugin operation creates new bindings and then fails so its registration journal aborts
- **THEN** the capacity and settled charges of exactly the removed operation-owned cells SHALL be released, and the meter's net retained usage SHALL equal its value before the operation

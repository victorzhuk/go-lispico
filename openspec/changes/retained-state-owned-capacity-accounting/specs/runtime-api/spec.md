# runtime-api — delta

## ADDED Requirements

### Requirement: Retained state is charged by owned capacity

`ResourceLimits` SHALL carry `MaxRetainedBytesPerEnv` and
`MaxRetainedSlotsPerEnv` fields, defaulting to 32 MiB and 100,000 when left at
zero. Each `core.Env` SHALL carry an owned-capacity counter (bytes + slots).
`Env.Child()` SHALL create a fresh counter bounded by the configured limit.
Binding a new name SHALL charge the actual backing bytes plus one slot, and
SHALL raise a `*core.LispicoError{Code: "ResourceLimitError"}` if the new
total would exceed either ceiling. Rebinding through an existing binding SHALL
NOT charge (the slot is reused). Deleting a binding SHALL tombstone the slot
without releasing the backing. The runtime SHALL provide a `(*Env).Rebuild()`
method that constructs a fresh env from the current live binding set and
atomically swaps it in, releasing the old backing; this is the only path that
releases dead-bucket capacity. Capturing an env through a closure SHALL NOT
transfer or double-count ownership.

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

- **WHEN** a binding is deleted and the env is then asked for its retained slot count
- **THEN** the count SHALL be unchanged from before the delete

#### Scenario: Rebuild releases dead-bucket capacity

- **WHEN** an env has had many bindings added and then most deleted, and `Rebuild` is called
- **THEN** the resulting env SHALL have a retained slot count equal to the live binding set, and the old backing SHALL be releasable by the garbage collector

#### Scenario: Closure capture does not double-count

- **WHEN** a `Lambda` captures an env and the env's bindings are later inspected
- **THEN** the captured env's counters SHALL be the same as before the capture

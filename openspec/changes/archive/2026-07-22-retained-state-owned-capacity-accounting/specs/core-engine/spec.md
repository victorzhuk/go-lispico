# core-engine — delta

## ADDED Requirements

### Requirement: Env owned-capacity accounting

The core evaluator SHALL charge an env's owned-capacity counter on every new
binding write — `def` / `let` / `fn` params / `defn` / `defmacro` in the
tree-walker and per-call frame env writes in the VM — and SHALL raise
`Code: "ResourceLimitError"` (terminal) when a new binding would exceed the
env's configured byte or slot ceiling, leaving the env unmodified. Rebinding
through an existing `Cell` and reviving a tombstoned `Cell` SHALL NOT charge.
Deleting a binding SHALL tombstone without decrementing. `Rebuild` SHALL
preserve `*Env` identity and live `*Cell` identity, drop tombstoned cells,
recompute counters, and bump the name generation so cached resolutions of
dropped cells invalidate. Counters SHALL be uniform across persistent and
transient envs; a transient env's counter dies with the env. Capturing an env
through a closure SHALL NOT transfer or double-count ownership.

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

- **WHEN** a binding is deleted and the env's retained counters are read
- **THEN** the counters SHALL be unchanged from before the delete

#### Scenario: Rebuild preserves live cell identity

- **WHEN** `Rebuild` runs while a VM site cache holds a resolution for a live binding and another for a deleted one
- **THEN** the live resolution SHALL keep serving the current value and the deleted one SHALL observe the binding unbound, with no stale value served

#### Scenario: Transient frame envs are counted but vanish

- **WHEN** a VM call allocates a frame env, binds locals, and returns
- **THEN** the bindings SHALL have charged that env's own counter, the per-env ceiling SHALL apply, and no persistent counter SHALL retain the charge after return

#### Scenario: Closure capture does not double-count

- **WHEN** a `Lambda` captures an env and the env's counters are later inspected
- **THEN** the captured env's counters SHALL be the same as before the capture

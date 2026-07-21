# core-engine — delta

## ADDED Requirements

### Requirement: Env owned-capacity accounting

The core evaluator SHALL charge an env's owned-capacity counter on every new
binding write — `let` / `fn` / `defn` / `defmacro` in the tree-walker and
per-call frame env allocation in the VM — and SHALL raise
`Code: "ResourceLimitError"` when a new binding would exceed the env's
configured byte or slot ceiling. Rebinding through an existing `Cell` SHALL
NOT charge. Deleting a binding SHALL tombstone the slot without decrementing
the counter. Capturing an env through a closure SHALL NOT transfer or
double-count ownership.

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

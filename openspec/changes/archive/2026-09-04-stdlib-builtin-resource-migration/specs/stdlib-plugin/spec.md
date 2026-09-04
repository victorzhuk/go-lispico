## ADDED Requirements

### Requirement: Active Builtins own scalable work and result storage exactly once

Every active stdlib Builtin, CL adapter, and transitive helper SHALL appear in a
repository-owned executable inventory. Each phase whose work scales with user
input SHALL have exactly one accounting owner. Uninterrupted core-owned work
SHALL accrue one local `BuiltinWorkBudget` step per semantic unit and flush every
return; callback execution SHALL be owned by evaluator re-entry while separate
copying, traversal, scheduling, and result construction remain Builtin work.

An opaque core-owned helper SHALL be replaced by an interruptible kernel,
rejected before entry by a deterministic work bound, or documented as a bounded
exception with proof and maximum work. Calls into arbitrary host-provided Go
`Value` methods MAY be recorded as trusted-host boundaries and are outside the
core-owned interruption guarantee. A check only before and after opaque work
SHALL NOT establish compliance.

Every successful return branch SHALL be classified as computed scalar/shared
singleton, wholly borrowed/already callback-accounted, fresh container, fresh
deep, incremental persistent, or mixed conditional storage. Wholly borrowed or
already callback-accounted branches SHALL mark zero result bytes. Other branches
SHALL charge only storage newly owned by that operation, preserving centralized
fallback accounting when the callee did not account. Registration, reachable
work phases, and return branches SHALL fail a static completeness test when an
entry is missing or duplicated.

Collection-length and construction-depth helpers SHALL query the active evaluator
passed to the GoFunc, not `env.Evaluator()`, so nested lexical environments retain
the dispatching Engine's policy.

#### Scenario: A long uninterrupted family is bounded

- **WHEN** any inventoried Builtin performs scalable core-owned work under cancellation, an expired absolute Evaluation deadline, or an exhausted Reduction budget
- **THEN** it SHALL stop at a bounded budget synchronization with the corresponding Terminal error

#### Scenario: Callback and surrounding work have separate owners

- **WHEN** a higher-order Builtin evaluates one callback per element and also copies input or builds a result
- **THEN** re-entry SHALL own callback execution and the Builtin budget SHALL own the separate uninterrupted phases without charging either twice

#### Scenario: Borrowed results are not allocated again

- **WHEN** an inventoried branch returns an existing member, default, accumulator, empty-path subject, or already-accounted callback result
- **THEN** it SHALL mark zero result bytes and the centralized apply site SHALL NOT charge that storage again

#### Scenario: Fresh and mixed results remain fail-closed

- **WHEN** a Builtin returns fresh, incremental persistent, or mixed fresh/borrowed storage under a tight allocation limit
- **THEN** it SHALL charge all and only newly owned storage and SHALL return `ResourceLimitError` if that charge exceeds the limit

#### Scenario: Child scopes retain Engine construction policy

- **WHEN** a Builtin constructs a collection inside a child Lambda environment with no evaluator of its own
- **THEN** collection-length and construction-depth limits SHALL come from the active Evaluator under both Evaluator and VM execution

#### Scenario: Inventory changes fail closed

- **WHEN** a registered Builtin, reachable scalable phase, opaque helper, or successful result branch is added without an inventory disposition
- **THEN** the static completeness test SHALL fail before the change is accepted

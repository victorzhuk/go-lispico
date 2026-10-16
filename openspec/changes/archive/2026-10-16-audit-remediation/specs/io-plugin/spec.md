## ADDED Requirements

### Requirement: Sandbox containment is enforced during the open

The `lio` sandbox SHALL enforce containment during the filesystem open itself, not only before it. A read or write SHALL be resolved relative to the sandbox root by the open, so that a path which leaves the root between validation and the open is refused. Existing deny-pattern and root checks SHALL remain in force as defense in depth and SHALL NOT be replaced by the open-time containment.

#### Scenario: A path that leaves the root at open time is refused

- **WHEN** a path is inside the sandbox root at validation time and resolves outside it at open time
- **THEN** the operation SHALL be denied for both read and write

#### Scenario: An ordinary in-root path still opens

- **WHEN** a path lies inside the sandbox root and matches no deny pattern
- **THEN** the operation SHALL succeed

#### Scenario: Deny patterns remain enforced

- **WHEN** a path matches a deny pattern
- **THEN** the operation SHALL be denied regardless of the open-time containment result

### Requirement: File reads are bounded and charged

A file read SHALL NOT read an unbounded amount of data into memory. The read SHALL respect a configured maximum size, SHALL charge the bytes it returns to the engine's allocation ledger, and SHALL fail with a typed error rather than truncate silently when the configured maximum is exceeded.

#### Scenario: A read at the limit succeeds

- **WHEN** a file is read whose size is within the configured maximum
- **THEN** the full content SHALL be returned and charged

#### Scenario: A read beyond the limit fails

- **WHEN** a file is read whose size exceeds the configured maximum
- **THEN** the read SHALL fail with a typed error and SHALL NOT return an unbounded buffer

#### Scenario: The read fails closed under an exhausted budget

- **WHEN** the engine's allocation budget cannot cover the read
- **THEN** the read SHALL fail rather than complete uncharged
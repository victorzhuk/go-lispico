# io-plugin Specification

## Purpose

The io-plugin capability provides file I/O functionality for the system, registered and made ready for use when the system initializes.

## Requirements

### Requirement: io-plugin implementation
The system SHALL implement the io-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the io-plugin SHALL be ready for use

### Requirement: File sandbox is a security boundary

In strict mode, the `lio` sandbox SHALL confine file operations to its configured root.
It SHALL resolve symlinks before root and deny-pattern validation.
Reads, writes, directory creation, listing, and metadata operations SHALL use the pinned `os.Root` to enforce containment during execution.

#### Scenario: Symlink escape is denied

- **WHEN** a path traverses a symlink whose target lies outside the sandbox root
- **THEN** the operation SHALL be denied for both read and write

#### Scenario: Deny pattern is enforced on the resolved path

- **WHEN** a deny pattern matches the symlink-resolved target
- **THEN** the operation SHALL be denied, regardless of the unresolved path text

### Requirement: Environment access isolation

`io/env-set` SHALL write only to its plugin instance's overlay, not the process environment.
`io/env-get` SHALL read that overlay first and then the process environment.
Separate plugin instances SHALL have independent overlays.

#### Scenario: Concurrent scripts do not corrupt each other's environment

- **WHEN** two scripts run concurrently and one sets an environment variable
- **THEN** a separate plugin instance SHALL remain unaffected and the process environment SHALL remain unchanged

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

`io/read-file` SHALL read at most 1 MiB plus one byte for size detection.
It SHALL reject content larger than 1 MiB with `ResourceLimitError`, rather than return truncated content.
A successful read SHALL charge its returned payload bytes to the evaluation allocation ledger.

#### Scenario: A read at the limit succeeds

- **WHEN** a file is read whose size is at most 1 MiB and its result fits the allocation budget
- **THEN** the full content SHALL be returned and charged

#### Scenario: A read beyond the limit fails

- **WHEN** a file is read whose size exceeds 1 MiB
- **THEN** the read SHALL fail with a typed error and SHALL NOT return an unbounded buffer

#### Scenario: The read fails closed under an exhausted budget

- **WHEN** the engine's allocation budget cannot cover the read
- **THEN** the read SHALL fail rather than complete uncharged

### Requirement: Relaxed allowlists deny access by omission

Relaxed mode SHALL require an explicit entry in the applicable read or write allowlist.
An empty or nil applicable allowlist SHALL deny access.

#### Scenario: Empty read allowlist denies reads

- **WHEN** relaxed mode has no read allowlist entries
- **THEN** read operations SHALL fail even if the path lies inside the configured root

#### Scenario: Empty write allowlist denies writes

- **WHEN** relaxed mode has no write allowlist entries
- **THEN** write operations SHALL fail

### Requirement: Directory results respect collection limits

`io/ls` SHALL check the active evaluator's collection limit before allocating the Lisp result list.
An oversized result SHALL fail with `ResourceLimitError`.
With an active collection limit, enumeration SHALL read at most that limit plus one entry before checking the result length.

#### Scenario: Oversized directory result is refused

- **WHEN** directory enumeration returns more entries than `MaxCollectionLen`
- **THEN** `io/ls` SHALL fail before constructing the Lisp result list

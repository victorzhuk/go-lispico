# Technical Specification

## ADDED Requirements

### Requirement: File sandbox is a security boundary

The `lio` file sandbox SHALL confine file access to its configured root as a
security boundary, resolving symlinks before enforcing the root and any deny
pattern, so that no symlink can reference a path outside the root.

#### Scenario: Symlink escape is denied

- **WHEN** a path traverses a symlink whose target lies outside the sandbox root
- **THEN** the operation SHALL be denied for both read and write

#### Scenario: Deny pattern is enforced on the resolved path

- **WHEN** a deny pattern matches the symlink-resolved target
- **THEN** the operation SHALL be denied, regardless of the unresolved path text

### Requirement: Environment access isolation

`io/env-*` functions SHALL either scope their reads and writes per engine, or
SHALL document that they mutate the process-global environment and are unsafe to
use from concurrent scripts sharing a process.

#### Scenario: Concurrent scripts do not corrupt each other's environment

- **WHEN** two scripts run concurrently and one sets an environment variable
- **THEN** the other SHALL either be unaffected (per-engine scope) or the shared-mutation behavior SHALL be documented as a known constraint

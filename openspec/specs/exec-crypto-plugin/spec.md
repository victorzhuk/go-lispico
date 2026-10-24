# exec-crypto-plugin Specification

## Purpose

The exec-crypto-plugin capability provides direct process execution and cryptographic utilities when the plugin is loaded.

## Requirements

### Requirement: exec-crypto-plugin implementation
The system SHALL implement the exec-crypto-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the exec-crypto-plugin SHALL be ready for use

### Requirement: Process timeouts are validated before execution

`exec/run` and `exec/pipe` SHALL require integer timeouts between 1 and 300000 milliseconds, inclusive.
The default timeout SHALL be 30000 milliseconds.
Invalid timeouts SHALL fail before a process starts.

#### Scenario: Timeout outside the accepted range fails

- **WHEN** either operation receives `:timeout` below 1 or above 300000
- **THEN** the operation SHALL return an error without starting a process

### Requirement: Captured process output is bounded

Captured stdout and stderr SHALL each have an independent 1 MiB limit.
For pipelines, these limits SHALL apply to the final stage's captured streams.
Overflow SHALL return `ResourceLimitError` instead of a truncated result.

#### Scenario: Exact output limit succeeds

- **WHEN** each captured stream contains at most 1048576 bytes and execution otherwise succeeds
- **THEN** the operation SHALL return both streams in full

#### Scenario: Excess output fails after execution

- **WHEN** either captured stream exceeds 1048576 bytes
- **THEN** the plugin SHALL discard excess bytes and wait for process completion
- **AND** the operation SHALL return `ResourceLimitError` instead of partial output

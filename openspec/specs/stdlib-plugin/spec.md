# stdlib-plugin Specification

## Purpose

The stdlib-plugin capability provides standard library functionality for the system, registered and made ready for use when the system initializes.
## Requirements
### Requirement: stdlib-plugin implementation
The system SHALL implement the stdlib-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the stdlib-plugin SHALL be ready for use

### Requirement: Builtins have a single shared implementation
Each stdlib operation SHALL have exactly one implementation, reusable across Dialects under different visible names. The stdlib SHALL NOT provide duplicate implementations that differ only by the name a Dialect exposes.

#### Scenario: One implementation serves multiple dialect names
- **WHEN** two Dialects expose the same operation under different names
- **THEN** both names SHALL resolve to the one shared implementation

#### Scenario: Adding a dialect name does not add an implementation
- **WHEN** a Dialect adds a new visible name for an existing operation
- **THEN** no new implementation of that operation SHALL be introduced in the stdlib

### Requirement: Bootstrap macros bind through the engine's evaluator

The stdlib bootstrap SHALL define its Lisp-source macros and functions through the
environment's own evaluator, so definitions land where the engine's dialect axes
(Lisp-2 function cell, truthiness) place them — never through a separately
constructed evaluator with different axes.

#### Scenario: Threading macros work under the default CL dialect

- **WHEN** `runtime.New(nil)` loads the stdlib plugin and evaluates `(-> 1 (+ 2))`
- **THEN** the result SHALL be `3`, not an `UndefinedError`

#### Scenario: All bootstrap macros resolve in head position under Lisp-2

- **WHEN** a Lisp-2 engine evaluates each of `->`, `->>`, `as->`, `if-let`, `when-let`, `get-in` in head position
- **THEN** every form SHALL resolve and evaluate without `UndefinedError`


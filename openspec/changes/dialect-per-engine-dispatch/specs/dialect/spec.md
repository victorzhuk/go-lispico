# dialect Specification

## ADDED Requirements

### Requirement: Dialect selection is fixed at Engine construction
An Engine SHALL run exactly one Dialect, selected via a construction-time option, and that Dialect SHALL be immutable for the Engine's lifetime. Evaluated code SHALL NOT be able to change the running Dialect.

#### Scenario: Dialect chosen at construction
- **WHEN** an Engine is created with a Dialect option
- **THEN** every evaluation on that Engine SHALL dispatch special forms through that Dialect's effective table

#### Scenario: Evaluated code cannot change the Dialect
- **WHEN** evaluated source attempts to alter which special forms are available
- **THEN** the effective table SHALL remain the one resolved at construction

### Requirement: A Dialect is a Delta over a declared base
A Dialect SHALL be defined as a Delta — renames, additions, and removals of special forms — over a base that is either the full Kernel table or empty. Resolving the Dialect SHALL produce the Engine's effective special-form table.

#### Scenario: Rename resolves to the canonical form
- **WHEN** a Dialect renames a canonical Kernel form to another name
- **THEN** invoking the renamed name SHALL evaluate the canonical form
- **AND** the original canonical name SHALL NOT resolve unless the Delta also keeps it

#### Scenario: Removal makes a form uncallable
- **WHEN** a Dialect removes a form from its base
- **THEN** invoking that form SHALL fail as undefined

### Requirement: Empty-base Dialects are fail-closed
A Dialect built on the empty base SHALL expose only the special forms its Delta explicitly adds. A special form added to the Kernel table by a later change SHALL NOT become callable in an empty-base Dialect unless its Delta adds it.

#### Scenario: Unlisted kernel form is rejected
- **WHEN** an empty-base Dialect omits a kernel special form from its Delta
- **THEN** invoking that form under the Dialect SHALL fail as undefined

### Requirement: Per-Engine dispatch isolation
Two Engines running different Dialects in one process SHALL NOT share special-form dispatch state.

#### Scenario: Divergent Dialects on concurrent Engines
- **WHEN** two Engines are constructed with different Dialects
- **THEN** a form present in one Dialect and absent in the other SHALL resolve only on the Engine whose Dialect defines it

### Requirement: Default Engine behavior is preserved
An Engine created without a Dialect option SHALL evaluate the identity Dialect, reproducing the special-form behavior of the interpreter prior to this change.

#### Scenario: No option selects the identity Dialect
- **WHEN** an Engine is created with no Dialect option
- **THEN** the special forms `if`, `def`, `defn`, `let`, `quote`, `cond`, `loop`, and `recur` SHALL behave as they did before this change

# dialect Specification

## ADDED Requirements

### Requirement: Vocabulary is a name map over shared implementations
A Dialect SHALL present builtins under dialect-specific names via a vocabulary map from a visible name to a shared builtin implementation. A Dialect SHALL NOT carry its own copy of an implementation that the shared core already provides.

#### Scenario: Renamed builtin resolves to the shared implementation
- **WHEN** an Engine runs a Dialect mapping `car` to the shared first-implementation
- **THEN** `(car '(1 2 3))` SHALL evaluate to `1` using that shared implementation

#### Scenario: Semantics-differing name uses an adapter over the shared core
- **WHEN** a Dialect maps a name whose semantics differ from the shared core by argument order or arity
- **THEN** the name SHALL resolve through a thin adapter over the shared implementation, not a duplicated implementation

### Requirement: Empty-base vocabulary is fail-closed
An empty-base Dialect's vocabulary SHALL be an allowlist. A builtin whose name is absent from the Dialect's vocabulary map SHALL be uncallable, and a builtin added to the shared core by a later change SHALL NOT become callable unless the map adds it.

#### Scenario: Unlisted builtin is rejected
- **WHEN** an empty-base Dialect omits a builtin from its vocabulary map
- **THEN** invoking that builtin under the Dialect SHALL fail as undefined

### Requirement: Identity Dialect vocabulary is unchanged
The identity Dialect SHALL map today's builtin names onto today's implementations, so an Engine created without changing vocabulary resolves builtins exactly as before this change.

#### Scenario: Default Engine vocabulary is preserved
- **WHEN** an Engine is created without changing vocabulary
- **THEN** the builtins registered by loaded plugins SHALL be callable under their current names

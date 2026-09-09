## ADDED Requirements

### Requirement: Meter attachment preserves call deadlines

Attaching a context meter, an engine meter, or existing evaluation state SHALL NOT disable the configured timeout of `Engine.Call`, `Fn.Call`, or `PinnedFn.Call`. Each call SHALL retain the same deadline ownership and cooperative enforcement as its unmetered equivalent. Reentry SHALL preserve the enclosing absolute evaluation deadline and shared resource budget. `WithTimeout(0)` SHALL add no engine deadline and SHALL NOT clear an enclosing evaluation's deadline. Earlier caller deadlines SHALL continue to govern; later caller deadlines SHALL not weaken the engine's bound.

#### Scenario: Context meter retains the engine bound

- **WHEN** a VM engine with a positive timeout invokes cooperative work through any call handle with a context meter and no caller deadline
- **THEN** the engine deadline SHALL remain present and expiry SHALL return a deadline error

#### Scenario: Engine meter retains the engine bound

- **WHEN** an engine meter supplies the meter for a named, shared-handle, or pinned call with no caller deadline
- **THEN** the configured engine timeout SHALL govern that call exactly as without the meter

#### Scenario: Existing state without a deadline acquires the bound

- **WHEN** a top-level call receives existing evaluation state with no inherited deadline and the engine timeout is positive
- **THEN** the engine SHALL enforce its configured timeout while preserving that state's resource budget

#### Scenario: Nested call does not restart its deadline

- **WHEN** a metered function reenters a named or handle call using its evaluation context
- **THEN** the nested call SHALL retain the enclosing absolute deadline without extending it or starting an independent budget

#### Scenario: Disablement and caller bounds still compose

- **WHEN** metered calls use timeout disablement or a caller deadline earlier or later than the engine bound
- **THEN** deadline selection SHALL match the existing deadline-ownership contract, and disablement SHALL leave an inherited evaluation deadline intact

#### Scenario: Unobserved unmetered calls stay lean

- **WHEN** an unmetered call without callbacks or existing evaluation state completes without observing a deadline
- **THEN** this change SHALL add no boundary clock read, timer, or derived deadline context

# dialect — delta

## ADDED Requirements

### Requirement: Stock dialect construction is memoized and immutable

The stock dialect constructors (`cl.Dialect()`, `clojure.Dialect()`) SHALL
return a process-memoized value: repeated calls SHALL NOT rebuild the delta
chain, the vocabulary map, the resolved special-form table, or the dialect
fingerprint. All state shared through memoization SHALL be immutable after
construction; nothing observable through a `Dialect` value SHALL change over
the process lifetime. `Fingerprint()` SHALL return the same digest it
returns without memoization. Per-engine dispatch isolation SHALL be
preserved: engine-level definition or redefinition of operators SHALL affect
only that engine's environment, never the shared resolution state, and two
engines constructed from one memoized dialect SHALL behave as two engines
constructed from independently built equal dialects. Custom dialects built
from deltas SHALL behave exactly as before.

#### Scenario: Repeated construction shares one resolution

- **WHEN** two engines are constructed with `clojure.Dialect()` in one process
- **THEN** dialect resolution work SHALL be performed once, and both engines SHALL evaluate the dialect test corpus with results identical to independently constructed dialects

#### Scenario: Redefinition on one engine does not leak through the shared dialect

- **WHEN** one engine redefines an operator name that the shared dialect resolves
- **THEN** the other engine's evaluation of that name SHALL be unaffected

#### Scenario: Fingerprint is stable under memoization

- **WHEN** `Fingerprint()` is read from the memoized stock dialect and from a structurally identical dialect built without memoization
- **THEN** the digests SHALL be equal

# runtime-api — delta

## MODIFIED Requirements

### Requirement: WithDialect Engine option
The runtime SHALL provide a `WithDialect` construction option that selects the Dialect an Engine runs. The option SHALL be applied once at `New` and SHALL compose with the existing construction options, including the bytecode evaluator: any resolvable Dialect SHALL be accepted alongside `WithBytecode()`.

#### Scenario: Selecting a Dialect at construction
- **WHEN** an Engine is created with `WithDialect` set to a given Dialect
- **THEN** the Engine SHALL evaluate source using that Dialect's effective special-form table

#### Scenario: Omitting the option
- **WHEN** an Engine is created without `WithDialect`
- **THEN** the Engine SHALL run the default Dialect, preserving prior behavior until the default is changed by a later change

#### Scenario: Bytecode composes with any Dialect
- **WHEN** an Engine is created with both `WithBytecode()` and a non-identity Dialect (the default CL, or a restricted dialect)
- **THEN** construction SHALL succeed and evaluation SHALL honor the Dialect's forms and axes on the bytecode path

#### Scenario: Unresolvable Dialect fails construction
- **WHEN** an Engine is created with a Dialect whose delta references a canonical form absent from the kernel
- **THEN** construction SHALL return an error rather than a partially-resolved Engine

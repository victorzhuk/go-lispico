# runtime-api — delta

## ADDED Requirements

### Requirement: ResourceLimits Engine option

The runtime SHALL provide a construction option that sets a `ResourceLimits` value
carrying the reader nesting-depth, evaluator structural-depth, collection-length,
and chunk-cache-size ceilings. The option SHALL be applied once at `New` and SHALL
be immutable for the Engine's lifetime, so evaluated code cannot raise its own
ceilings. When the option is omitted, or a field is left at its zero value, the
Engine SHALL apply a conservative built-in default for that ceiling — the absence
of a limit SHALL mean "use the default", never "unlimited". The limits SHALL be
carried into the reader, the evaluator, and the stdlib so each enforcement point
uses the Engine's configured value.

#### Scenario: Configured limit takes effect

- **WHEN** an Engine is created with a `ResourceLimits` that lowers the reader nesting-depth ceiling and then reads source nested past that ceiling
- **THEN** `Read`/`Eval` SHALL fail with the depth-limit error at the configured ceiling

#### Scenario: Omitted option applies safe defaults

- **WHEN** an Engine is created with no `ResourceLimits` option and is given adversarially deep input
- **THEN** the Engine SHALL still fail closed at its default ceilings rather than crashing the process

#### Scenario: Limits are immutable after construction

- **WHEN** an Engine is running and evaluated code attempts to change any ceiling
- **THEN** no evaluation path SHALL be able to raise a ceiling, and the Engine SHALL enforce the value fixed at `New`

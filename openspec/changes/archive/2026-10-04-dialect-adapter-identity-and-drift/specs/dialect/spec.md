## ADDED Requirements

### Requirement: Each Dialect binds its own adapter values

A Dialect's vocabulary SHALL bind the adapter values its own specification declared: two Dialects with equal fingerprints and different adapter values SHALL each install their own value, in any order of construction, and a value bound for another Dialect SHALL NOT be observed in place of the Dialect's own.

#### Scenario: Equal fingerprints keep their own adapter values

- **WHEN** two Engines are constructed from two specifications whose adapter maps declare the same visible name and the same semantic ID but different values, and both load the same plugin
- **THEN** each Engine SHALL call its own adapter value, not the other Engine's

## MODIFIED Requirements

### Requirement: Dialects are constructed from a validated specification

The system SHALL construct a Dialect from a plain-data specification in one step that validates, resolves and fingerprints it, returning either a frozen Dialect or an error together with the zero Dialect. Construction SHALL reject: a base that is neither `BaseFull` nor `BaseEmpty`, a form mapping to an unknown kernel form, a hidden name absent from the base, a name both hidden and mapped as a visible form, a Lisp-2 specification that maps `funcall` or `function` as a visible form, an adapter without a semantic ID, an adapter without a value, and a name present both as a vocabulary rename and as an adapter. The returned Dialect SHALL NOT observe later mutation of the specification's maps or slices. The fingerprint SHALL be a function of the resolved configuration only, so two specifications with identical base, dispatch, axes, vocabulary (including whether one is configured) and adapter IDs SHALL produce equal fingerprints. The zero-value Dialect SHALL behave as the identity Dialect: the full kernel with no delta, the Lisp-1 namespace and no vocabulary.

#### Scenario: Invalid specification fails at construction

- **WHEN** a specification maps a visible name to a kernel form that does not exist
- **THEN** construction SHALL return an error and the zero Dialect

#### Scenario: Conflicting vocabulary and adapter entries are rejected

- **WHEN** a specification names `nth` both in its vocabulary map and in its adapters
- **THEN** construction SHALL return an error

#### Scenario: Hidden and mapped name is rejected

- **WHEN** a specification hides a name and also maps the same name as a visible form
- **THEN** construction SHALL return an error

#### Scenario: Lisp-2 reserved form names are rejected

- **WHEN** a Lisp-2 specification maps `funcall` or `function` as a visible form
- **THEN** construction SHALL return an error

#### Scenario: Adapter without a value is rejected

- **WHEN** a specification declares an adapter with a semantic ID and no value
- **THEN** construction SHALL return an error

#### Scenario: Specification mutation does not leak

- **WHEN** the caller modifies the specification's vocabulary map after construction
- **THEN** the Dialect's vocabulary and fingerprint SHALL be unchanged

#### Scenario: Zero value is the identity Dialect

- **WHEN** an Engine is constructed with `WithDialect(core.Dialect{})`
- **THEN** it SHALL dispatch every kernel form under its kernel name and resolve builtins under their registered names

#### Scenario: Equal dispatch yields equal fingerprints

- **WHEN** two specifications express the same base, resolved form table, axes, vocabulary and adapter IDs through different but equivalent entries
- **THEN** their fingerprints SHALL be equal

#### Scenario: Configured empty vocabulary differs from no vocabulary

- **WHEN** two empty-base specifications differ only in that one configures an empty vocabulary and the other configures none
- **THEN** their fingerprints SHALL differ

#### Scenario: Unknown base is rejected

- **WHEN** a specification sets `Base` to a `DialectBase` value that is neither `BaseFull` nor `BaseEmpty`
- **THEN** construction SHALL return an error and the zero Dialect

### Requirement: Dialect adapters accept every callable kind

A Dialect adapter that takes a function argument SHALL accept every value the evaluator can call, including values produced by the bytecode VM, and SHALL decide callability through `core.IsCallable`, one core predicate, rather than an adapter-local list of concrete types.

#### Scenario: VM closure passed to a CL adapter

- **WHEN** a CL Engine on the VM evaluates `(mapcar (fn (x) (* x 2)) '(1 2))`
- **THEN** the result SHALL be `(2 4)`

#### Scenario: Non-callable argument is a type error

- **WHEN** a CL Engine evaluates `(mapcar 5 '(1 2))`
- **THEN** evaluation SHALL fail with a type error naming a function as the expected type

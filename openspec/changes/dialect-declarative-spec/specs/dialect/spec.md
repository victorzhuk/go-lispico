## ADDED Requirements

### Requirement: Dialects are constructed from a validated specification

The system SHALL construct a Dialect from a plain-data specification in one step that validates, resolves and fingerprints it, returning either a frozen Dialect or an error. Construction SHALL reject: a form mapping to an unknown kernel form, a hidden name absent from the base, an adapter without a semantic ID, and a name present both as a vocabulary rename and as an adapter. The returned Dialect SHALL NOT observe later mutation of the specification's maps or slices. The fingerprint SHALL be a function of the resolved configuration only, so two specifications with identical dispatch, axes, vocabulary and adapter IDs SHALL produce equal fingerprints. The zero-value Dialect SHALL behave as the identity Dialect: the full kernel with no delta, the Lisp-1 namespace and no vocabulary.

#### Scenario: Invalid specification fails at construction

- **WHEN** a specification maps a visible name to a kernel form that does not exist
- **THEN** construction SHALL return an error and no Dialect

#### Scenario: Conflicting vocabulary and adapter entries are rejected

- **WHEN** a specification names `nth` both in its vocabulary map and in its adapters
- **THEN** construction SHALL return an error

#### Scenario: Specification mutation does not leak

- **WHEN** the caller modifies the specification's vocabulary map after construction
- **THEN** the Dialect's vocabulary and fingerprint SHALL be unchanged

#### Scenario: Zero value is the identity Dialect

- **WHEN** an Engine is constructed with `WithDialect(core.Dialect{})`
- **THEN** it SHALL dispatch every kernel form under its kernel name and resolve builtins under their registered names

#### Scenario: Equal dispatch yields equal fingerprints

- **WHEN** two specifications express the same resolved form table, axes, vocabulary and adapter IDs through different but equivalent entries
- **THEN** their fingerprints SHALL be equal

## MODIFIED Requirements

### Requirement: A Dialect is a Delta over a declared base

A Dialect SHALL be defined as a delta over a declared base: the full kernel table or an empty table. The delta SHALL be expressed as a specification of forms added under visible names and kernel names hidden from the base, and resolving it SHALL yield one effective name→form table. Rename SHALL be expressed as adding the canonical form under the new name and hiding the canonical name.

#### Scenario: Rename resolves to the canonical form

- **WHEN** a Dialect renames a canonical Kernel form to another name
- **THEN** invoking the renamed name SHALL evaluate the canonical form
- **AND** the original canonical name SHALL NOT resolve unless the Delta also keeps it

#### Scenario: Removal makes a form uncallable

- **WHEN** a Dialect removes a form from its base
- **THEN** invoking that form SHALL fail as undefined

#### Scenario: Rename through the specification

- **WHEN** a full-base specification maps `setq` to `set!` and hides `set!`
- **THEN** `setq` SHALL dispatch the `set!` kernel form and `set!` SHALL be undefined

#### Scenario: Empty base exposes only specified forms

- **WHEN** an empty-base specification maps only `if` and `let`
- **THEN** only `if` and `let` SHALL dispatch as special forms

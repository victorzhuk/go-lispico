## ADDED Requirements

### Requirement: Dialects are constructed from a validated specification

The system SHALL construct a Dialect from a plain-data specification in one step that validates, resolves and fingerprints it, returning either a frozen Dialect or an error together with the zero Dialect. Construction SHALL reject: a form mapping to an unknown kernel form, a hidden name absent from the base, a name both hidden and mapped as a visible form, a Lisp-2 specification that maps `funcall` or `function` as a visible form, an adapter without a semantic ID, an adapter without a value, and a name present both as a vocabulary rename and as an adapter. The returned Dialect SHALL NOT observe later mutation of the specification's maps or slices. The fingerprint SHALL be a function of the resolved configuration only, so two specifications with identical base, dispatch, axes, vocabulary (including whether one is configured) and adapter IDs SHALL produce equal fingerprints. The zero-value Dialect SHALL behave as the identity Dialect: the full kernel with no delta, the Lisp-1 namespace and no vocabulary.

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

### Requirement: Stock dialect construction is memoized and immutable

The stock dialect constructors (`cl.Dialect()`, `clojure.Dialect()`) SHALL
return a process-shared value built once from a static specification by the
validated constructor: repeated calls SHALL NOT rebuild the vocabulary map,
the resolved special-form table, or the dialect fingerprint. All shared state
SHALL be immutable after construction; nothing observable through a `Dialect`
value SHALL change over the process lifetime. `Fingerprint()` SHALL equal the
fingerprint of an independently constructed equal specification. Per-engine
dispatch isolation SHALL be preserved: engine-level definition or redefinition
of operators SHALL affect only that engine's environment, never the shared
resolution state, and two engines constructed from one shared stock dialect
SHALL behave as two engines constructed from independently built equal
dialects.

#### Scenario: Repeated construction shares one resolution

- **WHEN** two engines are constructed with `clojure.Dialect()` in one process
- **THEN** dialect resolution work SHALL be performed once, and both engines SHALL evaluate the dialect test corpus with results identical to independently constructed dialects

#### Scenario: Redefinition on one engine does not leak through the shared dialect

- **WHEN** one engine redefines an operator name that the shared dialect resolves
- **THEN** the other engine's evaluation of that name SHALL be unaffected

#### Scenario: Fingerprint is stable under memoization

- **WHEN** `Fingerprint()` is read from a stock dialect and from a dialect independently constructed from an equal specification
- **THEN** the digests SHALL be equal

### Requirement: Dialect adapters have semantic fingerprint identity

Every Dialect adapter SHALL have a non-empty stable semantic ID/version supplied
in the specification's adapter entry and stored in its `VocabEntry`. The Dialect
fingerprint SHALL include that ID with the visible/canonical names and SHALL
change when the ID/configuration changes. It SHALL NOT use a function pointer or
only the adapter's Go concrete type as semantic identity. Dialect construction
SHALL reject an adapter entry with an empty ID.

#### Scenario: Adapter semantics participate in the fingerprint

- **WHEN** two otherwise identical Dialects bind an adapter under the same visible name with different semantic IDs or versions
- **THEN** their fingerprints SHALL differ, while repeated construction with the same stable ID SHALL produce the same fingerprint

#### Scenario: Empty adapter identity fails closed

- **WHEN** a specification declares an adapter with an empty semantic ID
- **THEN** construction SHALL return an error rather than a Dialect with an ambiguous fingerprint

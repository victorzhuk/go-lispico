## ADDED Requirements

### Requirement: Lisp-2 dialects bridge every plugin builtin

Under a Lisp-2 Dialect, every GoFunc a plugin registers and the Dialect exposes SHALL be callable in head position, whether or not the Dialect has a vocabulary, and whether the builtin was registered eagerly or through a lazy template.

#### Scenario: Lisp-2 without vocabulary

- **WHEN** an Engine runs `core.NewDialect(core.DialectSpec{Lisp2: true})` with the stdlib and json plugins loaded
- **THEN** `(json/encode 1)` and `(+ 1 2)` SHALL both evaluate successfully on both execution paths

### Requirement: Host bindings survive plugin vocabulary passes

A binding the host creates through the Engine SHALL NOT be removed or replaced by the vocabulary or allowlist processing of a later plugin load or reload.

#### Scenario: Empty-base allowlist keeps a host binding

- **WHEN** an empty-base Dialect Engine binds `host/f` through `Bind` and then loads another plugin
- **THEN** `(host/f)` SHALL still evaluate to the host function's result

### Requirement: Vocabulary registration is one rule set on every path

Vocabulary renames, adapters, the empty-base allowlist and the Lisp-2 function-cell mirror SHALL be decided by one rule set shared by eager and lazy registration, so the same Dialect and plugins produce the same bindings, canonical flags and function cells on either path. Loading a plugin SHALL process only the names that operation registers.

#### Scenario: Eager and lazy agree

- **WHEN** two Engines with the same Dialect load the stdlib, one eagerly and one through lazy templates
- **THEN** every exposed name SHALL have the same value-cell binding, canonical flag and function-cell binding after materialization

#### Scenario: Loading a plugin does not rewrite unrelated bindings

- **WHEN** a CL Engine with the stdlib loaded then loads json
- **THEN** no binding registered by the stdlib SHALL be rewritten or journaled by the json operation

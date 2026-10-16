## Purpose
Define per-engine Lisp dialect selection, reader syntax, symbol namespaces and
builtin vocabulary while preserving shared evaluation semantics and isolation. An
Engine constructed with no `Dialect` option runs the Common Lisp dialect; the
identity-dialect surface (Lisp-1 symbol behavior, the identity reader flags, the
identity vocabulary) is reached only through an explicit selection. Ground truth:
`runtime/engine.go:305-310` (`dialect: cl.Dialect()` in the engine config),
`cl/cl.go:203-213` (the CL axes and renames), and
`openspec/specs/runtime-api/spec.md:57-62` ("Default dialect is Common Lisp").

## ADDED Requirements

### Requirement: The no-option default dialect is Common Lisp

An Engine constructed without a Dialect option SHALL evaluate source with the Common Lisp dialect. The prior identity-dialect surface SHALL be available only through explicit selection, and the identity-dialect namespace, reader-flag, and vocabulary guarantees SHALL be read as applying to an Engine that selected the identity or Clojure dialect. A Dialect that is explicitly selected SHALL behave as its own specification requires.

#### Scenario: A no-option Engine speaks Common Lisp

- **WHEN** an Engine is constructed with no Dialect option and evaluates Common Lisp source
- **THEN** it SHALL evaluate that source as the Common Lisp dialect, including its renames and namespace axis

#### Scenario: The prior surface is available by explicit selection

- **WHEN** an Engine is constructed with an explicit Clojure or identity Dialect
- **THEN** the identity-preserving namespace, reader-flag, and vocabulary behavior SHALL hold on that Engine

#### Scenario: A no-option Engine does not get the identity surface implicitly

- **WHEN** an Engine is constructed with no Dialect option and is evaluated with the identity dialect's Lisp-1 symbol behavior assumed without selection
- **THEN** the Engine SHALL NOT be treated as having the identity dialect

#### Scenario: Explicit selection is unaffected by the default

- **WHEN** two Engines are constructed, one with no Dialect option and one with an explicitly selected Clojure dialect
- **THEN** the explicitly selected Engine SHALL resolve every form its dialect defines regardless of the other Engine's dialect
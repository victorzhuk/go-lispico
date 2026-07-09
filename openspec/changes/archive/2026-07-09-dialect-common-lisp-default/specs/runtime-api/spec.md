# runtime-api Specification

## ADDED Requirements

### Requirement: Default dialect is Common Lisp
An Engine created via `runtime.New()` without a `WithDialect` option SHALL run the Common Lisp dialect. Embedders requiring the prior surface SHALL select it explicitly with `WithDialect(clojure.Dialect())`.

#### Scenario: Zero-config Engine speaks Common Lisp
- **WHEN** an Engine is created with no dialect option
- **THEN** it SHALL evaluate source using the Common Lisp dialect

#### Scenario: Prior surface available by explicit selection
- **WHEN** an Engine is created with `WithDialect(clojure.Dialect())`
- **THEN** it SHALL reproduce the interpreter's behavior prior to the default flip

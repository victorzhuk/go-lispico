# core-engine — delta

## ADDED Requirements

### Requirement: Reader errors carry token positions

Reader errors SHALL report the line and column of the offending token whenever the
tokenizer recorded one — including invalid numeric literals and unexpected EOF —
never a placeholder `0,0`.

#### Scenario: Invalid number reports its position

- **WHEN** parsing source containing an invalid numeric literal on line 3
- **THEN** the returned `*core.LispicoError` SHALL carry `Line: 3` and the token's column

#### Scenario: Unexpected EOF reports the end position

- **WHEN** parsing source that ends mid-form
- **THEN** the returned error SHALL carry the EOF token's recorded line and column

## MODIFIED Requirements

### Requirement: Typed evaluation errors

Evaluation failures SHALL be reported as `*core.LispicoError` carrying a `Code`,
and SHALL include a source position (`Line`, `Col`, `Source`) when the failing form
carries one. An uncaught `throw` SHALL surface as a `*core.LispicoError`, not an
untyped error.

#### Scenario: errors.As recovers a typed error

- **WHEN** an evaluation fails on arity, type, an undefined symbol, or a general eval error
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and `lispicoErr.Code` SHALL classify the failure

#### Scenario: Uncaught throw is typed

- **WHEN** `(throw "boom")` is evaluated with no enclosing `try`
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and the error SHALL carry the thrown value's rendering

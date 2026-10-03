## ADDED Requirements

### Requirement: Compiler construction never panics

Constructing a compiler SHALL NOT panic for any Dialect value, including the zero value, which SHALL compile as the full kernel with no delta.

#### Scenario: Zero-value dialect

- **WHEN** a compiler is constructed with the zero-value Dialect and compiles `(if true 1 2)`
- **THEN** compilation SHALL succeed and the chunk SHALL evaluate to `1`

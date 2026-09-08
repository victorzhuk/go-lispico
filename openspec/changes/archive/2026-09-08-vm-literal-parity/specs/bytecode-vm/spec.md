## ADDED Requirements

### Requirement: Compiled quote requires exactly one operand

The bytecode compiler SHALL reject `quote` with zero or multiple operands using
a typed compile error before emitting executable quotation bytecode. A valid
quotation SHALL return its datum without evaluating it. Both evaluator modes
SHALL reject malformed quote arity and accept valid quotation under each shipped
dialect.

#### Scenario: Extra quote operands are rejected

- **WHEN** `(quote 1 2)` is evaluated
- **THEN** both evaluator modes SHALL return a typed error rather than silently returning `1`

#### Scenario: Missing quote operand remains rejected

- **WHEN** `(quote)` is evaluated
- **THEN** both evaluator modes SHALL return a typed arity or compile/evaluation error without a panic

#### Scenario: Valid quote does not evaluate its datum

- **WHEN** `(quote missing)` is evaluated without a binding for `missing`
- **THEN** both evaluator modes SHALL return the symbol value without an undefined-symbol error

### Requirement: Evaluated empty lists retain their value type

An evaluated empty list SHALL remain an empty list under the VM, with the same
value, runtime type and dialect-dependent behavior as under the tree-walker.
Compilation SHALL NOT substitute `nil` for that list. Loading this existing
literal datum SHALL preserve the tree-walker's construction-charge behavior.

#### Scenario: Bare empty list remains a list

- **WHEN** `()` is evaluated through either shipped dialect
- **THEN** both evaluator modes SHALL return an empty list of the same runtime type rather than substituting `nil`

#### Scenario: Quoted empty list remains unchanged

- **WHEN** `(quote ())` is evaluated
- **THEN** both evaluator modes SHALL return the same empty list value

#### Scenario: Repeated evaluation preserves empty-list identity by value

- **WHEN** `()` is evaluated repeatedly on one VM engine, including a cached execution
- **THEN** every result SHALL remain an empty list with the same value and type as the tree-walker result

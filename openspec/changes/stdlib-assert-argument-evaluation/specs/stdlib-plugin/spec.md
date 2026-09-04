## ADDED Requirements

### Requirement: assert reports against its received arguments

`assert` is a Builtin, so its arguments arrive already evaluated. It SHALL use
those values as received and SHALL NOT evaluate them again. In particular, an
argument whose value is a `Symbol` SHALL NOT be resolved as a binding and an
argument whose value is a `List` SHALL NOT be applied as a call.

A failing assertion SHALL report its own message. When a message argument is
supplied, the reported error SHALL be built from that argument's value; when
none is supplied, it SHALL be the bare assertion failure. Arity errors, the
truthiness rule that selects the failure branch, the error's code, and the value
returned on success SHALL be unchanged.

#### Scenario: A quoted symbol message is reported, not resolved

- **WHEN** a script asserts a false condition with a message argument whose value is a symbol that is not bound in scope
- **THEN** the error SHALL be the assertion failure naming that symbol, not an undefined-binding error

#### Scenario: A list message is reported, not applied

- **WHEN** a script asserts a false condition with a message argument whose value is a list
- **THEN** the error SHALL be the assertion failure rendering that list, not the result of applying it as a call

#### Scenario: Reporting is identical across execution modes and dialects

- **WHEN** the same failing assertion runs under the tree-walker and under the bytecode VM, in each dialect
- **THEN** the reported error SHALL be the same in all four combinations

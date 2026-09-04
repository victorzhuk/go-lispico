## ADDED Requirements

### Requirement: The format pre-charge bounds a mismatched verb

`format` charges an estimate before `fmt.Sprintf` runs so that a render too
large for the remaining budget never happens. That guarantee requires the
estimate to be an upper bound on the rendered output for every directive,
including one whose verb the operand's type cannot satisfy.

Where `fmt` renders a verb/operand mismatch by printing the operand inside a
diagnostic, the estimator SHALL size that directive against the operand it will
render, not against a constant for the verb. An explicit argument index that
directs several directives at one operand SHALL be counted once per directive.

A precision on a floating-point verb SHALL NOT reduce the estimate below what
the verb renders without one.

#### Scenario: A verb the operand cannot satisfy is charged for what it renders

- **WHEN** a format string pairs a verb with an operand whose type `fmt` cannot render with it
- **THEN** the pre-charge SHALL be at least the length of the rendered output, and a call whose render exceeds the remaining allocation budget SHALL be refused before the render happens

#### Scenario: One operand feeding many directives is charged per directive

- **WHEN** a format string uses an explicit argument index so that a single large operand is named by several directives
- **THEN** the pre-charge SHALL account for each directive's rendered output rather than for the operand once

#### Scenario: A precision does not shrink a float estimate

- **WHEN** a floating-point verb carries a precision and the operand's magnitude requires more integer digits than the precision covers
- **THEN** the estimate SHALL remain at least what the same verb without a precision would estimate

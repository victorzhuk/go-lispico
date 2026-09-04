## ADDED Requirements

### Requirement: The format pre-charge is computed against the rendered arguments

`format` charges an estimate of its output before `fmt.Sprintf` runs, so that a
render too large for the remaining budget never happens. That guarantee holds
only while the estimator reads a format string the same way `fmt` does.

The estimator SHALL agree with `fmt` on which argument each directive refers to.
An explicit argument index that `fmt` refuses SHALL be refused by the estimator
too, rather than being reduced into range by unchecked arithmetic and used to
select a different argument. Where `fmt` declines to honour a literal — an
out-of-range argument index, or a width or precision beyond what it parses — the
estimator SHALL size the directive as `fmt` will actually render it.

#### Scenario: An out-of-range argument index does not shrink the pre-charge

- **WHEN** a format string carries an explicit argument index that `fmt` refuses, followed by a directive naming a large argument
- **THEN** the pre-charge SHALL NOT be computed against a smaller argument than the one rendered, and the render SHALL NOT proceed when its output will not fit the remaining budget

#### Scenario: Refused literals render as fmt renders them

- **WHEN** a directive carries a literal `fmt` will not honour
- **THEN** the estimate SHALL match the error text `fmt` produces rather than the value the literal's digits spell

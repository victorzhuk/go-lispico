## ADDED Requirements

### Requirement: Resource counters cannot be wrapped past their limit

Every counter that enforces a resource ceiling SHALL refuse a charge it cannot
represent, and SHALL NOT rely on the arithmetic staying in range. This applies to
the evaluator's meter and to the bytecode VM's own reduction and allocation
counters alike: a counter that adds before it compares admits everything once the
addition wraps.

A charge that would exceed the limit SHALL be refused. A refused charge SHALL
leave the counter in a state that still refuses — failing closed once is not
sufficient if the refusal itself makes the next charge succeed. A counter SHALL
NOT become negative.

#### Scenario: A refused charge does not admit the next one

- **WHEN** a charge near the representable maximum is refused, and any further charge is then made against the same counter
- **THEN** that further charge SHALL also be refused, whatever its size

#### Scenario: Both execution modes enforce the same ceiling

- **WHEN** a script exhausts its reduction or allocation budget under the bytecode VM's own counters rather than the evaluator's meter
- **THEN** the ceiling SHALL be enforced identically to the tree-walker's, including at the representable maximum

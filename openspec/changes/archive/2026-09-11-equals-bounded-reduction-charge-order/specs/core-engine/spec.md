## ADDED Requirements

### Requirement: Reduction charges do not depend on map iteration order

The reduction units charged for evaluating a form SHALL be a function of that form and
the values it operates on. Where a charged walk visits a collection, the number of
units it charges SHALL NOT depend on the order it visited that collection in.

This binds the case where a walk stops early. A walk that charges per visited element
and exits on the first mismatch charges a count decided by where the mismatch fell, so
either the visiting order is derived from the data or the charge is taken from the
collection's size rather than from the walk's progress. Which of the two is an
implementation choice; the reproducibility is not.

It is stated separately from the allocation-side property because the two ledgers are
charged by different code and a path can satisfy one while violating the other:
`equalsBounded` allocates nothing and charged a reduction count that varied by up to
n-1 units on unchanged input.

#### Scenario: Two unequal maps compared repeatedly charge one reduction count

- **WHEN** two hash maps above the small-map threshold hold unequal contents, the receiver is in builder form so that its entries are held in a Go map rather than in the trie, and the pair is compared under a reduction budget repeatedly within one process
- **THEN** every comparison SHALL charge the identical number of reduction units, and repeating the sequence in a new process SHALL charge that same number again

#### Scenario: Equal contents charge equally regardless of build order

- **WHEN** two pairs of hash maps hold the same contents and differ only in the order their entries were inserted, and each pair is compared under a reduction budget
- **THEN** both comparisons SHALL charge the same number of reduction units

#### Scenario: The comparison's answer is unchanged

- **WHEN** any pair of values is compared under a reduction budget after the charge is made reproducible
- **THEN** the boolean answer and the budget-exhaustion behavior SHALL be what they were before, so that only the number charged moves

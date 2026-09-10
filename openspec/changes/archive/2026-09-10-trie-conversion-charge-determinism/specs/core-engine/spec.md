## ADDED Requirements

### Requirement: Allocation charges are reproducible

An allocation charge SHALL be a function of the input that produced it. Charging the
same operation over the same value SHALL yield the same number of bytes every time,
within one process and across processes, so that a difference between two measurements
is evidence of a difference in the work.

Where an operation's storage cost depends on the order in which it visits a collection,
that order SHALL be derived from the data rather than from Go map iteration, which is
randomised per range. An operation MAY visit in any order it likes; what it charges
SHALL NOT depend on which order it chose.

This is the property ADR 0008's consumer gate compares releases on and ADR 0011's unit
tables describe. It is stated here because a metered path violated it undetected: the
builder-to-trie conversion charged a ~25% spread on unchanged input.

#### Scenario: One value converted repeatedly charges one number

- **WHEN** a map above the small-map threshold in builder form is updated by `Assoc` or `Dissoc` repeatedly, each update taken against that same retained receiver so that each one performs the conversion afresh
- **THEN** every one of those updates SHALL charge the identical number of bytes for the conversion, and repeating the whole sequence in a new process SHALL charge that same number again

#### Scenario: Equal contents charge equally regardless of build order

- **WHEN** two maps above the small-map threshold hold equal contents but were bulk-built by inserting their pairs in different orders, and each is then updated by `Assoc`
- **THEN** both updates SHALL charge the same number of bytes, because the charge follows the contents and not the construction history

#### Scenario: A reproducible charge is still an honest one

- **WHEN** the conversion allocates intermediate structure that the finished trie does not retain
- **THEN** the charge SHALL account for the storage the operation actually obtained rather than only the storage it kept, so that reproducibility is not bought by under-billing

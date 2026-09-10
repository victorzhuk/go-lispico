## MODIFIED Requirements

### Requirement: Map representation efficiency

`HashMap` SHALL keep its public semantics — immutable operations, key domain,
`Int`/`Float` key distinctness, deterministic iteration — while meeting
efficiency bounds: a map operation SHALL NOT format a key into a string;
iterating a map SHALL NOT allocate or re-sort per call for maps at or below the
small-map threshold; and constructing, reading, or copying a small map SHALL
allocate O(1) objects. Promotion between the small and large representations
SHALL be semantically invisible: equality, iteration order rules, printing, and
immutability are identical at both representations.

Above the small-map threshold an immutable update SHALL NOT copy the whole map:
`Assoc` and `Dissoc` SHALL share the untouched majority of the structure with the
receiver, so that the storage a single update allocates is bounded by the depth of
the structure rather than by its entry count, and extending a map n times costs
O(n log n) in total rather than O(n²). The receiver SHALL be unaffected by an
update derived from it.

That bound SHALL hold whichever builder produced the receiver. A map above the
threshold may be held in either large representation, and converting one to the
other is a per-value cost, not a per-update one: repeated updates against one
retained receiver SHALL NOT each pay a cost proportional to its entry count.

The bound is per value per converter, not per value outright. An implementation
MAY convert a receiver more than once when concurrent updates reach it before
any conversion is published, and each such update SHALL be charged the
conversion it performed, because it allocated that storage. What is forbidden is
paying the conversion once per update: the number of conversions a value can be
charged for SHALL be bounded by the number of updates that raced to convert it,
never by the number of updates it receives.

The hash backing that structure SHALL be derived from fixed constants rather than
a per-process random seed. A randomized seed would make the structure's shape, and
anything derived from it, differ across restarts for identical input, which
contradicts the determinism this requirement states.

#### Scenario: Small-map operations are allocation-bounded

- **WHEN** a map literal with at most the threshold number of keys is built, read with `Get`, extended with `Assoc`, and iterated
- **THEN** `Get` and iteration SHALL allocate nothing and `Assoc` SHALL allocate only the new map's storage

#### Scenario: Numeric keys never format

- **WHEN** `Get`, `Set`, `Assoc`, or `Dissoc` runs with an `Int` or `Float` key
- **THEN** the operation SHALL NOT allocate a formatted string representation of the key

#### Scenario: Promotion is invisible

- **WHEN** a map grows past the small-map threshold via `Assoc` and later shrinks via `Dissoc`
- **THEN** equality with a same-pairs map, iteration determinism, and immutability SHALL hold identically before and after promotion

#### Scenario: Iteration order is deterministic

- **WHEN** the same map value is iterated or printed repeatedly, at either representation
- **THEN** the order SHALL be identical on every iteration and identical across both evaluators

#### Scenario: Extending a large map does not copy it

- **WHEN** a map above the small-map threshold is extended by `Assoc` at sizes spanning two orders of magnitude
- **THEN** the bytes and allocations a single call charges SHALL stay bounded as the map grows rather than rising in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable

#### Scenario: Repeated updates on a bulk-built map do not re-pay its conversion

- **WHEN** a map above the small-map threshold is produced by bulk construction — through `Set` or by reading a map literal — and is then updated repeatedly by `Assoc` or `Dissoc`, each update taken against that same retained map rather than against the previous result
- **THEN** the bytes and allocations charged SHALL stay bounded per update as the map grows, so that k updates against one receiver do not each charge in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable

#### Scenario: Colliding keys stay retrievable

- **WHEN** a large map holds distinct keys whose hashes agree in every bit position the structure discriminates on
- **THEN** each key SHALL resolve to its own value, `Dissoc` of one SHALL leave the others intact, and `Len` SHALL count them separately

#### Scenario: Structure shape does not vary across processes

- **WHEN** the same sequence of map operations runs in separate processes
- **THEN** the resulting map SHALL print identically and iterate identically in every run

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

Converting a builder-form map to trie form is work a value undergoes once. Reproducibility
therefore binds the conversion wherever it is performed, not every update that could have
performed it: an update that finds the conversion already done has done less work, and
charges less for exactly that reason.

#### Scenario: One value converted repeatedly charges one number

- **WHEN** a map above the small-map threshold in builder form is updated by `Assoc` or `Dissoc` repeatedly, each update taken against that same retained receiver
- **THEN** the update that performs the conversion SHALL charge one number for it, every later update SHALL charge only the path it copied, and repeating the whole sequence in a new process SHALL charge those same numbers again

#### Scenario: Two equal values convert for one number

- **WHEN** two maps above the small-map threshold in builder form hold equal contents and each is updated so that each performs its own conversion
- **THEN** both conversions SHALL charge the identical number of bytes, because the charge follows the contents and not which value was converted

#### Scenario: Equal contents charge equally regardless of build order

- **WHEN** two maps above the small-map threshold hold equal contents but were bulk-built by inserting their pairs in different orders, and each is then updated by `Assoc`
- **THEN** both updates SHALL charge the same number of bytes, because the charge follows the contents and not the construction history

#### Scenario: A reproducible charge is still an honest one

- **WHEN** the conversion allocates intermediate structure that the finished trie does not retain
- **THEN** the charge SHALL account for the storage the operation actually obtained rather than only the storage it kept, so that reproducibility is not bought by under-billing

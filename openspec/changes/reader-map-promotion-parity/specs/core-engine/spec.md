## MODIFIED Requirements

### Requirement: Metered reader storage is admitted before allocation

A runtime-owned read SHALL reserve deterministic allocation charges before
allocating token storage, decoded payloads, parser workspace, numeric-conversion
and diagnostic storage, or AST nodes and containers. Token counting SHALL reject
a token-storage plan exceeding the
remaining allocation allowance before allocating that plan. Token-count
arithmetic, payload lengths, workspace growth, and node charges SHALL reject
overflow rather than wrap.

The allocation model SHALL add 32 bytes per admitted token, including its end
marker, and 16 bytes per logical reader workspace value slot to the existing
reader node and payload model. Numeric conversion SHALL additionally reserve
`2 * tokenBytes + 256` bytes before conversion, including successful conversions;
invalid-number diagnostics SHALL render at most 128 source bytes plus a truncation
marker while retaining error kind and source position. Linked-list construction
SHALL add 32 bytes per cell. Map construction SHALL add the existing collection
header and map entry units for its allocated entry storage, on the same
deterministic growth schedule below and above the small-map threshold.
Workspace and construction-buffer growth SHALL use a deterministic
allocation schedule, charged on the same logical schedule for cold and pooled
reads. Reader output SHALL retain its existing node and payload charges; a
payload charged before decoding SHALL NOT be charged again when its AST node is
created. The whole reader result SHALL NOT receive another post-parse charge.

Successful `ReaderStats.Nodes` and `ReaderStats.Bytes` SHALL remain unchanged for
the same source. They describe output, not the added workspace, conversion,
construction, or scan-work
charges. Increasing the unconsumed suffix after a fixed resource rejection point
SHALL NOT increase the storage admitted before that rejection.

#### Scenario: A wide source is rejected before its token array exists

- **WHEN** a quoted list containing 250,000 integer elements is read with `MaxAllocationBytes` set to 1024
- **THEN** reading SHALL fail with terminal `ResourceLimitError` before allocating the full token array or constructing the full list

#### Scenario: A decoded string reserves its payload first

- **WHEN** an escaped string's decoded payload exceeds the remaining allocation allowance
- **THEN** reading SHALL reject the payload before materializing an oversized decoded buffer, and any already-accounted prefix SHALL NOT be charged twice

#### Scenario: Invalid numeric tokens cannot allocate an unbounded diagnostic

- **WHEN** a long overflowing numeric token fits the reduction allowance but its conversion-storage charge exceeds a 1 KB allocation ceiling
- **THEN** reading SHALL return terminal `ResourceLimitError` before conversion or formatting the token; with sufficient allocation, an invalid token SHALL produce a position-preserving read error with a bounded excerpt

#### Scenario: Exact admitted allocation succeeds once

- **WHEN** a valid source is read with an allocation ceiling exactly equal to its deterministic output, workspace, conversion, and construction charges and sufficient reductions
- **THEN** it SHALL succeed with that total charge, and the same read with a ceiling one byte lower SHALL fail before the disallowed allocation

#### Scenario: Successful output stats retain their meaning

- **WHEN** the same in-budget source is parsed through legacy and metered readers
- **THEN** their values and `ReaderStats` SHALL be equal even though only the metered read charges workspace and scan work to its evaluation

#### Scenario: A promoted map literal is charged on the entry-buffer schedule

- **WHEN** a map literal with more keys than the small-map threshold is read under a sufficient allocation allowance
- **THEN** the admitted construction storage SHALL be the collection header plus the map entry units for the buffer the promotion allocates, on the deterministic growth schedule, and SHALL NOT include per-trie-node units

#### Scenario: Promotion is refused before its storage exists

- **WHEN** a map literal whose promoted entry storage exceeds the remaining allocation allowance is read
- **THEN** reading SHALL fail with terminal `ResourceLimitError` before that storage is allocated

## ADDED Requirements

### Requirement: Collection representation does not depend on its builder

A collection produced by reading source SHALL use the same internal
representation as the same collection produced through the public constructors,
at every size. Reading SHALL NOT select a promotion form, a promotion threshold,
or a growth policy that the corresponding constructor would not select for the
same contents.

Reading SHALL continue to admit the storage it allocates before allocating it;
where a constructor allocates storage the reader must account for, the reader
SHALL charge that storage rather than build a different structure to make it
accountable.

#### Scenario: A read map and a constructed map agree on representation

- **WHEN** the same key-value contents are produced once by reading a map literal and once through `HashMap.Set`, at sizes below, at, and above the small-map threshold
- **THEN** both SHALL hold their entries in the same storage form, and every observable — lookup, length, iteration order, printed form, and equality — SHALL agree

#### Scenario: A read list and a constructed list agree on representation

- **WHEN** the same elements are produced once by reading a list literal and once through `NewList`, at sizes below, at, and above the flat-list threshold
- **THEN** both SHALL hold their elements in the same storage form, and every observable SHALL agree

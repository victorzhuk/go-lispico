# core-engine — delta

## ADDED Requirements

### Requirement: Reader allocation cost scales with input, not with hidden constant factors

`core.Read` and its underlying tokenizer SHALL size their working storage from
the input rather than growing it through unbounded incremental append, so that
allocation count and bytes for tokenizing and parsing a source string grow
proportionally to that string's size rather than carrying an unsized-growth
constant-factor penalty on top. Internal token representation SHALL favor
compact field widths over convenience-sized machine words where the value
range does not require them, provided reader-error position reporting loses
no precision a caller can observe today for realistic source sizes. A
zero-copy substring fast path (as already used for numbers, symbols, and
keywords) MAY be extended to other token kinds where the token's text is
never mutated after tokenization; where doing so extends how long the source
string stays reachable through a parsed value, that extension SHALL be a
documented, deliberate choice, not an incidental side effect.

`ReaderStats` (node count, deterministic byte accounting) SHALL remain
unaffected by any internal allocation-efficiency change to the reader — the
ledger this feeds (`ChargeEvalReader`) is evaluator-independent per ADR 0011,
and an allocation-shape change to the reader's own internals SHALL NOT alter
what it reports.

#### Scenario: Small-literal parsing allocates close to its content size

- **WHEN** a short source string such as `(1 2 3)` is parsed
- **THEN** the allocation and allocation-count cost SHALL NOT be dominated by unsized slice growth, and SHALL be measurably lower than an implementation that starts token storage from a zero-capacity slice

#### Scenario: A zero-copy token extension is a stated decision

- **WHEN** a token kind's text is served as a substring of the original source rather than an independent copy
- **THEN** that choice, and any resulting extension of the source string's reachability through parsed values, SHALL be documented rather than left as an unstated side effect of an allocation-efficiency change

#### Scenario: Reader stats are unaffected by internal allocation changes

- **WHEN** the reader's internal token or buffer representation changes for allocation efficiency
- **THEN** `ReaderStats.Nodes` and `ReaderStats.Bytes` for a given source SHALL be identical to their values before the change

#### Scenario: Error position precision is preserved

- **WHEN** a reader error is produced for source within any realistic file's line and column range
- **THEN** the reported line and column SHALL be exact, not truncated or wrapped by a narrower internal representation

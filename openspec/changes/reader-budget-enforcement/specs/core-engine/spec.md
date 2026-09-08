## MODIFIED Requirements

### Requirement: Structural recursion is bounded

The reader and the evaluator SHALL bound structural recursion so that no input can
exhaust the Go stack. The reader SHALL enforce a nesting-depth ceiling while
parsing lists, vectors, and maps; the evaluator SHALL enforce a structural-depth
ceiling while descending `Vector` and `HashMap` literals and expanding quasiquote.
Exceeding either ceiling SHALL return a `*core.LispicoError`, never a Go panic and
never a fatal stack overflow. The reader ceiling SHALL be fixed for each read.
Legacy reader calls without a context SHALL retain their depth-only contract;
runtime-owned reads SHALL additionally carry evaluation budgets, cancellation,
and the already-resolved deadline. The evaluator ceiling SHALL be tracked per
evaluation, not on a shared engine field, consistent with the concurrent-evaluation
contract.

#### Scenario: Deeply nested source fails closed instead of crashing

- **WHEN** source consisting of millions of unbalanced opening delimiters is read
- **THEN** `Read` SHALL return a `*core.LispicoError` reporting the depth limit, and the process SHALL NOT abort with a fatal stack overflow

#### Scenario: Deeply nested literal is bounded during evaluation

- **WHEN** a vector, map, or quasiquote literal nested past the structural-depth ceiling is evaluated
- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the depth limit, not a panic or a fatal stack overflow

#### Scenario: Structural depth does not leak across goroutines

- **WHEN** two goroutines evaluate deeply nested literals concurrently on one engine
- **THEN** each SHALL be bounded by its own per-evaluation structural-depth counter and `go test -race` SHALL report no data race

## ADDED Requirements

### Requirement: Metered reader work is interruptible before materialization

A runtime-owned read SHALL consume the current evaluation's reduction budget
while scanning and parsing. The deterministic work model SHALL charge one unit
per source byte advanced on each scanning pass, one per parsed node including
reader-generated nodes, one per copied, hashed, or compared byte, and one per
visited, linked, compared, cleared, or copied collection/workspace slot. Comment,
whitespace, number, symbol, and string scans
SHALL participate even when they produce no AST node until the scan ends.
Cancellation and the armed engine deadline SHALL be observed before work begins,
at least every 128 local work units, and before every return. Pending reduction
charges SHALL settle exactly once at each checkpoint and before returning,
without hidden checkpoint charges, a second batching layer, or resetting the
outer evaluation's counters or deadline. Numeric conversion is the sole
opaque reader phase: its token-length work SHALL be admitted and charged before
conversion, with cancellation/deadline checks immediately before and after it.
That phase SHALL be bounded by the remaining reduction allowance; its admitted
token length is the explicit exception to the 128-unit observation bound. It
SHALL NOT perform input-independent exponent expansion or unbounded retries.
Collection construction SHALL obey the checkpoint bound, including final list
linking, string copies, map hashing, key comparisons, and collision handling.

Terminal failures SHALL take precedence over a pending nonterminal read error.
A read SHALL NOT return a partial form sequence on failure. No form SHALL execute
until the complete source has been admitted and parsed successfully.

#### Scenario: A cancelled comment-only read fails before scanning

- **WHEN** a runtime-owned read receives an already-cancelled context and a long comment-only source
- **THEN** it SHALL return the cancellation error before scanning source bytes or allocating input-sized token storage, including when the source contains no executable form

#### Scenario: Long token and trivia scans consume bounded work

- **WHEN** a long comment, whitespace run, symbol, number, or string is read with a reduction budget smaller than the required scanning work
- **THEN** the read SHALL fail with terminal `ResourceLimitError` without traversing the complete source, within at most one 128-unit synchronization interval

#### Scenario: An existing deadline covers both scanning passes

- **WHEN** the evaluation deadline expires during token counting or token production
- **THEN** reading SHALL stop within the checkpoint bound and SHALL NOT install a fresh deadline for the later pass

#### Scenario: Cancellation wins over a pending syntax error

- **WHEN** cancellation is observed while settling a malformed read
- **THEN** the cancellation error SHALL be returned instead of the pending syntax error, and already-incurred charges SHALL remain accounted

#### Scenario: Numeric conversion is admitted as bounded opaque work

- **WHEN** converting a numeric token would consume more work than the remaining reduction allowance
- **THEN** reading SHALL reject it before conversion; an admitted conversion SHALL consume its token-length charge once and observe cancellation before publishing a result

#### Scenario: Checkpoints neither batch again nor charge extra work

- **WHEN** a read crosses several 128-unit work checkpoints and a final partial checkpoint
- **THEN** each checkpoint SHALL observe cancellation/deadline directly and total reductions SHALL equal only the documented work units, independently of evaluator polling state

#### Scenario: Final list construction remains interruptible

- **WHEN** cancellation or deadline expiry occurs while linking a large list after its children have been parsed and copied
- **THEN** the read SHALL stop within 128 local work units without completing the remaining chain or publishing a partial value

#### Scenario: Map construction cannot hide long key or collision work

- **WHEN** a read hashes or compares long keys, promotes a map beyond the small-map threshold, or scans colliding keys
- **THEN** construction SHALL charge those bytes and slots, admit storage before allocation, and observe cancellation/deadline within 128 local work units while preserving existing key identity and duplicate-key behavior

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
header, map entry, and trie child-slot units for its allocated node/buffer storage.
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

### Requirement: Reader reuse cannot bypass current resource policy

Every pooled read SHALL use only its own context, limits, deadline, charges, and
logical workspace growth schedule. A larger buffer retained from an earlier read
SHALL NOT permit the next read to bypass a lower limit. Returning reader scratch
SHALL clear source references and used reference-bearing slots or discard the
whole buffer without retaining it; storage whose
logical retained capacity exceeds the current read's allocation ceiling SHALL
NOT remain in the shared pool. Clearing and discarding scratch SHALL preserve
previously returned value trees and SHALL NOT reset evaluation charges.

#### Scenario: Warm buffers obey a smaller next budget

- **WHEN** a large successful read is followed by an over-budget read using the same scratch object under a smaller ceiling
- **THEN** the second read SHALL fail at the same logical admission point as a cold read and SHALL NOT retain the oversized scratch capacity afterward

#### Scenario: Failure and concurrent reuse do not leak policy

- **WHEN** failed reads and successful reads with different dialects and limits share the reader pool concurrently
- **THEN** each SHALL observe its own policy, previously returned values SHALL remain unchanged, and race checks SHALL report no shared-state race

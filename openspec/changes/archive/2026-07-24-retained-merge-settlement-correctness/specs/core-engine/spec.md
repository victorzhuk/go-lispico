# core-engine — delta

## ADDED Requirements

### Requirement: Env merge is atomic against concurrent writes

`MergeInto` and `MergeIntoCanonical` SHALL be atomic with respect to concurrent
`Set`/`SetCanonical`/`Bind`-driven writes on the target env: a write that races
the merge SHALL NOT be silently overwritten by a stale precomputed merge value.
The implementation SHALL either hold the target lock for the whole merge or
re-validate each target cell's version before committing and skip/recompute on a
version change. The documented locking guarantee and the implementation SHALL
agree.

#### Scenario: Concurrent write during merge is not lost

- **WHEN** a `Set` on a name lands concurrently with a `MergeInto` touching the same name
- **THEN** the final value SHALL reflect one of the two writes under a defined order, never a stale merge value that discards the concurrent write, and the `-race` detector SHALL report no race

### Requirement: Retained aggregate stays consistent across overwrites

An env's retained byte and slot aggregate SHALL remain equal to the sum of its
live cells' retained backing across arbitrary merge/overwrite sequences. The
`MergeInto` overwrite branch SHALL adjust the aggregate by the difference between
the new and old cell backing, not only on first insertion. This aggregate gates
`MaxRetainedBytesPerEnv`.

#### Scenario: Repeated overwrite merges do not drift the aggregate

- **WHEN** the same names are merged into an env repeatedly with changing values (iterative hot-reload)
- **THEN** the env retained aggregate SHALL equal the true sum of live cell retained bytes, with no accumulated drift

### Requirement: Multi-meter retained settlement is all-or-nothing

`settleRetained` SHALL charge meters in a deterministic order and, if any charge
fails, SHALL unwind the charges already applied in that settlement before
returning the error, leaving no charge applied without its owning cell recorded.
A partial failure SHALL NOT leave charged-but-unowned backing that a later
`Rebuild()` cannot release.

#### Scenario: Partial multi-meter charge failure rolls back

- **WHEN** a settlement spanning more than one meter fails on a later meter's charge
- **THEN** the earlier successful charges SHALL be released before returning the error, and no cell SHALL be left charged without its `retainedMeter` recorded

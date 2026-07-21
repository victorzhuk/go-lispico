# runtime-api — delta

## ADDED Requirements

### Requirement: Pinned function handles

A function handle SHALL offer a pinned variant (`(*Fn).Pin()`) that owns a
private execution machine and is documented as NOT safe for concurrent use —
one pinned handle per goroutine. A pinned call SHALL be semantically identical
to the shared handle's call: current-binding resolution, undefined-after-delete
error shape, stats attribution, callback events, deadline enforcement, and
re-entrant resource-budget sharing. A pinned call SHALL NOT borrow from or
return to any shared machine pool. Concurrent use of one pinned handle SHALL be
detected on entry and rejected with a typed error — never a panic, never
silent corruption — and SHALL leave the engine's shared state unaffected.
Pinning SHALL be per-handle: the engine and all other handles remain safe for
concurrent use.

#### Scenario: Pinned call behaves like a shared call

- **WHEN** the same function is exercised through `Fn.Call` and through a `PinnedFn` — including rebind, delete, stats, callback, and deadline cases
- **THEN** results, error shapes, stats attribution, and callback events SHALL be identical

#### Scenario: No pool traffic on the pinned path

- **WHEN** a `PinnedFn` is called repeatedly
- **THEN** no execution machine SHALL be fetched from or returned to a shared pool for those calls

#### Scenario: Concurrent misuse is rejected, not corrupted

- **WHEN** two goroutines call one `PinnedFn` concurrently
- **THEN** at least one call SHALL return a typed concurrent-use error, no call SHALL panic, `go test -race` SHALL report no data race, and subsequent single-goroutine calls SHALL behave correctly

#### Scenario: Independent pins run concurrently

- **WHEN** two `PinnedFn`s obtained from the same `Fn` are called from two goroutines
- **THEN** both SHALL execute correctly and independently

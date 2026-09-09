## Why

The code and architecture review of commit `3bcf9c1` found that a 500,003-byte
quoted list allocated about 42.6 MB before rejecting a 1 KB allocation budget.
The runtime checks reader allocation only after constructing the complete AST;
an already-cancelled comment-only input also scans to completion and succeeds.

## What Changes

- Enforce the existing allocation and reduction budgets during token counting,
  tokenization, and parsing, before input-dependent storage is allocated.
- Carry the caller's cancellation and the evaluation's absolute deadline through
  reading, including comments, whitespace, escaped strings, and failed reads.
- Account reader workspace separately from output nodes; charge each owner once,
  with deterministic costs independent of pool reuse.
- Check final collection construction in bounded batches within existing value
  representations; admit numeric-conversion storage and bound invalid-token diagnostics.
- Keep successful `ReaderStats` and unmetered reader results unchanged; provide a
  new context-aware reader entry point for runtime-owned source evaluation.
- **BREAKING:** reader workspace, conversion, construction, and scan work now
  consume existing budgets; resource rejection can precede a syntax error, and
  guarded invalid-number errors truncate long tokens. No new limit fields or defaults.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: bounded, cancellable reader work and storage; clarify the
  distinction between legacy depth-only reads and runtime metered reads.
- `runtime-api`: charge during reading, arm deadlines before reading, and avoid
  a second post-parse charge on already-accounted output.

## Impact

Affected code: `core/reader.go`, `core/dialect.go`, `core/types.go`, `core/metering.go`,
`runtime/eval.go`, and the reader call in `runtime/watch.go`. Existing
`Read`, `ReadOne`, and `Dialect.ReadWithMaxDepthStats` remain source-compatible.
The new guarded entry point uses the current evaluation ledger; core stays
standard-library-only.

Update `ARCHITECTURE.md`, `README.md`, ADR 0007 and ADR 0011 during
implementation to replace the post-parse-only safety claim and document the new
deterministic charges. Record the behavior change in `CHANGELOG.md`.

Blocked by: `metered-call-deadlines`, then `evaluation-outcome-settlement`.
Both must be implemented and archived before this change starts, so reading uses
the agreed deadline and final-outcome lifecycle. Reader work starts with a source
string; filesystem acquisition and arbitrary host callbacks are outside this
change. Testing mode: existing-service-strict.

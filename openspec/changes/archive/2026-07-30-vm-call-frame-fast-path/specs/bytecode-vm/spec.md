# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Structural-depth state hygiene

VM structural-depth accounting SHALL be restored on every exit path — normal
return, thrown error, ceiling breach, and malformed input — including when the VM
instance is reused from the pool. One failed evaluation SHALL NOT reduce the
structural-depth budget available to any later evaluation on the same Engine.

Depth counters SHALL use atomic access whenever the counter is shared with an
evaluation state or a re-entrant context; a counter private to one VM MAY be a
plain field. The choice SHALL be made from the counter's identity at arm time,
not re-derived per operation, and SHALL NOT change what a limit breach reports
or when it trips.

#### Scenario: Failed evaluation does not poison the next

- **WHEN** a VM evaluation fails for any reason and a subsequent well-formed evaluation runs on the same `WithBytecode()` Engine
- **THEN** the subsequent evaluation SHALL see the full configured structural-depth budget and succeed

#### Scenario: Pooled reuse restores depth state

- **WHEN** a pooled VM instance that previously exited through an error path is reused for a new evaluation
- **THEN** its structural-depth accounting SHALL start fresh, with no carry-over from the failed run

#### Scenario: Shared depth counters still enforce limits

- **WHEN** a host `GoFunc` re-enters the evaluator so the call-depth counter is shared across the boundary
- **THEN** combined nesting SHALL still trip the configured depth limit, and `go test -race` SHALL report no data race

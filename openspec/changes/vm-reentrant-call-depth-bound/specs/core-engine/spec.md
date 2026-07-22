# core-engine — delta

## ADDED Requirements

### Requirement: Call recursion is bounded across re-entrant apply

Both evaluators SHALL bound call-stack recursion by the shared per-evaluation
`MaxDepth` ceiling, including recursion that re-enters the evaluator through a
higher-order builtin (`map`, `filter`, `reduce`, `apply`). The call-depth
counter SHALL be shared across the re-entrant apply boundary, not reset per
pooled VM instance, so no script can drive unbounded native-stack recursion by
laundering self-recursion through a higher-order function. Exceeding the ceiling
SHALL return a `*core.LispicoError`, never a Go panic and never a fatal stack
overflow. The counter SHALL be tracked per evaluation, not on a shared engine
field, consistent with the concurrent-evaluation contract.

#### Scenario: Higher-order recursion is bounded on the bytecode VM

- **WHEN** a function recurses through `map` past `MaxDepth` on the bytecode VM
- **THEN** evaluation SHALL stop with a `*core.LispicoError` reporting the call-depth limit, and the process SHALL NOT abort with a fatal stack overflow

#### Scenario: Both evaluators bound the same recursion identically

- **WHEN** the same higher-order-recursion program runs on the tree-walker and on the bytecode VM under identical limits
- **THEN** both SHALL fail closed at the call-depth ceiling with the same error class

#### Scenario: Under-limit higher-order recursion still completes

- **WHEN** a bounded computation recurses through `map` to a depth below `MaxDepth`
- **THEN** it SHALL complete normally, with no false trip from counter state left over by a prior top-level evaluation

#### Scenario: Depth counter does not leak across goroutines

- **WHEN** two goroutines run bounded higher-order recursion concurrently on one engine
- **THEN** each SHALL be bounded by its own per-evaluation call-depth counter and `go test -race` SHALL report no data race

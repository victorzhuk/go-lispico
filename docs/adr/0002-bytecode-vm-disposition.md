---
status: superseded by ADR-0006
---

# The bytecode VM is an opt-in hot-loop optimizer, not the default

The tree-walking Evaluator stays the default and complete execution path. The bytecode VM is opt-in (`WithBytecode()`) and exists because it measurably wins on loop- and recursion-heavy code (roughly 2x faster, 10x less memory on tight arithmetic loops). It is not the default because for one-shot evaluations it is ~2x slower and allocates ~12x more, since it compiles and allocates a fresh machine per call.

## Consequences

- The on-disk bytecode cache is removed. It was never invoked from the runtime path — `WithBytecodeCache` had no effect — and the one end-to-end file-load benchmark showed the "cached" path losing to the tree-walker. Reintroduce a cache only once it is wired into evaluation and benchmarked to beat the tree-walker end-to-end.
- Forms the VM does not compile (currently `defmacro` nested in a body, `unquote-splicing`) must return a clean error and defer to the Evaluator, never panic. The two evaluators must agree on results, including the runtime type bound by `catch`.

## Considered options

- Delete the VM entirely: rejected — it earns its place on hot loops.
- Make the VM the default at full parity with a live cache: rejected for now — it fights the measured one-shot regression and requires parity work the subset does not yet have.

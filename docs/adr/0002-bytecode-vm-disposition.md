---
status: superseded by ADR-0006
---

# The bytecode VM was the non-default hot-loop optimizer

This superseded disposition kept the tree-walking Evaluator as the default and
complete execution path. The bytecode VM was selected with `WithBytecode()`
because it won on loop- and recursion-heavy code but lost one-shot evaluations
while it compiled and allocated a fresh machine per call.

## Amendment

ADR 0006 reopened the default decision. After native arithmetic/comparison
opcodes, slot-only locals, chunk caching, dialect-axis support, runtime
integration coverage, and the gold-set parity/performance gate, the evidence
supports VM promotion. The VM is now the default execution path; forms it still
cannot compile fall back to the tree-walking Evaluator form-by-form. Use
`runtime.WithTreeWalker()` to roll back to tree-walk-only execution.

## Consequences

- The on-disk bytecode cache is removed. It was never invoked from the runtime path — `WithBytecodeCache` had no effect — and the one end-to-end file-load benchmark showed the "cached" path losing to the tree-walker. Reintroduce a cache only once it is wired into evaluation and benchmarked to beat the tree-walker end-to-end.
- Forms the VM does not compile (currently a `defmacro` nested inside a larger form, and `unquote-splicing`) must return a clean error and defer to the Evaluator, never panic. The two evaluators must agree on results, including the runtime type bound by `catch`.

## Considered options

- Delete the VM entirely: rejected — it earns its place on hot loops.
- Make the VM the default at full parity with a live cache: rejected for now — it fights the measured one-shot regression and requires parity work the subset does not yet have.

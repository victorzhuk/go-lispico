---
status: accepted
---

# VM-first, staged: the bytecode VM becomes the performance path for every dialect

At the time of this decision, the performance goal was an ultra-fast VM, and the VM could not deliver it: it recompiled and allocated a fresh machine per call (13x slower than the tree-walker on repeated file loads), dispatched `+`/`-`/`<` through full GoFunc calls in its best-case hot loops, mirrored every local write into a heap-allocated Env map, and was gated to the identity dialect — so the default Common Lisp Engine and fail-closed restricted dialects could never use it. ADR 0002's re-open condition (a cache wired into evaluation that beats the tree-walker end-to-end) became the active plan. The VM would gain native arithmetic/comparison opcodes, slot-only locals with capture analysis, a compiled-chunk cache, and all three dialect axes: rename normalization to canonical kernel forms (delivering ADR 0005 consequence 4), the truthiness hook, and Lisp-2 function-cell resolution. The `IsIdentity()` bytecode gate would go away. The tree-walker would remain the fallback for any form the VM could not compile until fresh benchmarks showed the VM winning end-to-end, including one-shot evaluation; the default would flip only then.

## Amendment

The promotion evidence is now complete: native arithmetic/comparison opcodes,
slot-only locals, chunk caching, dialect-axis support, runtime integration
coverage, and the gold-set parity/performance gate. `runtime.New()` now defaults
to bytecode VM execution. Forms the VM cannot compile continue to defer to the
Evaluator per form, and `runtime.WithTreeWalker()` is the explicit rollback to
tree-walk-only execution.

## Consequences

- Staging order: opcodes and slot capture first, dialect axes second, chunk cache third, then the default-flip decision on fresh benchmarks. Each stage must keep the two evaluators agreeing on results.
- Chunks are compiled after macro expansion, so the chunk cache must invalidate on macro redefinition; a stale chunk silently running old expansions is a correctness bug, not a perf bug.
- Forms the VM cannot compile still defer cleanly to the Evaluator — ADR 0002's fallback invariant survives the disposition change. The VM now supports all three dialect axes (rename normalization 4.1, truthiness hook 4.2, Lisp-2 function cell 4.3) so non-identity dialects compile and run on the bytecode VM.
- Deep Env optimization for the tree-walker is deliberately skipped; only cheap wins (per-node context walk) are taken, since the VM is the performance path.

## Considered options

- Flip the default immediately: rejected — ships the measured one-shot regression until the cache lands.
- Optimize the tree-walker only, leave the VM manually selected as-is: rejected — caps the ceiling and drops the ultra-performance goal.
- Keep the identity-only gate: rejected — the default CL dialect and restricted policy dialects, the real consumers, would never benefit from any VM work.

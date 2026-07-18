# YAGEL VM consumer readiness — PRD

## Problem statement

YAGEL runs its shipped Rules on go-lispico's Evaluator even though the VM is intended to accelerate repeated Rule dispatch. Enabling the VM today changes observable behavior: Clojure `cond` is rejected or misread, lexical `set!` can update the wrong scope, `try` can corrupt local-slot layout, false `when` and `unless` paths can underflow the stack, keyword calls fail, malformed forms can panic the Compiler, macro redefinition can reuse stale bytecode, and VM errors can leak structural-depth state. These defects come from code inspection and temporary probes; each receives a pinned failing regression before the VM correctness floor closes.

Existing microbenchmarks do not represent YAGEL's Rule-loading and handler-application paths. They provide no release contract for correctness, latency, allocation, concurrency, or scale, and they can reward work that never improves the consumer. Shared collection defects can also dominate both execution paths; stdlib `merge` currently builds fresh maps through repeated immutable copies.

YAGEL users need Rule behavior to remain correct when the VM is enabled. Maintainers need an authoritative consumer gate that identifies the smallest required fixes, blocks known defects, and stops optimization once the consumer has a measured win.

## Solution

Establish a **VM correctness floor**, then run a YAGEL-owned **Consumer workload gate** over the shipped Rule corpus and checked-in **Scale envelopes**. Correctness comes from independent **Behavior goldens** exercised under both Evaluator and VM modes. Timed hot cells use the **Apply boundary** with deterministic fake Primitives; scheduler and bus flows remain untimed end-to-end behavior checks.

Fix the known VM parity, cleanup, cache, and no-panic defects. Add one dialect-owned **Form-shape rule** so Clojure and Common Lisp `cond` forms normalize to the same canonical clauses without changing quoted source. Correct stdlib `merge` with a fresh mutable builder and report that **Shared-path fix** separately from VM gains.

YAGEL owns the gate corpus as a **Gold set** — rule-shaped fixtures with independent golden results plus benchmark cells, exported from its shipped Rules and committed in go-lispico. A go-lispico release job runs the gold set under both execution modes and performs a **Paired release run**, self-contained — no consumer checkout. Passing the gate authorizes YAGEL to add `WithBytecode()` directly. It does not change go-lispico's global Engine default.

## User stories

1. As a YAGEL user, I want shipped Rules to produce the same specified outcomes under the VM, so that faster execution does not change agent behavior.
2. As a Rule author, I want Clojure flat `cond` clauses to work, so that `.clj` source remains compatible with YAGEL's documented Dialect.
3. As a Rule author, I want quoted `cond` data left untouched, so that Dialect normalization does not rewrite data.
4. As a Rule author, I want `set!` to mutate the existing lexical owner, so that handler state persists across invocations.
5. As a Rule author, I want `set!` on an undefined binding to fail, so that a typo cannot silently create state.
6. As a Rule author, I want false `when` and true `unless` expressions to return `nil`, so that they compose inside `let`, `do`, and function bodies.
7. As a Rule author, I want `try` and `catch` to preserve normal-body local slots, so that later bindings cannot fail with bytecode slot errors.
8. As a Rule author, I want keywords to remain callable map lookups under the VM, so that Clojure-style `(:key value)` forms retain their meaning.
9. As an embedder, I want malformed Special forms to return typed errors instead of panicking, so that untrusted source cannot terminate the host.
10. As an embedder, I want macro redefinitions to invalidate compiled chunks, so that later evaluations use the current Macro.
11. As an embedder, I want VM failures to release structural-depth state, so that one failed evaluation cannot poison a later call.
12. As an operator sharing one Engine across YAGEL Routines, I want concurrent handler execution to remain race-clean and contention-safe.
13. As a YAGEL maintainer, I want every shipped Rule covered by independent Behavior goldens, so that neither execution path is treated as the correctness oracle.
14. As a YAGEL maintainer, I want hot handlers timed at `Evaluator.Apply` with deterministic fake Primitives, so that real I/O and scheduler noise do not hide interpreter costs.
15. As a YAGEL maintainer, I want tool schemas, maps, history, and handler fan-out sampled at baseline, knee, and supported-boundary levels, so that nonlinear behavior is visible before rollout.
16. As a release manager, I want engine-sensitive cells to show a **Material VM win**, so that VM activation produces a meaningful consumer benefit.
17. As a release manager, I want data/output-dominated cells to remain non-regressing, so that shared work does not create an impossible VM target.
18. As a release manager, I want startup and Rule loading protected by mixed relative and absolute budgets, so that meaningful cold regressions fail without blocking on a sub-millisecond percentage.
19. As a release manager, I want distinct Rule closures tested concurrently on one Engine, so that VM pooling and environment locking cannot regress unnoticed.
20. As a release manager, I want Evaluator and VM samples paired in one release job, so that hosted-runner variance does not become a cross-run comparison.
21. As a go-lispico maintainer, I want YAGEL to own the live corpus, so that copied fixtures cannot drift from the consumer.
22. As a go-lispico maintainer, I want Lua and JavaScript results treated as diagnostics only, so that competitor benchmarks do not set release scope.
23. As a YAGEL operator, I want YAGEL to own its distinct load and dispatch deadlines, so that handler lifecycles are not silently capped by a second Engine timer.
24. As another embedder, I want the Engine's safe default deadline retained, so that YAGEL-specific ownership does not weaken default safety.
25. As a maintainer, I want demonstrated shared allocation pathologies fixed without counting them as VM gains, so that consumer health and VM attribution remain separate.
26. As a maintainer, I want the gate to be the optimization stop line, so that binding-cell, tagged-slot, and other deep VM redesign waits for measured demand.
27. As a YAGEL maintainer, I want one direct VM activation with an ordinary revert path, so that production does not support a permanent execution-mode flag or replay side effects in shadow mode.

## Implementation decisions

- The **VM correctness floor** blocks performance authorization. It includes every known parity, state-cleanup, cache-freshness, and malformed-input panic defect, including defects not reached by shipped Rules.
- `cond` clause shape belongs to the Dialect. Clojure accepts flat test/expression pairs; Common Lisp retains nested clauses. Evaluator and Compiler consume one shared canonical clause representation at Special-form dispatch. Reader output and quoted data are unchanged.
- Every compiled expression leaves exactly one result. Non-executed `when` and `unless` bodies produce `nil`; arity and shape are validated before the Compiler indexes operands.
- Definition and mutation have distinct bytecode semantics. A definition writes to the current scope. `set!` updates the scope that already owns the binding and errors when none exists. Local mutation continues to use the resolved local slot.
- A catch binding exists only in the handler scope. Compiling a `try` normal body does not reserve or shift the catch slot, and leaving the handler restores the previous local layout.
- VM application supports Keyword values with the same arity, map lookup, missing-key, and non-map behavior as the Evaluator.
- VM structural-depth accounting is restored on every return and error path, including pooled VM reuse.
- The macro epoch is part of the chunk-cache key: a macro definition bumps the epoch, logically invalidating every previously cached chunk. Macro definitions are rare, so epoch invalidation is preferred over dependency tracking; stale-epoch entries are physically reclaimed on the next compile miss and remain bounded by the cache size ceiling.
- stdlib `merge` builds its fresh result through the mutable construction escape hatch, preserving immutable inputs, deterministic iteration, right-most precedence, and existing errors. This is a **Shared-path fix** and does not count toward VM thresholds.
- The Engine retains its safe 30-second **Evaluation deadline**. It skips creating a redundant timer when the caller already has an earlier deadline.
- YAGEL selects `WithTimeout(0)` only after its separate Rule-load and handler-dispatch deadlines cover every evaluation path. Direct `Apply` continues to receive the caller-owned context.
- YAGEL owns the shipped Rule corpus, Behavior goldens, deterministic Primitive fakes, benchmark cell definitions, Scale envelopes, and checked-in **Hot-cell tiers**. The gate consumes them as the **Gold set** YAGEL exports into go-lispico and refreshes deliberately; ad-hoc copying outside that export is still out.
- Single-handler timing begins at `Evaluator.Apply` and ends when the Rule handler returns. Primitive call overhead remains included, while network, filesystem, process, and model work use deterministic fakes.
- Scheduler and bus flows are correctness evidence only. They validate registration, state, publication, acknowledgement, failure isolation, and deadlines without contributing timing thresholds.
- ADR 0008 is the single owner of the numeric thresholds. Engine-sensitive hot cells require its Material VM win; data/output-dominated, concurrent, and startup/Rule-load cells hold its non-regression budgets. Tier classification and baseline profiles are committed before candidate results.
- Concurrent cells use distinct Rule closures and environments on one shared Engine. Timed runs never execute under the race detector; a separate untimed run must be race-clean.
- Inconclusive benchstat results follow ADR 0008's burden-of-proof rule: one rerun at doubled benchtime, then improvement cells fail and non-regression cells pass.
- Each scaled dimension has three levels: shipped baseline, an operational knee, and the supported boundary. A CI-safe proxy is permitted only when a separate load test covers the real boundary.
- The authoritative **Paired release run** interleaves Evaluator and VM variants in one hosted job with fixed concurrency and benchtime, at least ten samples, and benchstat confidence. Pull requests run correctness and race checks without percentage gates.
- go-lispico release CI runs the committed **Gold set** under both execution modes, self-contained in the candidate checkout. No consumer checkout, no revision pin, no cross-repo secret; YAGEL exports and refreshes the corpus deliberately (ADR 0008).
- Passing the complete gate authorizes YAGEL to add `WithBytecode()` directly. There is no user-facing execution flag and no shadow run. Rollback is a normal code or dependency revert.
- The improvement gate is one-shot. After authorization, later releases keep the paired run but compare the candidate VM against the previous release's VM baseline as a non-regression check; an Evaluator improvement cannot fail the gate.
- Passing authorizes YAGEL only. The Evaluator remains go-lispico's complete global default until a separate dialect-wide gate supplies evidence for every supported Dialect.
- Passing ends this VM-specific optimization effort. Further work starts only from a failing gate cell or another measured consumer need.

## Testing decisions

- **Flow: TDD in vertical slices.** This is existing-library and existing-consumer work. Each behavior change starts with one failing regression or Behavior golden, receives the smallest implementation needed to pass, then refactors while green. Benchmark cells, Scale envelopes, and Hot-cell tiers are established before performance changes.
- **Primary go-lispico seam: public Engine behavior.** Tests construct an Engine in Evaluator or VM mode and exercise `Eval`, `EvalWithBindings`, or `Call`, asserting expected Values and typed errors from independent literals or worked examples. VM-versus-Evaluator equality is supplemental, not the oracle.
- **Primary consumer seam: YAGEL Rule application.** Tests load the live shipped corpus under selectable test-only execution modes, capture the registered handlers, and validate or time them at the **Apply boundary** with deterministic Primitive fakes.
- Existing runtime bytecode, dialect, cache, resource-limit, and concurrent-Apply suites provide prior art for Engine regressions. Existing VM cross-validation and opcode suites remain focused supplements when a public test cannot localize a stack or instruction invariant.
- Existing YAGEL Core tests provide prior art for Boot, Rule loading, advisor, agent-loop, tool registration, compatibility, interruption, and spawned-role behavior. Their expected consumer outcomes become or inform Behavior goldens rather than snapshots of Evaluator output.
- Correctness cycles cover: both `cond` clause shapes and quoted data; lexical and undefined `set!`; catch normal/error paths with later locals; false `when`/`unless` in value positions; keyword hit/miss/wrong arity/non-map; malformed arity and shape for every compiled Special form; sequential macro redefinition; structural-depth reuse after errors; pooled sequential isolation; and shared-Engine concurrency under the race detector.
- The stdlib `merge` cycle first pins zero-map behavior, right-most precedence, immutable inputs, deterministic output, and type errors. A benchmark over increasing map sizes demonstrates that bytes and allocation growth are no longer quadratic.
- YAGEL Behavior goldens cover every shipped Rule's externally visible result or invariant. Scheduler/bus tests remain end-to-end and untimed; Apply benchmarks consume the same prepared handlers and fixture builders.
- Performance evidence records `ns/op`, `B/op`, and `allocs/op` for every cell. Concurrent cells also record throughput. Baseline profiles justify Hot-cell tier classification before candidate comparisons.
- Verification runs targeted regressions first, then both repositories' full race-enabled suites. Release evidence includes the paired benchmark output and benchstat report.

## Out of scope

- Changing the global Engine default from Evaluator to VM.
- Using Lua, GopherLua, goja, or headline benchmark parity as a release gate.
- Copying YAGEL Rule fixtures into go-lispico or moving the gate back to the benchmark lab.
- A user-facing YAGEL execution-mode flag or side-effecting shadow execution.
- Build-time Rule bytecode, a serialized bytecode artifact format, or a `Prepare(source) -> Program` API.
- Resolved binding cells, tagged or unboxed VM slots, a second VM implementation, or other representation redesign.
- Batched VM cancellation polling or a new cross-engine execution-step budget unless a gate cell later proves both necessary.
- Broad stdlib, collection, string, or map cleanup beyond profile-proven **Shared-path fixes**.
- Real network, filesystem, process, or model latency in the performance gate.
- Exhaustive powers-of-two load curves in release CI.
- Performance percentage gates on ordinary pull requests.
- Reworking YAGEL scheduler or bus architecture.

## Further notes

- The YAGEL-exported **Gold set** corpus does not exist yet; the committed fixtures are examples that keep the harness executable. YAGEL must export its shipped Rules as gold-set fixtures with goldens and benchmark cells (ADR 0008).
- Exact baseline, knee, boundary, and CI-proxy values remain to be derived from the blessed YAGEL contracts for tool schemas, maps/catalogs, history, Rule count, and handler fan-out.
- Hot-cell tier assignments remain open until baseline profiles are recorded and checked in. Classification must precede candidate results.
- The fixed `GOMAXPROCS`, benchmark benchtime, interleaving order, and release workflow trigger remain operational gaps.
- YAGEL must demonstrate complete coverage of its Rule-load and handler-dispatch deadlines before selecting `WithTimeout(0)`.
- The exact Behavior golden inventory still needs to be mapped from YAGEL's current specifications to the shipped Rules and their scaled cases; the Rule count is YAGEL's to state.
- The supported-boundary cases that require separate load tests rather than release-CI execution are not yet identified.
- Temporary probes indicate that the known `cond`, catch-slot, and lexical-mutation fixes are sufficient for the current YAGEL suite to run under the VM, but they do not replace the broader correctness floor.
- Temporary paired measurements showed material VM improvements on tool-registration handlers and a sub-millisecond cold-load penalty. They are diagnostic only; the checked-in harness and Paired release run remain authoritative.

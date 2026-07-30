# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Gate corpus dialect and recursion coverage

The gold-set corpus is scoped, by decision, to `clojure.Dialect()` and to
non-recursive shapes — bounded iteration, closure state, dispatch, error
handling, keyword lookup, macro expansion, collection folds, and startup. It
SHALL NOT be read as covering the shipped engine's default Common Lisp
(Lisp-2) configuration, deep call-stack recursion, or the `Engine.Call`
boundary. Each excluded path's regression protection SHALL be named here
rather than left implied:

- **CL/Lisp-2 dialect behavior** — the dialect test suites (`cl/`, `clojure/`),
  per ADR 0013's standing consequence that "the gold set runs the Clojure
  dialect, so dialect-specific default behavior (Lisp-2 function cells, CL
  list bindings) is covered by the dialect test suites rather than the gate."
  That is correctness coverage; no gate cell times the Lisp-2 path.
  Per-change timing evidence comes from `runtime.BenchmarkEngine_FibonacciCL`,
  which is not a gate cell and SHALL NOT be cited as one.
- **Deep call-stack recursion** — the recursion correctness tests
  (`TestEval_TCO_DeepRecursion`, `TestDeepRecursion_ManyFrames`), plus
  `TestVM_CancelObservedWithinOneCall_DeepRecursion`, which additionally
  asserts a one-second wall-clock budget on cancellation responsiveness — a
  liveness bound, not a performance measurement — plus per-change benchstat
  evidence recorded in the change that claims it. No gate cell measures
  recursion depth or recursion cost.
- **The `Engine.Call` boundary** — no gate cell covers it, and no change
  currently owns adding one. The standing prohibition therefore remains in
  force: no harness-facing document quotes a `Call` figure as a settled bar.

A benchmark added specifically to cover a gap in this corpus SHALL live where
the gate actually runs — a benchmark that exists only in a package the release
workflow does not execute SHALL NOT be cited as closing that gap. Such a
benchmark MAY be kept for per-change evidence, provided its own documentation
states that it is not a gate cell.

Widening the corpus SHALL NOT be done without a committed baseline profile
justifying each new cell's tier, per ADR 0008 and the reclassification rule
`internal/perfgate/tiers.json` states for itself. A tier assigned from a
developer box does not satisfy this: the gate's first hosted run found eight
of twenty-six committed cells misclassified, five of them close to inverted.

#### Scenario: A dialect-coverage benchmark is not cited as gate coverage

- **WHEN** a benchmark exists in a package the release workflow does not execute, such as `runtime.BenchmarkEngine_FibonacciCL`
- **THEN** it SHALL NOT be cited as closing a gold-set corpus gap, and its own documentation SHALL state that it is not a gate cell; it MAY still serve as per-change evidence

#### Scenario: An excluded path is a stated decision, not a silent gap

- **WHEN** the gold-set corpus does not cover a dialect configuration or an execution shape, such as Lisp-2, deep recursion, or the `Engine.Call` boundary
- **THEN** this specification SHALL name the exclusion explicitly and state where that path's regression protection comes from instead, or state that none exists and what prohibition follows from that

#### Scenario: A new gate cell requires a hosted baseline profile

- **WHEN** a fixture is proposed for addition to the gold-set corpus
- **THEN** its tier SHALL NOT be committed to `internal/perfgate/tiers.json` until a hosted run at the gate's fixed parameters has produced the profile justifying it

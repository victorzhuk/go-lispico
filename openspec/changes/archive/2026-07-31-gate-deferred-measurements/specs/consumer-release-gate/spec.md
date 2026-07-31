# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Attributing a commit needs a two-ref run

A performance claim about a specific commit SHALL NOT be settled by a run that
measures only the tree containing it. When a commit is already an ancestor of
every candidate — no build tag, no environment knob, no way to disable it — a
paired Evaluator/VM run yields a mode ratio, not an attribution, and SHALL NOT
be recorded as confirming or refuting that commit's claim.

Attribution SHALL come from a run that measures both `<ref>^` and `<ref>` at
the gate's committed fixed parameters and compares them with benchstat, and the
record SHALL name both refs. A null result SHALL be recorded as the verdict,
not treated as a run that has yet to happen.

Where no such run is scheduled, the change carrying the claim SHALL either name
the future run that will settle it or record the claim as declined, per the
existing rule that "deferred to the release runner" means deferred to an
identified run rather than deferred indefinitely.

#### Scenario: A single-tree run does not attribute a commit

- **WHEN** a commit under test is an ancestor of the measured tree with no way to disable it
- **THEN** that run's figures SHALL NOT be recorded as the commit's verdict, and the obligation SHALL remain open with a named owner

#### Scenario: A null attribution is a verdict

- **WHEN** a two-ref run shows no significant difference between `<ref>^` and `<ref>`
- **THEN** that result SHALL be recorded as the commit's verdict, naming both refs, rather than left open pending a better run

## MODIFIED Requirements

### Requirement: Gate corpus dialect and recursion coverage

The gold-set corpus is scoped, by decision, to `clojure.Dialect()` and to
non-recursive shapes — bounded iteration, closure state, dispatch, error
handling, keyword lookup, macro expansion, collection folds, and startup. It
SHALL NOT be read as covering the shipped engine's default Common Lisp
(Lisp-2) configuration or deep call-stack recursion. Each excluded path's
regression protection SHALL be named here rather than left implied:

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

The `Engine.Call` boundary is covered by the gate cell
`GoldsetCall/call-boundary` (`internal/goldset`, `BenchmarkGoldsetCall`),
which calls a GoFunc-free callee once per timed iteration under both
execution modes. The standing prohibition on quoting a `Call` figure is
lifted, and lifted only this far: a quoted figure SHALL come from a hosted
run of that cell, SHALL name the run, and SHALL carry the qualifiers the
measurement actually has — the runner, the engine configuration the gold set
builds, and the fact that the cell hoists the caller's variadic argument
slice out of the timed loop and therefore excludes it. A developer-box
measurement SHALL NOT be quoted as a settled bar: the boundary's recorded
dev-box spread (137.0-137.4 ns against 119.7-122.8 ns at one HEAD on one day)
is wider than the margins such a bar asserts, and the cell itself reads
89.57 ns on a developer box against 188.50 ns hosted.

An absolute nanosecond target for this boundary SHALL name the machine, the
engine configuration, and what its timed region includes; a target that names
none of them SHALL NOT be recorded as met or missed, because no measurement
can settle it. Where such a target already exists, it SHALL be restated
against what the gate measures or retired with its reasoning, rather than
carried forward unmet.

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

- **WHEN** the gold-set corpus does not cover a dialect configuration or an execution shape, such as Lisp-2 or deep recursion
- **THEN** this specification SHALL name the exclusion explicitly and state where that path's regression protection comes from instead, or state that none exists and what prohibition follows from that

#### Scenario: A new gate cell requires a hosted baseline profile

- **WHEN** a fixture is proposed for addition to the gold-set corpus
- **THEN** its tier SHALL NOT be committed to `internal/perfgate/tiers.json` until a hosted run at the gate's fixed parameters has produced the profile justifying it

#### Scenario: The Call bar is adjudicated against a hosted figure

- **WHEN** an absolute `Engine.Call` target is claimed met or missed
- **THEN** the figure SHALL come from the gate's own `Call` cell on a hosted run; a miss SHALL be recorded as a finding about the target rather than as a regression, since the boundary cut's relative deltas are independently confirmed

#### Scenario: An absolute target that names no machine is not adjudicable

- **WHEN** an absolute latency target for a boundary names neither the machine, the engine configuration, nor what its timed region includes
- **THEN** it SHALL be restated against what the gate measures or retired with its reasoning, and SHALL NOT be recorded as met or missed against a figure measured under different conditions

#### Scenario: A quoted Call figure carries its qualifiers

- **WHEN** a document quotes the `Engine.Call` boundary's cost
- **THEN** it SHALL name the hosted run the figure came from and state that the figure excludes the caller's variadic argument slice, which the cell hoists out of its timed loop

# go-lispico

Ubiquitous language for go-lispico, an embeddable Lisp interpreter for Go. This
glossary fixes the meaning of terms that were used ambiguously across the code
and docs, so that "evaluator", "plugin", and "sandbox" mean one thing.

## Language

### Execution

**Evaluator**:
The tree-walking interpreter in `core/eval.go` — the default and complete execution path for all special forms.
_Avoid_: interpreter, VM, runtime

**VM**:
An opt-in stack machine in `core/vm` that runs bytecode for a subset of forms to speed up hot loops; not the default and not at full parity.
_Avoid_: evaluator, interpreter

**Compiler**:
Translates the AST into bytecode for the VM (`core/compiler`); unrelated to native-code compilation.
_Avoid_: transpiler

**Consumer workload gate**:
A per-cell release criterion over YAGEL's shipped rules across each **Scale envelope** that authorizes YAGEL's explicit VM opt-in, not a global Engine default flip; Lua and JavaScript engines remain reference points only.
_Avoid_: global VM gate, dialect-wide gate, aggregate benchmark score, competitor parity, benchmark leaderboard

**Scale envelope**:
Three checked-in workload levels for one YAGEL data dimension: shipped baseline, an operational knee, and the supported boundary or a CI-safe proxy backed by a separate boundary load test.
_Avoid_: arbitrary input size, exhaustive curve, average workload

**Material VM win**:
An engine-sensitive hot-handler cell meeting the latency and allocated-bytes improvement thresholds owned by ADR 0008, with allocation count non-increasing; startup and Rule-load cells pass under its mixed relative/absolute budgets. The numbers live only in the ADR.
_Avoid_: relative-only cold gate, statistically significant win, marginal win, noise-level win

**Behavior golden**:
An expected consumer-visible result or invariant derived from YAGEL's specifications and checked independently against both Evaluator and VM runs.
_Avoid_: Evaluator oracle, VM parity snapshot

**Apply boundary**:
The timed consumer seam from `Evaluator.Apply` through one Rule handler, using deterministic fake Primitives while retaining their GoFunc call overhead.
_Avoid_: scheduler benchmark, real-I/O benchmark, primitive-free microbenchmark

**Contention non-regression**:
Distinct Rule handlers sharing one Engine hold ADR 0008's throughput budget without increasing bytes or allocation count, and a separate untimed run is race-clean.
_Avoid_: same-handler race, scheduler throughput, timing under the race detector

**VM correctness floor**:
All known VM parity, state-cleanup, cache-freshness, and no-panic defects are fixed before any consumer performance result can authorize opt-in.
_Avoid_: corpus-only correctness, known-defect waiver

**Hot-cell tier**:
A pre-candidate classification: engine-sensitive cells require a Material VM win, while profile-proven data/output-dominated cells hold ADR 0008's latency budget without increasing bytes or allocation count.
_Avoid_: post-result classification, every-cell speedup, shared-work speedup

**Shared-path fix**:
A profile-proven asymptotic or allocation correction in code used by both execution paths, measured and reported separately from VM wins.
_Avoid_: VM optimization, broad cleanup, benchmark credit

**Paired release run**:
An interleaved Evaluator-versus-VM benchmark run for one candidate in one hosted CI job, using fixed concurrency, benchtime, at least ten samples, and benchstat confidence.
_Avoid_: cross-run comparison, PR performance gate, manual benchmark sign-off

**Special form**:
A form the Evaluator handles directly without pre-evaluating its arguments, e.g. `if`, `let`, `quote`.
_Avoid_: builtin, keyword, macro

**Builtin**:
A Go function (`GoFunc`) callable from Lisp whose arguments are evaluated before the call; supplied by plugins, never core.
_Avoid_: special form, primitive, native function

**Macro**:
A form that rewrites its unevaluated arguments into new code at expansion time, before evaluation.
_Avoid_: special form, function

**Literal**:
A vector `[...]` or map `{...}` written in source; its elements are evaluated when the literal is evaluated.
_Avoid_: constant, data literal

**Equality**:
`=` is structural equality via `Equals`, strict across types — `(= 1 1.0)` is false. Ordering (`<`, `>`, `<=`, `>=`) is numeric-only, variadic monotonic, mixing int and float by the same promotion arithmetic uses. A numeric `==` does not exist until something needs it.
_Avoid_: numeric equality (as a meaning for `=`), identity

### Dialect

**Dialect**:
A named language configuration an Engine runs, fixed at Engine construction — a Delta over a declared base, plus Semantic axes, Form-shape rules, reader feature flags, and a vocabulary name map. Common Lisp flavor is the default; Clojure-style and restricted rule subsets (yagel) are alternative dialects.
_Avoid_: profile, subset, flavor, language mode

**Semantic axis**:
A kernel evaluation rule a Dialect may set. v1 axes: symbol namespaces (Lisp-1 vs Lisp-2) and truthiness (nil-only vs nil+false falsy). Data representation (immutable List/Vector/HashMap, no cons cells) and data immutability are fixed, not axes.
_Avoid_: feature flag, mode, option

**Form-shape rule**:
A Dialect-owned normalizer for one Special form's argument structure, shared by Evaluator and Compiler at dispatch without changing quoted source; `cond` clause shape is the first.
_Avoid_: reader rewrite, compiler-only syntax, evaluator-only syntax

**Kernel table**:
The canonical set of special forms the Evaluator implements under neutral names; every Dialect is expressed against it.
_Avoid_: specialForms map, syntax table, builtin set

**Delta**:
A Dialect's changes against its base — renames, additions, removals — covering special forms and vocabulary alike. The base is either the full Kernel table (language dialects) or empty (fail-closed restricted dialects).
_Avoid_: patch, overlay, override

### Embedding

**Engine**:
The public embedding handle in `runtime` that owns an environment, loads plugins, and evaluates source.
_Avoid_: interpreter, VM, context

**Plugin**:
A bundle of builtins registered into the environment under a namespace; the namespace is chosen independently of the plugin's `Name()`.
_Avoid_: module, extension, package

**Namespace**:
The prefix on a builtin's name, e.g. `http/`, `json/`; groups a plugin's functions and need not equal the plugin `Name()`.
_Avoid_: package, scope

**Sandbox**:
A real security boundary in the `lio` plugin that confines file access to a root, resolving symlinks before the check.
_Avoid_: path guard, jail

**Trust domain**:
One Engine is the unit of trust isolation. Code from different trust levels runs on separate Engines; a fresh Engine is cheap (~124B, ~123µs boot with stdlib). Persistent top-level `def`/`set!` across `Eval` calls is intended REPL state within a single Engine, not a cross-call isolation break — so an Engine must not be shared across a trust boundary.
_Avoid_: session, tenant, per-Eval isolation

**Resource limits**:
Per-Engine, construction-time ceilings that keep adversarial or accidental input from exhausting the host — reader nesting depth, evaluator structural depth (Vector/HashMap/quasiquote descent), collection length (`range`), and chunk-cache size. Fail-closed with a clean error, never a fatal stack overflow. Distinct from `MaxDepth`, which bounds function-call/macro depth per evaluation.
_Avoid_: quota, timeout (that is `WithTimeout`), sandbox

**Evaluation deadline**:
The cooperative wall-clock limit carried by context; Engine supplies a safe default unless an embedder fully owns every evaluation lifecycle and explicitly disables it with `WithTimeout(0)`.
_Avoid_: resource limit, instruction budget

## Relationships

- An **Engine** runs exactly one **Dialect**, fixed at construction; a **Dialect** is a **Delta** over the **Kernel table** (or an empty base) plus **Semantic axes**, **Form-shape rules**, reader flags, and a name map over shared **Builtins**.
- An **Engine** loads **Plugins**; each **Plugin** registers **Builtins** under a **Namespace**.
- YAGEL owns distinct load and dispatch **Evaluation deadlines** and uses `WithTimeout(0)` only after every path is covered; other embedders retain the Engine's safe default. See `docs/adr/0010-embedder-owned-evaluation-deadlines.md`.
- The **Evaluator** handles **Special forms** and expands **Macros** directly, and calls **Builtins**.
- The **Compiler** turns forms into bytecode; the **VM** runs it for a subset and otherwise defers to the **Evaluator**.
- The **Consumer workload gate** decides whether YAGEL directly enables the **VM** with `WithBytecode()`; no user-facing mode flag or shadow execution is added, and a normal code/dependency revert is the rollback. It does not authorize a global Engine default change. See `docs/adr/0008-consumer-performance-gate.md`.
- go-lispico owns the gate corpus as a **Gold set** — rule-shaped fixtures with hand-derived golden results plus benchmark cells, committed in `internal/goldset` and modeled on embedder rule workloads; the release gate runs it self-contained, with no consumer checkout or revision pin.
- The improvement thresholds apply once. After the first authorization, later releases compare the candidate VM against the previous release's VM baseline as a non-regression check, so an Evaluator improvement cannot fail the gate.
- The **VM correctness floor** precedes the **Consumer workload gate**; every gate cell then satisfies a **Behavior golden** under both execution paths before timing is considered. Scheduler and bus flows remain end-to-end correctness checks.
- Timed single-handler cells use the **Apply boundary**; their checked-in **Hot-cell tier** decides between a **Material VM win** and non-regression. Concurrent cells require **Contention non-regression** across distinct Rule environments.
- Passing the complete gate is the optimization stop line until a measured consumer regression justifies deeper VM representation work.
- Competitor microbenchmarks and profiles diagnose costs but do not decide the release or justify speculative binding-cell/tagged-slot redesign.
- A demonstrated consumer-envelope pathology may justify a **Shared-path fix**, but it receives no credit toward a **Material VM win**.
- Each YAGEL data dimension in the **Consumer workload gate** uses a **Scale envelope**; release performance evidence comes from a **Paired release run**, while ordinary pull requests enforce correctness and race safety.

## Example dialogue

> **Dev:** "Is `+` a special form?"
> **Designer:** "No — `+` is a **Builtin** from the stdlib **Plugin**; its arguments are evaluated first. `if` is a **Special form**: the **Evaluator** decides which branch to run. `defmacro` defines a **Macro**, which rewrites code before evaluation."
> **Dev:** "When I turn on the **VM**, does it run everything?"
> **Designer:** "It runs a subset, but it now covers all dialects and caches compiled chunks per `Engine`. Anything it still can't compile defers to the **Evaluator** — the VM is an optimization for hot loops and repeated loads, not a replacement, and it stays opt-in via `WithBytecode()`."

## Flagged ambiguities

- "evaluator" was used for both the tree-walker and the VM — resolved: the **Evaluator** is the tree-walker (default, complete); the **VM** is an opt-in subset executor. See `docs/adr/0002-bytecode-vm-disposition.md`.
- "immutable" was used unqualified for Values — resolved: immutability holds at the `Equals` level; `HashMap.Set` is an internal-only construction escape hatch, not public mutation.
- "thread-safe" was read as concurrent evaluation on one **Engine** — resolved: that is the contract; per-evaluation depth and `recur` state must not be shared engine fields. See `docs/adr/0003-concurrency-model.md`.
- "sandbox" was left undefined between a convenience guard and a boundary — resolved: it is a real security **boundary**.
- **Literal** element evaluation was inconsistent (quasiquote evaluated inside vectors, plain literals did not) — resolved: literal elements are evaluated. See `docs/adr/0001-literal-evaluation-semantics.md`.
- "performance goal" was used to mean both competitor parity and consumer readiness — resolved: the **Consumer workload gate** governs releases; Lua/goja parity is diagnostic only.
- "representative workload" could mean an invented routing rule or arbitrary private scripts — resolved: use YAGEL's shipped rules plus parameterized envelopes for tool schemas, maps, history, and handler fan-out.
- An aggregate benchmark could hide a severe regression in one consumer path — resolved: the **Consumer workload gate** is Pareto-style per cell, not a weighted total.
- "material win" was undefined — resolved: a **Material VM win** per the thresholds owned by `docs/adr/0008-consumer-performance-gate.md`; cold cells use its mixed relative/absolute budgets. The numbers are stated once, in the ADR.
- A relative-only cold gate treated a measured 0.6 ms Rule-load cost as a release blocker — resolved: startup and Rule load use mixed relative/absolute practical-equivalence budgets.
- Host and scheduler work could hide interpreter costs in hot benchmarks — resolved: time the **Apply boundary** with deterministic fake Primitives; cover scheduler/bus behavior with untimed **Behavior goldens**.
- Concurrency evidence could mean racing one mutable handler or timing the scheduler — resolved: benchmark distinct Rule closures sharing one Engine and require **Contention non-regression** plus race safety.
- It was unclear whether passing the consumer gate merely started deeper VM work — resolved: it is the stop line; binding-cell and tagged-slot redesign waits for a failing gate cell or another measured consumer need.
- A passing shipped corpus could have authorized VM use despite known defects outside that corpus — resolved: every known parity, stale-state/cache, and malformed-input panic bug belongs to the **VM correctness floor** and blocks opt-in.
- YAGEL had both its lifecycle deadlines and the Engine's default load timer — resolved: once ADR 0042 covers every path, YAGEL owns those **Evaluation deadlines** and disables the redundant Engine timer; the safe default remains for other embedders.
- Requiring the same percentage win from output-heavy and map-dominated Rules would measure shared work, not execution — resolved: each **Hot-cell tier** is profile-backed and checked in before candidate results; all cells remain no-regression gates.
- VM-only scope would leave measured shared-path pathologies intact — resolved: include profile-proven **Shared-path fixes**, report them separately, and avoid a broad cleanup pass.
- Cross-run hosted CI variance could make a 5% band flaky — resolved: use a same-job interleaved **Paired release run** with at least ten samples; PR CI remains correctness/race-only.
- Scale inputs could be either toy baselines or exhaustive curves — resolved: each **Scale envelope** uses baseline, knee, and supported boundary, with a CI proxy only when a separate load test covers the real boundary.
- A VM rollout flag or shadow mode would create two production paths or replay side effects — resolved: YAGEL enables `WithBytecode()` directly after the gate; rollback is a normal revert.
- Batched VM cancellation checks implied a new cross-engine instruction-budget contract — resolved: retain per-instruction polling and existing **Evaluation deadlines** unless a failing gate cell justifies both changes together.
- Gate ownership was ambiguous between go-lispico, YAGEL, and the benchmark lab — resolved: go-lispico owns the **Gold set** and its tier assignments, independent of any consumer; release CI runs the committed gold set against the candidate.
- The YAGEL gate was at risk of being treated as proof for every Dialect — resolved: passing it authorizes only YAGEL's explicit VM opt-in; a global default flip requires a separate dialect-wide gate.
- `cond` was described as Clojure-compatible while the Kernel parsed Common Lisp-style nested clauses — resolved: one dialect-owned **Form-shape rule** canonicalizes Clojure flat pairs or Common Lisp nested clauses for both Evaluator and Compiler without rewriting quoted data. See `docs/adr/0009-cond-clause-shape-is-dialect-owned.md`.
- "bytecode-correct" could mean only matching the Evaluator — resolved: each gate cell has an independent **Behavior golden**, and both paths must satisfy it before comparison.
- The gate could read as either one-shot authorization or a standing improvement bar against the same-release Evaluator that a faster Evaluator would fail — resolved: improvement thresholds apply once; later releases compare VM against the previous VM baseline as non-regression.
- Inconclusive benchstat on hosted runners had no defined outcome — resolved: one rerun at doubled benchtime, then improvement cells fail and non-regression cells pass (burden of proof lies with the claim).
- A pinned or checked-out consumer could couple the public release job to the private YAGEL repo — resolved: the gate runs the repo-owned **Gold set** instead, self-contained and always runnable; no cross-repo secret and no private output in public logs. Representativeness is maintained by evolving the corpus against measured consumer needs.
- Race-detector cleanliness could be read as part of the timed run — resolved: race runs are separate and untimed; no timing threshold is evaluated under the race detector.

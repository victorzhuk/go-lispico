## Why

A pre-release audit of `643def566981f573aced2b3ed34a4dbbc49d1d87` returned `BLOCKED`. The local CI floor was green (build, vet, full tests, race, lint, gold-set, CLI smoke), but four consumer-visible defects were reproduced at runtime, and static review, spec reconciliation, and release-gate inspection each produced findings that block a version cut.

Reproduced defects:

- A top-level `def` following a nested binding form compiles on the bytecode path but is refused by the tree-walker, because every nested-scope boundary in `core/compiler/compiler.go` writes `false` on exit instead of restoring the enclosing value. The two evaluators then disagree about whether `(defn f [] (let [x 1] x) (def leaked 1))` defines `leaked`.
- `MacroExpandDeep` passes the binding form's head to `NormalizeBindings`, so a macro used inside `let`, `let*`, or `loop` bindings fails to expand and the two evaluators return different values for `(defmacro m [x] \`(* 2 ~x))` followed by `(let [y (m 3)] y)`.
- Plugin binding ownership comes from a whole-environment before/after snapshot diff, so a host binding written into the root environment while a plugin `Init` is paused is attributed to that plugin and deleted by `UnloadPlugin`.
- `fileWatcher.Stop` joins the watcher goroutine, so a watched GoFunc that calls `Engine.Stop` or `Engine.Close` shuts the engine down from inside the goroutine it is waiting for and hangs.

Release-gate integrity: `cmd/perfgate` can exit 0 having measured nothing. The bench-metrics reader accepts input with no samples, an empty candidate map yields zero cell names, and the verdict switch reaches the pass branch because neither a failure nor a rerun was ever set. No step compares the configured tier inventory against observed candidate cells.

Documented contradictions: the `bytecode-vm` Purpose still calls the VM experimental and advertises a removed on-disk cache; `dialect` still mandates the identity dialect for a no-option engine while the shipped default is Common Lisp; the three workflow specs carry generated placeholder Purpose text; `ARCHITECTURE.md` claims literally every I/O operation lives in plugins, which `runtime/eval.go` contradicts.

Target version: `v0.16.0`, a minor bump, because the unreleased delta already carries breaking `plugins/fsm` API changes (`Reset() State` became `Reset() (State, error)`, and `TransitionResult` gained `Committed`).

## What Changes

- `bytecode-vm` gains two requirements: a scoped-definition refusal must survive an enclosing nested scope, and macro expansion must reach binding-form operands on the `MacroExpandDeep` pre-pass.
- `runtime-api` gains two requirements: plugin binding ownership is attributed through the registration journal via a new `Registration.BindingNames()`, and `Engine.Stop`/`Close` request watcher cancellation without joining the watcher goroutine. The second is an observable completion-semantics change.
- `dialect` gains a requirement scoping identity-dialect preservation to explicit selection and naming Common Lisp as the no-option default.
- `io-plugin` and `net-plugin` gain requirements for confinement enforced during the open rather than before it, and for outbound destination policy plus error redaction.
- `consumer-release-gate` gains a requirement that the gate rejects empty or cell-incomplete candidate evidence instead of exiting 0.
- `docs/adr/0004-kernel-first-mission.md` records the freeze exception: `lio`, `net`, and `exec` take security fixes, not features, in this release.
- `CHANGELOG.md` `[Unreleased]` records the fixed defects, the changed `Engine.Stop`/`Close` completion semantics, the changed plugin-ownership behavior, the spec corrections, and the gate-coverage additions, and `v0.16.0` is tagged and released.

Deviation from the default template: all six capabilities already exist under `openspec/specs/`, so every delta uses `## ADDED Requirements` for new requirements only and carries no `## MODIFIED Requirements`. The Purpose-text corrections in Step 4.1 land when the deltas are merged at archive, not as separate delta sections.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: nested-scope-restoring scoped-definition refusal; macro expansion into binding-form operands.
- `runtime-api`: journal-attributed plugin binding ownership; cancellation-only watcher shutdown.
- `dialect`: Common Lisp is the no-option default; identity preservation is scoped to explicit selection.
- `io-plugin`: containment enforced during the open, and bounded, charged reads.
- `net-plugin`: redirect and TLS policy, and redacted outbound errors.
- `consumer-release-gate`: empty or incomplete candidate evidence is a non-pass exit.

## Impact

Production code, tests, specifications, and documents. Affected code: `core/compiler/compiler.go`, `core/eval.go`, `core/registration.go`, `runtime/plugin.go`, `runtime/watch.go`, `runtime/engine.go`, `runtime/lazy_template.go`, `runtime/stats.go`, `runtime/repl.go`, `core/vm/vm.go`, `internal/perfgate/bench_metrics.go`, `cmd/perfgate/main.go`, `internal/goldset/goldset.go`, `internal/perfgate/tiers.json`, `plugins/fsm/{machine,plugin}.go`, `plugins/stdlib/collections.go`, and the frozen `plugins/{lio,net,exec}` packages.

Deliverable boundary: this change writes its requirements as deltas inside the change directory, at `specs/<capability>/spec.md` under `openspec/changes/2026-10-16-audit-remediation/`, and writes nothing under `openspec/specs/`. The canonical capability specs receive these requirements at archive, after the release floor is green.
## Context

The audit found two classes of defect. The first class is an evaluator divergence: the compiler and the tree-walker disagree about the same source, and the runtime disagrees with itself about which evaluator owns a binding. The second class is ownership and completion semantics: the runtime cannot tell which bindings a plugin created, and it cannot shut a watcher down from inside the watcher.

Two further gaps are about evidence. The release gate accepts a measurement that measured nothing. And the frozen-plugin security findings would otherwise change code in packages ADR 0004 keeps frozen without recording that fact where the freeze is declared.

## Decisions

### Scoped-definition refusal is restored, not tracked

Each nested-scope boundary in `core/compiler/compiler.go` captures the enclosing `c.inScopeDef` immediately before setting it to `true` and restores that prior value where it currently writes `false`. The four boundaries are `compileLet`, `compileLetStar`, `compileLoop`, and the `compileTry` catch handler.

A per-depth counter was rejected. The flag is a single boolean whose contract is "a lexical scope is open on this compiler", and save/restore expresses that contract exactly. The counter would encode depth in a field the guards read as a boolean.

The sub-compiler sites are deliberately untouched. `compileFn` and the Lisp-2 `compileDefn` branch set the flag on a fresh `sub` compiler whose zero value is already the correct post-body state, and the parent's flag is unaffected there. Restoring the enclosing value is what makes the three existing guard sites correct; they need no edit.

The missing coverage is the point. Every existing scoped-definition assertion places a `def` inside an open scope, which stays green under the bug. The new regression test places a top-level `def` after a nested form, for all four forms plus the `defn` and `loop`/`recur` twins, and the runtime parity test asserts `RootEnv().Get("leaked")` is absent under both evaluators.

### `NormalizeBindings` receives the operand, not the head

`expandDeepLet` and `expandDeepLoop` pass `items[1]`, matching every other call site in `core/eval.go`. `NormalizeBindings` accepts only a bindings operand, so the head-relative index guaranteed a `bindingSyntaxError` before the expansion could run, and macro expansion inside a binding form silently diverged between evaluators.

The reconstruction lines were already head-inclusive and correct. They were unreachable only because the error branch returned first, so no reconstruction logic changes.

### Plugin binding ownership comes from the registration journal

A whole-environment snapshot diff cannot distinguish a binding the plugin wrote from a binding the host wrote into the same environment concurrently. `core/registration.go` gains `(*Registration).BindingNames()`, which walks the registration's own entries under the root read lock and reports a name only when the live cell is still the one this registration created and has not moved past the recorded version.

`Names()` is not widened. Its value-cell-only contract drives vocabulary processing and is pinned by its own tests; a separate accessor preserves that contract without adding stored state. `BindingNames` never materializes a lazy binding, so asking costs no evaluation.

`runtime/plugin.go` keeps the before-snapshot purely as a compatibility exclusion set. A plugin that overwrites an already-live name does not newly acquire unload ownership, and the baseline unions both namespaces so adding a function cell for an existing value name stays excluded. Collection moves into a `defer` that runs after `lazyMaterializer.fence()`, because Go evaluates return expressions before defers and an admitted view materialization may still be finishing. The same chunk installs unconditional abort cleanup on uncommitted exit, so a host that recovers an `Init` panic does not receive an engine with incomplete registration cleanup.

### Watcher shutdown is cancellation-only

`fileWatcher.Stop` requests cancellation and never joins. The watcher goroutine owns its own exit, ticker cleanup, evaluator unwinding, and `wg.Done`. Cancellation checks go at the initial scan, each ticker-triggered scan, each `scan` iteration, each form in `reloadFile`, and immediately before `childEnv.MergeInto`.

Joining cannot work here: evaluation of a watched file can call `Engine.Stop` from the watcher goroutine itself, so a join deadlocks on the caller's own frame. This is an observable completion-semantics change, not an internal cleanup change, and the `Engine` interface comments say so. A callback that cancels and returns successfully must not execute later forms or publish child bindings, which is why the check sits immediately before the merge.

`stopWatcher` keeps its existing ordering: detach under the engine lock, release the lock, cancel watcher contexts, synchronously flush the bytecode cache, return. Cache flushing does not depend on watcher completion, because each stripe evicts under its own lock.

The regression test drives the shipped path — `Engine.Watch` through `reloadFile` to a host GoFunc — in a subprocess with its own watchdog, because the failure mode is a hang that would otherwise take the suite with it.

### The gate rejects evidence it did not collect

Before any verdict is evaluated, every cell in the tiers configuration must have candidate samples and the metrics its tier requires. Empty candidate evidence is a fail, never a pass. The first-authorization policy for genuinely new baseline cells is preserved, and the exit-code contract stays 0 pass, 1 fail, 2 rerun, 3 unconfigurable.

The gate's purpose is evidence, so the defect is the acceptance of a measurement that measured nothing, not a threshold-policy disagreement.

### Common Lisp is the no-option default, stated in the dialect capability

`openspec/specs/runtime-api/spec.md` already states the shipped default. `openspec/specs/dialect/spec.md:59-64` still mandates the identity dialect for a no-option engine and contradicts it. The delta scopes identity preservation to explicit identity or Clojure selection and names Common Lisp as the no-option default, reconciled consistently with the namespace and vocabulary requirements that carry the same assumption.

### Confinement and destination controls land in the frozen plugins, and the freeze exception is recorded

`lio` resolves symlinks up front and validates, then opens the already-resolved path separately, so containment is decided before the filesystem change that the open performs. The fix is `os.OpenRoot` plus a relative open, which enforces containment during the open, needs no dependency change because the `go.mod` floor is already `go 1.24.0`, and keeps the deny-pattern and root checks as defense in depth. Reads are bounded and charged.

`net` builds a client with no `CheckRedirect`, so a redirect chain reaches hosts the script author never named. The default is not to follow, stated as the capability's contract. `TLSClientConfig{MinVersion: tls.VersionTLS12}` is written as explicit policy and recorded as hygiene, not a fix: the client already defaults to TLS 1.2 on this toolchain, and the change does not claim a vulnerability. Wrapped `*url.Error` values render the full URL including query-string secrets, so the redaction keeps method and scheme only.

`exec` already uses `exec.CommandContext` with separate arguments and no shell, which is correct but not sufficient: an argument beginning with `-` can still be read as an option by the target. The argument shape is rejected, or an end-of-options delimiter is used where the selected target supports it.

ADR 0004 freezes these plugins at security and correctness fixes only, no new features. Every item above is a security or correctness fix, so it is inside the existing decision. The decision is still recorded where the freeze is declared, because the mission record otherwise reads as if the packages were untouched.

### `exec` takes no capability delta

Option-injection refusal is a code-level fix inside a package that already has no capability spec. Adding a requirement for it would create a capability contract the release does not need to publish.

### `v0.16.0` is a minor bump

The unreleased delta carries breaking `plugins/fsm` API changes — `Reset() State` became `Reset() (State, error)`, and `TransitionResult` gained `Committed` — plus new canonical workflow specs. Under 0.x SemVer a breaking change with additions is a minor bump. No compatibility shim is added for the already-breaking FSM API.

### The deltas land at archive, after the floor is green

Apply writes requirements into this change directory only. The canonical specs receive them at archive, which happens after the release floor is green. Writing to `openspec/specs/` during apply would leave a rejected change with requirements already in force.

### Adding gold-set cells is deferred, and the coverage guard is why

Two thresholds this repository states are configured but unexercised, and the change records that rather than leaving the claim standing.

`internal/perfgate/tiers.json` configures 12 engine-sensitive cells, 14 data-dominated cells, and 1 startup cell, and zero concurrent cells, so the concurrent tier's 5% throughput threshold named in ADR 0008 has no fixture behind it. `internal/goldset/goldset.go:37-43` pins `runtime.WithDialect(clojure.Dialect())`, while only `stdlib` and `json` ship in `cmd/lispico`, so a green gate cannot establish non-regression for the shipped default dialect, including its Lisp-2 function resolution.

The straightforward fix for both is blocked by the repository's own coverage guard. `internal/perfgate/bytes_allowance_spread_test.go:39-48` requires historical profile samples for every configured cell, including cells whose `bytesAllowanceBOp` is a stated `0`, and a stated `0` does not avoid the assertion. The pinned profile is `profile-30637802780` (`internal/perfgate/perfgate_test.go:705-706`), captured in the past, so it cannot hold a cell introduced later. Adding any new cell makes `TestBytesAllowancesAreJustifiedBySpread` fail with `cell %q has no B/op samples in %s`.

Amending that guard immediately before a release, with a hosted re-baseline behind it, is the wrong moment, so both additions are deferred rather than dropped. The follow-up amends the guard so the historical-sample requirement applies to cells carrying a nonzero `bytesAllowanceBOp`, then adds the cells and re-baselines at the next hosted gate run. No tier, threshold, allowance, or exit code changes here, and `internal/perfgate/` and `internal/goldset/` are untouched.

## Consequences

- The two evaluator paths agree on scoped definitions and on macro expansion inside binding forms, and the regression tests pin the specific source shapes the audit reproduced.
- Unload ownership follows the journal, so a concurrent host binding is no longer deleted by a plugin unload.
- `Engine.Stop` and `Engine.Close` return without joining watcher cleanup. A host that needs watcher quiescence must observe it itself; the interface comments state this.
- The gate fails closed on missing evidence, so an unconfigured or mis-instrumented cell blocks a cut instead of passing it.
- The frozen-plugin security fixes ship with the exception recorded in ADR 0004, so the mission record and the code agree.
- Consumers embedding a plugin that writes root bindings concurrently with `Use` see corrected unload behavior; the change is behavioral and visible.
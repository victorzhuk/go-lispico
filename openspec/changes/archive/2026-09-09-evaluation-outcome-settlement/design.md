## Context

See proposal.md for the reproduced failure. `Eval` and `evalWithBindingScope` record results along several return paths, while a deferred `core.FinishEval` can replace their named error afterward. Panic recovery has its own stats/callback path. Existing tests cover these behaviors separately.

## Goals / Non-Goals

**Goals:** one final observation per source-evaluation invocation, after settlement selects the returned outcome.

**Non-Goals:** transactions for ordinary evaluations, changes to `PluginCallEvent`, callback-panic policy, meter composition, or public error-message rewrites.

## Decisions

### One final observation point per public source boundary

Keep named result/error variables and explicit lifecycle ownership. Establish final observation before any early return can occur. Order completion as: recover an evaluated GoFunc panic; finish the acquired top-level evaluation lifecycle if any; apply existing terminal-error precedence and result clearing; capture elapsed duration; update stats; fire callbacks.

Remove intermediate success/error publications from parse and form loops. `EvalWithBindings` delegates to `evalWithBindingScope` and must not publish a second event. A failed `StartEval` has no acquired lease to finish, but still has a final failed source-evaluation outcome. Nested evaluators continue to honor the existing `top` ownership result from `core.StartEval`.

Retain existing source labels and error wrapping. Callback errors are not interpreted as meter failures, and this change does not define a new recovery policy for callback panics. Characterize current callback behavior before arranging deferred calls so refactoring does not expand that policy accidentally.

Duplicating settlement calls at each return is rejected: it leaves error and panic branches susceptible to double return or missing settlement. Moving only the success callback is insufficient because setup, terminal precedence, and panic branches have the same ordering requirement.

### Preserve retained-state semantics

`core.FinishEval` remains the settlement owner; core does not gain a second finalization API. The accepted charge-after-write behavior in `2026-07-22-meter-leases-and-session-ledgers/design.md` remains intact. A retained denial changes the observed outcome, not the already-written root binding or the established scope-return behavior.

### Deterministic verification

Existing-service-strict, regression-first. Extend the existing `recordingMeter` test seam to capture lease return and charge order. In callbacks, inspect its completed settlement state and the stats snapshot. Use `chargeErr` for retained rejection, a returned nonterminal GoFunc error for precedence, and the existing panic fixtures for unwind. Cover VM and tree-walker with context and engine meters.

Reuse `runtime/meter_test.go`, `runtime/stats_test.go`, and `runtime/panic_boundary_test.go`. Assertions cover cause/type rather than demanding identical wrapper pointer identity. No sleeps or implementation-mirroring tests are needed.

## Risks / Trade-offs

- Deferred ordering can publish twice during panic unwind → pin exactly-one event/count for existing recovered-GoFunc fixtures before changing control flow.
- Callback duration previously excluded settlement → document that duration now includes the work required to produce the returned outcome.
- Callbacks may reenter the engine → invoke them after settlement and without holding engine/environment locks.
- Early setup failures were previously undercounted → explicitly test and document their inclusion in final source-evaluation statistics.

## Migration Plan

No API or dependency migration. Existing consumers receive accurate failure observations; successful callbacks occur after settlement. Update metering documentation and CHANGELOG. No data rollback is involved. Merge after `metered-call-deadlines` and before `plugin-binding-rollback`; neither is a semantic prerequisite.

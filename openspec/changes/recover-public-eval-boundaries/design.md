## Context

Panic recovery is centralized at `callBoundary` (`runtime/eval.go`) for
`Engine.Call`/`Fn.Call`/`PinnedFn.Call`, and at `Engine.Eval`'s own deferred
recover. `EvalWithBindings`/`LoadScope` take a different path
(`evalWithBindingScope`) that was never given the same guard, and `Watch`'s
reload runs the evaluator on a background goroutine with no caller frame to
catch anything. `core/` has zero `recover()` by invariant, so recovery must live
in `runtime/`.

## Goals / Non-Goals

- Goal: no `GoFunc` panic escapes any public evaluation path; a background
  reload panic never crashes the host.
- Goal: recovered panics reach callers as the existing `PanicError` shape.
- Non-Goal: catching panics from host code outside evaluation (e.g. a panic in a
  `Watch` callback the embedder registered runs outside `reloadFile`).
- Non-Goal: recovering in `core/` — the guard stays in `runtime/`.

## Decisions

### Recover in `evalWithBindingScope`

Wrap the evaluate step in the same `defer func(){ if r := recover(); r != nil {
… core.NewPanicError(name, r) } }()` pattern `Engine.Eval` uses, with the same
result/err reset semantics. Both `EvalWithBindings` and `LoadScope` inherit it
since they share this function; `LoadScope` must still return its child `*Env`
on the success path and a nil scope on the recovered-panic path.

### Surface the recovered reload error instead of crashing

`reloadFile` runs with no caller to receive an error. The recovered panic SHALL
be converted to an error and delivered through the watcher's existing failure
channel — the same route a normal reload evaluation error already takes (a
reload that returns an ordinary error today does not crash the process, so the
mechanism exists). The reload loop continues watching after a recovered panic;
one bad file version does not stop the watcher. If no error sink is wired, the
error is logged via the engine's logger rather than dropped silently.

Rationale: a background goroutine cannot propagate to a caller, so the only
choices are crash (current), silent swallow (hides the bug), or surface-and-
continue. Surface-and-continue preserves the watcher's usefulness and matches
how ordinary reload errors are already handled.

## Risks / Trade-offs

- A recovered panic that leaves the pooled VM in an inconsistent state must not
  be returned to the pool dirty — reuse the reset-before-Put discipline the
  `Call` path already applies on its panic branch. Verify the watcher's VM
  acquisition path resets on the recovered branch.
- Behavior change: embedders relying (accidentally) on a panic propagating now
  get an error instead. This is the intended fix, called out as BREAKING.

## Migration

None for correct callers. Embedders that wrapped `EvalWithBindings`/`LoadScope`
in their own `recover()` can drop it; the error now arrives normally.

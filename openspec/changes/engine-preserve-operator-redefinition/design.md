## Context

The Lisp-2 bridge exists because head-position lookup resolves through the
function cell, but plugin `GoFunc`s land in the value cell; without bridging,
`(* x x)` is undefined. The bridge (`runtime/engine.go:440-457`) therefore
copies each value-cell `GoFunc` into the function cell — canonically
(`SetFuncCanonical`) for native operators so the VM's native-op fast path still
fires, plainly (`SetFunc`) otherwise. The defect is that it does this
unconditionally on every `applyVocabulary`, clobbering any function-cell
binding the user set in between.

## Why the canonical flag cannot gate the fix

The obvious guard — "only refresh canonical function cells" — fails for
non-operator builtins. `map`, `str`, `car` are bridged with `SetFunc`
(non-canonical), so a non-canonical function cell is ambiguous: it may be the
bridge's own prior write or a user `(defun map …)`. The canonical flag does not
distinguish them.

## Decision: value-identity guard

Gate the write on value identity instead:

```
existing, hasFunc := e.rootEnv.GetFunc(name)
if hasFunc && !existing.Equals(bridgedGoFunc) {
    continue // user (or a prior definition) owns this function cell
}
// absent, or already equal → (re)bridge as today
```

`GoFunc.Equals` compares by name, so re-bridging the same builtin is a
recognized no-op; a user `(defun + …)` installs a `Lambda`, which is never
`Equals` to the value-cell `GoFunc`, so it is preserved. This handles canonical
and non-canonical bridges uniformly, needs no new cell state, and reuses the
`Value.Equals` contract already relied on elsewhere.

## ReloadPlugin interaction

`ReloadPlugin` removes the plugin's bindings before re-adding them
(`runtime/plugin.go`), so a genuine reload clears both cells and the bridge
re-establishes canonically from a clean slate — correct. A user `defun` of a
binding owned by the plugin being reloaded is reset by that reload, which is the
expected semantics of reloading the owning plugin. The bug this change fixes is
narrower and distinct: an unrelated `Use()` reverting a redefinition it has
nothing to do with.

## Scope

- Applies to the Lisp-2 bridge only; Lisp-1 dialects never enter this loop.
- Does not change how a redefinition is made (`defun`/`setf`-fdefinition) — only
  stops the bridge from undoing it.
- Related latent axis (not in scope): a redefined operator's value cell stays
  canonical, so a consumer reading the value cell still sees the original. This
  change fixes head-position call semantics; value-cell provenance of a
  redefined operator is a separate question flagged for follow-up if a consumer
  needs it.

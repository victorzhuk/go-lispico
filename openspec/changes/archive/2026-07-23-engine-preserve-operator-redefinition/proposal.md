## Why

`applyVocabulary` runs on every `Use()` and `ReloadPlugin()`
(`runtime/plugin.go:144,279`). Under a Lisp-2 dialect — the default CL — its
bridge loop (`runtime/engine.go:440-457`) rewrites each name's function cell
from its value cell for every `GoFunc`. A user `(defun + (a b) …)` rebinds only
the function cell (`SetFunc`, `core/env.go:583`), leaving the value cell holding
the original canonical `GoFunc`. The next plugin load re-derives the function
cell from that stale value cell and silently overwrites the redefinition — no
error, no log. The code comment already names the defect: "a defun rebind lands
through SetFunc and loses it again."

Confirmed by reproduction on master:

```
Use(stdlib) → (defun + (a b) 999) → (+ 1 2) = 999   ; rebind holds
Use(data.New())                    → (+ 1 2) = 3     ; rebind silently reverted
```

Loading a second, unrelated plugin — the normal multi-plugin case — is enough
to trigger it, and the mechanism reverts any `GoFunc` redefined via `defun`,
not only canonical operators. It violates the documented determinism invariant
("same input + env → same output") with zero embedder-visible signal.

## What Changes

- The Lisp-2 vocabulary bridge SHALL NOT overwrite a function-cell binding the
  user has redefined. Before bridging a name from its value cell, if the
  function cell already holds a binding that is not equal (`Value.Equals`) to
  the value-cell `GoFunc` being bridged, the bridge SHALL leave it untouched —
  a prior definition owns that function binding.
- First-time bridging (function cell absent) and idempotent refresh (function
  cell already equals the derived `GoFunc`) are unchanged, so a newly loaded
  plugin's `GoFunc`s are still bridged into head position.
- No API change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Function redefinitions survive plugin
  loading`.

## Impact

- Code: `runtime/engine.go` (`applyVocabulary` bridge guard), using the existing
  `GetFunc` read and `Value.Equals`.
- Downstream: an embedder that redefines an operator or builtin under CL keeps
  the redefinition across later `Use()` calls.
- Fixes a latent determinism bug on the default dialect; no consumer code
  change.

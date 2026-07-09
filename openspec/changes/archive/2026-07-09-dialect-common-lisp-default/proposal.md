# Common Lisp dialect and default flip

## Why

The mechanism slices deliver the axes, reader flags, and vocabulary map but ship no user-facing dialect and leave the default unchanged. This slice assembles the parts into the two named dialects the project needs and makes Common Lisp the default surface — the stated goal that lispico speaks Common Lisp out of the box.

## What Changes

- A **Common Lisp dialect** is assembled: CL vocabulary (`defun`, `setq`, `progn`, `car`/`cdr`, `funcall`, `#'`, …), `nil`-only truthiness, the Lisp-2 namespace axis, and CL reader flags (`#'`/`#(...)` on, `[..]`/`{..}` off).
- A **Clojure dialect** is assembled from the current flavor — Lisp-1, `nil`+`false` truthiness, bracket literals — so nothing that works today is lost; it is now a named, explicitly selectable dialect.
- `runtime.New()` with no dialect option resolves to the Common Lisp dialect. This is the breaking flip; it is acceptable while the project is alpha.
- Existing tests, examples, and the yagel consumer are pinned to the Clojure dialect where they depend on today's surface, so the flip is contained to intent, not accident.

## Impact

- Affected specs: `dialect`, `runtime-api`.
- Affected code: the CL and Clojure dialect definitions, `runtime/engine.go` default resolution, and updates to existing tests/examples that assumed the old default.
- Breaking for embedders relying on the previous default; migration is a one-line `WithDialect(clojure.Dialect())`.
- An Engine on the CL dialect's non-default semantic axes falls back to the tree-walker; the VM is unchanged (ADR 0005).

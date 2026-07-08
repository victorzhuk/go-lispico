# Dialect namespace axis (Lisp-1/Lisp-2)

## Why

Common Lisp is a Lisp-2: a symbol can name a function and a value at once, calls resolve through the function cell, and code references functions with `funcall` and `#'name`. The current interpreter is a Lisp-1 — the head of a form is evaluated as an ordinary value. This is the axis that most distinguishes a CL surface from a Clojure one, and it lands independently of truthiness.

## What Changes

- A namespace setting is added to the Dialect: Lisp-1 (single namespace) or Lisp-2 (separate function cell).
- The environment gains a function cell used only under Lisp-2.
- Under Lisp-2, head-symbol resolution in `evalList` consults the function cell, and the `funcall` form and `#'` function-reference evaluate against it. Under Lisp-1, head resolution is unchanged.
- The identity Dialect stays Lisp-1, so existing behavior is preserved; `funcall` and `#'` exist only where a Dialect selects Lisp-2.

Reader parsing of `#'` is a separate slice; this slice defines what `#'` and `funcall` mean once present.

## Impact

- Affected specs: `dialect`.
- Affected code: `core/env.go` (function cell), `core/eval.go` (head resolution, `funcall`, `#'` evaluation).
- No change for existing embedders on the default Dialect.

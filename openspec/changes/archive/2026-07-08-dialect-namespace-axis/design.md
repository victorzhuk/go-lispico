# Design — Dialect namespace axis (Lisp-1/Lisp-2)

## Context

The tree-walker's `evalList` currently evaluates the head as a value (`e.Eval(ctx, head, env)`) and applies it — pure Lisp-1. Lisp-2 requires a distinct function binding namespace so a symbol can be both a variable and a function, and requires `funcall`/`#'` to bridge value position and function position.

## Decisions

- **Function cell in the environment.** The `Env` chain gains a function-binding namespace, populated by function-defining forms under a Lisp-2 Dialect. Under Lisp-1 the function cell is unused and the environment behaves exactly as today.
- **Head resolution is axis-driven.** Under Lisp-2, a symbol in head position resolves against the function cell first; a symbol in argument position resolves against the value cell. Under Lisp-1, both resolve against the single namespace. The axis is read from the Engine's Dialect.
- **`funcall` and `#'` are Lisp-2 forms.** `#'name` yields the function bound to `name` in the function cell; `funcall` applies a function value obtained from value position. Both are present only when the Dialect selects Lisp-2, and are supplied through the Dialect's Delta, not the kernel default.
- **Definition forms populate the right cell.** Under Lisp-2, the function-defining form binds into the function cell; value-defining forms bind into the value cell. The identity (Lisp-1) Dialect keeps a single cell.

## Risks

- Lisp-2 head resolution changes a hot path; the Lisp-1 path must stay byte-for-byte behaviorally identical. Tests assert the default Dialect is unaffected.
- The VM does not learn Lisp-2 in this slice; an Engine on the Lisp-2 axis falls back to the tree-walker (consistent with ADR 0005). This slice does not modify the VM.

## Out of scope

Reader syntax for `#'` (slice `dialect-reader-flags`) and assembling the full CL dialect (slice `dialect-common-lisp-default`).

Plugins and `Engine.Bind` register into the value cell, so under Lisp-2 their functions are not reachable in head position without `funcall`. Registering plugin functions into the function cell is part of `dialect-common-lisp-default`; the default Lisp-1 Dialect is unaffected.

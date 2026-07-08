# Per-Engine dialect dispatch

## Why

The special-form table in `core/eval.go` is a package-global `map`, so the language an Engine speaks is a compile-time constant shared by every Engine in the process. Before any dialect can differ — Common Lisp, Clojure, a restricted rule subset — dispatch must become per-Engine state, and there must be a value that names a language configuration. This slice is the enabling prefactor: make the change easy, then the easy changes (axes, vocabulary, reader flags, the CL default) follow.

## What Changes

- The kernel defines its canonical special forms once under neutral names — the **Kernel table**.
- A **Dialect** value is introduced: a **Delta** (renames, additions, removals of special forms) over a declared base, where the base is either the full Kernel table or empty.
- Resolving a Dialect against its base yields the Engine's effective special-form table, held per-Engine rather than in a package global.
- A `WithDialect(...)` Engine option selects the Dialect at construction; it is immutable for the Engine's lifetime, and no evaluated code can change it.
- One identity Dialect ships, resolving to today's exact behavior. `runtime.New()` with no option keeps its current behavior — the default flip waits until the Common Lisp dialect exists.
- An empty-base Dialect is fail-closed: a special form absent from its Delta is uncallable, and a form later added to the Kernel table never leaks into it.

No language surface changes for existing embedders in this slice. It is behavior-preserving plumbing plus a new, unused-by-default selection point.

## Impact

- Affected specs: `dialect` (new capability), `runtime-api`.
- Affected code: `core/eval.go` (dispatch + per-Engine table), `core/dialect.go` (new Dialect value + kernel table), `runtime/engine.go` (option + wiring). The compiler and VM are untouched — an identity dialect maps one-to-one to canonical forms, and a non-identity dialect combined with the bytecode evaluator is rejected at construction.
- Existing behavior is pinned by characterization tests before the refactor moves it.

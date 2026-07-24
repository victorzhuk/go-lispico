## Why

A closure created inside a `loop` body captures the loop variable by reference,
and every `recur` writes through the same cell, so all closures from all
iterations read the final post-loop value instead of their per-iteration value.
Reproduced on both evaluators under the `clojure` dialect:

```
(def fns (loop [i 0 acc []]
  (if (< i 3) (recur (+ i 1) (conj acc (fn [] i))) acc)))
(list ((nth fns 0)) ((nth fns 1)) ((nth fns 2)))
;; => (3 3 3)   expected (0 1 2)
```

Root cause: `compileRecur` emits the rebind as `OpSetLocal`, which `finalize()`
rewrites to `OpSetCell` (write-through to the existing `*cellBox`) rather than
`OpBindCell` (fresh cell); the tree-walker's `evalLoop` likewise overwrites the
same `*Cell` in place each iteration. This silently breaks the idiomatic
"build a list of closures in a loop" pattern and diverges from the per-iteration
binding semantics of the Clojure model the forms are styled on.

## What Changes

- A closure created during a `loop` iteration SHALL capture the value the loop
  variables held at that iteration; a later `recur` SHALL NOT retroactively
  change what an earlier iteration's closure observes.
- Mechanism: a `recur` that rebinds a **captured** loop slot SHALL install a
  fresh cell for that slot (as a fresh `let` iteration would), instead of
  writing through the existing one. Non-captured loop slots keep the current
  stack-slot / write-through path unchanged.
- Both evaluators SHALL implement this identically and remain crossval-equal.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement that `loop`/`recur` gives per-iteration binding
  identity for captured loop variables, so closures created in the loop body
  observe their own iteration's values.

## Impact

- Code: `core/compiler/compiler.go` (`compileRecur` / `finalize` cell
  treatment for captured loop slots), `core/eval.go` (`evalLoop` fresh-cell for
  captured slots).
- Performance: only captured loop slots pay a per-iteration cell allocation
  (semantically required). Non-capturing loops — the common case, e.g. the
  `loop-sum` goldset cell — keep write-through and allocate nothing new, so the
  allocation gate is preserved.
- Crossval: both evaluators must produce `(0 1 2)` for the repro; add crossval
  cases for closures-in-loop.

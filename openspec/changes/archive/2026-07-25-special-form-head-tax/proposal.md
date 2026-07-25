## Why

Under the bytecode VM — the default execution mode — every top-level form headed by a
special form allocates and discards an error on every evaluation.

`runtime/eval.go:349` calls `MacroExpand` unconditionally per form, regardless of whether
the chunk cache hit; the cache amortizes compilation, not expansion. `MacroExpand` reaches
`resolveHead` (`core/eval.go`), which calls `e.Eval(ctx, head, env)` to discover whether
the head names a macro. A special form — `def`, `defn`, `do`, `when`, `unless`, and the
rest — is dispatched through `e.forms` in `evalList` (`core/eval.go:471`) and is never
bound as a value, so that evaluation fails, constructs a `*LispicoError` (struct plus a
`fmt.Sprintf`), and the error is silently swallowed to return "not a macro."

The tree-walking evaluator never pays this: `evalList` consults the special-form table
before ever looking the head up as a value.

Measured on the gold set, per iteration: `guard-nil` twice, `rule-load` eleven times. Both
run slower under the VM than under the tree-walker — `guard-nil` +34.78% to +53.25%,
`rule-load` +22.68% to +33.68% across two captures at p=0.000. The tax is general, not
specific to those fixtures: `safe-parse` and `registry-fold` pay it too and still win
comfortably because their per-form work is large enough to mask it, while `pipeline`, whose
single top-level form has no special-form head, pays none at all.

So this is a defect in the default execution path that happens to be visible only where
per-form work is small.

## What Changes

- `resolveHead` short-circuits a head that names a special form, returning "not a macro"
  without evaluating it. This mirrors the check `core/eval.go:1147` already performs.
- The short-circuit is placed **after** the existing Lisp-2 function-cell lookup and
  **before** `e.Eval`. Order matters: under a Lisp-2 dialect a user macro may shadow a
  special-form name through the function cell, and that lookup must keep winning as it does
  today.

## Capabilities

No spec deltas. Observable behavior is unchanged — `resolveHead` already returns "not a
macro" for these heads, via a failed evaluation whose error is discarded. This removes the
wasted work, not a result. Archive with `--skip-specs`.

## Impact

- Code: `core/eval.go`, one function.
- Risk: a genuinely undefined head must still be reported as undefined where callers expect
  that. `TestEval_MacroExpand_UndefinedHead` exists and must stay green; the short-circuit
  fires only for names present in `e.forms`, which an undefined symbol is not.
- Risk: reordering could change Lisp-2 shadowing behavior. The control is placing the check
  after the function-cell lookup, plus the crossval suite, which covers CL and Clojure
  dialects and includes function-cell rebind cases.
- Sequencing: independent of the other outstanding work. The two larger findings from the
  same investigation — that all `defmacro` falls back and flushes the chunk cache, and that
  per-form VM setup costs recur per top-level form — are filed separately and are not
  addressed here.

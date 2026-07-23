## Why

The tree-walker bounds call depth with a shared per-evaluation counter:
`evalState.callDepth` (`core/eval.go:89`) increments on every `Apply` and trips
`MaxDepth` (1000) at `core/eval.go:481`, so recursion laundered through the
higher-order builtins — `map`, `filter`, `reduce`, `apply` — still accrues and
fails closed.

The bytecode VM tracks depth on a per-instance field, `vm.depth`
(`core/vm/vm.go:84`, checked at `:1497`). But `bytecodeEvaluator.Apply` pulls a
fresh VM from the pool and `v.Reset()`s its depth to zero on every re-entrant
call (`runtime/eval.go:307-308`). So `vm.depth` never accumulates across a
higher-order re-entry, and the VM — the default evaluator — recurses unbounded.

Confirmed by reproduction under the Clojure dialect (so the conditional is not
itself affected by the separate CL-truthiness issue):

```
tree-walker  (deep 2000)    => EvalError: max call depth 1000 exceeded   ; bounded
vm (default) (deep 2000)    => 0, no error                               ; unbounded
vm (default) (deep 200000)  => 0, no error
```

`deep` recurses through `map`. The native Go stack is the only remaining
backstop, so a deep enough recursion is an uncatchable `fatal error: stack
overflow` that aborts the process — exactly the failure the "Structural
recursion is bounded" requirement forbids, on the call-depth axis it does not
yet cover.

## What Changes

- Expose the shared per-evaluation call-depth counter through `ctx`, mirroring
  the existing `EvalStructCounter` / `WithStructuralDepthCounter` mechanism
  (`core/eval.go:245`, `core/vm/vm.go:142`, threaded at `runtime/eval.go:312`).
- The VM observes the shared counter across the re-entrant pooled-`Apply`
  boundary, so recursion through `map`/`filter`/`reduce`/`apply` accrues and
  trips `MaxDepth` on the VM exactly as it already does on the tree-walker.
- Exceeding the bound returns the existing `*core.LispicoError` ("maximum call
  depth exceeded"), never a Go stack overflow.
- No new limit and no API change: `MaxDepth` (1000) already exists and is
  shared by both evaluators.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement `Call recursion is bounded across re-entrant
  apply`.

## Impact

- Code: `core/eval.go` (expose the shared `callDepth` atomic on the eval state,
  as `structDepth` already is), `core/vm/vm.go` (consume the shared call-depth
  counter across `Apply`/`ApplyPooled`), `runtime/eval.go` (thread it into the
  pooled-VM apply alongside the structural counter).
- Downstream: a recursive rule that fans through `map` on the shared engine no
  longer aborts the process.
- Parity: closes a tree-walker/VM behavioral divergence guarded by
  `crossval_test.go`.

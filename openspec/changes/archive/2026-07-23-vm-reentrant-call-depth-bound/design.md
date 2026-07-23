## Context

Two depth axes already exist and are bounded correctly:

- Structural depth (descending `Vector`/`HashMap`/quasiquote literals) is
  tracked on a shared per-evaluation `atomic.Int64` that both evaluators
  consume: `EvalStructCounter(ctx)` (`core/eval.go:245`) feeds the VM via
  `WithStructuralDepthCounter` (`core/vm/vm.go:142`), threaded at
  `runtime/eval.go:312`.
- Call depth in the tree-walker is tracked on `evalState.callDepth`
  (`core/eval.go:89`), a shared per-evaluation counter that survives re-entrant
  `Apply`, so higher-order recursion is bounded.

The gap: the VM's call depth lives on the per-instance `vm.depth` field, and a
pooled VM `Reset()`s it to zero on each re-entrant `Apply`. The mechanism to
fix it already exists one axis over.

## Decision

Reuse the structural-depth-counter pattern for call depth.

- Add a shared call-depth accessor on the eval state, symmetric with
  `EvalStructCounter` — e.g. `EvalCallCounter(ctx) *atomic.Int64` returning the
  same `evalState.callDepth` the tree-walker already bumps.
- The VM increments/decrements the shared counter across each re-entrant
  `ApplyPooled` entry and checks it against `maxDepth`, alongside its existing
  per-instance `vm.depth` check. The shared counter uses `sharedDepth > maxDepth`
  to match the tree-walker's boundary semantics (since the shared counter starts
  at 1 on re-entry).

`vm.depth` stays for intra-chunk `OpCall`/`OpTailCall` recursion (cheap,
per-instance, no atomics on the hottest path). The shared counter is consulted
only at the re-entrant `Apply` boundary — once per higher-order element call,
not per VM instruction — so the atomic cost lands off the dispatch hot loop,
the same place the structural counter already sits.

## Options considered

- **Seed `vm.depth` from the shared counter on re-entry** (base = current
  shared depth, restore on exit). Single depth notion, but requires the pooled
  `Apply` to write `vm.depth` before `Run` and couples the reset path to the
  counter. More invasive than mirroring the proven structural pattern.
- **Check both counters independently** (chosen): the VM trips on
  `vm.depth >= maxDepth` or `sharedCallDepth > maxDepth`. Smallest diff, mirrors the
  structural-counter code already in `Run`, keeps the per-instance fast
  path untouched, and preserves tree-walker boundary parity (`>`).

## Verification shape

The existing repro is the acceptance test: `(defn deep (n) (if (= n 0) 0
(first (map (fn (x) (deep (- n 1))) (list 1))))) (deep D)` under the Clojure
dialect, `D` past `MaxDepth`, must return "maximum call depth exceeded" on the
VM as it already does on the tree-walker — and `go test -race` must stay clean
(the shared counter is the same atomic the tree-walker uses, so concurrent
evaluations on one engine each carry their own eval-state counter).

## Non-goals

- Raising or making `MaxDepth` configurable — out of scope; 1000 is unchanged.
- Tail-call handling for higher-order recursion — `map` is not a tail position;
  this change bounds the recursion, it does not eliminate it.

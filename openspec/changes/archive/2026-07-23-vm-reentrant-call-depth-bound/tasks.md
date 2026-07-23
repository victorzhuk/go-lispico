## 1. Behavior contracts

- [x] 1.1 Red test (VM, Clojure dialect): `(deep D)` recursing through `map`
  past `MaxDepth` returns a `*core.LispicoError` "maximum call depth exceeded",
  not a completed value and not a stack overflow. Assert the same `D` on the
  tree-walker already fails the same way (characterization).
- [x] 1.2 Red test: recursion through each higher-order builtin —
  `map`, `filter`, `reduce`, `apply` — is bounded on the VM.
- [x] 1.3 Characterization: a legitimate deep-but-under-limit computation
  through `map` still completes on the VM (no false positive from the shared
  counter leaking across sequential top-level evals).
- [x] 1.4 Race test: two goroutines each run a bounded-recursion program on one
  shared engine; each is bounded by its own eval-state counter and `go test
  -race` reports no data race.

## 2. Implementation

- [x] 2.1 `core/eval.go`: expose the shared `evalState.callDepth` via a ctx
  accessor symmetric with `EvalStructCounter` (e.g. `EvalCallCounter`).
- [x] 2.2 `core/vm/vm.go`: accept the shared call-depth counter (VMOption
  mirroring `WithStructuralDepthCounter`); increment/decrement it across
  `ApplyPooled` entry; trip `maxDepth` on `vm.depth >= maxDepth` or `sharedCallDepth > maxDepth`.
- [x] 2.3 `runtime/eval.go`: thread the shared call-depth counter into the
  pooled-VM apply next to `WithStructuralDepthCounter(EvalStructCounter(ctx))`.
- [x] 2.4 Confirm the counter is decremented on every exit path from
  `ApplyPooled` (normal return, error return, and — given the panic-boundary
  change — recovered panic) so it cannot leak.

## 3. Integration

- [x] 3.1 Crossval: `crossval_test.go` extended so identical higher-order
  recursion programs bound identically on both evaluators.
- [x] 3.2 `go test ./... -race` green; `GOLDSET_MODE=vm` goldset gate
  non-increasing (the shared-counter touch is per-`Apply`, off the dispatch hot
  loop — verify no per-instruction atomic was added).

## 4. Verification

- [x] 4.1 `openspec validate --strict vm-reentrant-call-depth-bound`.
- [x] 4.2 CHANGELOG `[Unreleased]` note: VM now bounds recursion through
  higher-order builtins, matching the tree-walker.

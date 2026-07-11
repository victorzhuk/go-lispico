## Context

`runVM` (the `Eval`/`EvalCached` path) already gets/resets/puts a `*vm.VM` through `be.vmPool`. `bytecodeEvaluator.Apply` — the only path `Engine.Call` takes — instead does `fresh := vm.New(...)` per call, so every Go→Lisp function call allocates and discards a machine (and `vm.New` also eagerly builds a tree-walker it immediately overwrites). ADR 0006's whole premise is that the VM must stop allocating per call.

## Goals / Non-Goals

**Goals:**

- `core/vm/vm.go` supplies a public pooled receiver apply method (`ApplyPooled`) that reuses the existing receiver without `vm.New`, preserving `VM.Apply`'s documented fresh-isolation contract.
- `Engine.Call` runs on a pooled, reset VM via `ApplyPooled` — no per-call `vm.New`.
**Non-Goals:**

- The lazy-tree-walker construction in `vm.New` (a separate low-priority allocation win, backlog).
- Any behavior change to results — pooling is transparent.

## Decisions

- **Public `ApplyPooled` on `*vm.VM`.** New method calls `vm.apply(...)` directly on receiver (same logic as `VM.Apply` but without `vm.New`). Callers that own a reset VM call this; `VM.Apply` stays unchanged.
- **Reuse `runVM` semantics in `Apply`.** `bytecodeEvaluator.Apply` gets from `be.vmPool`, `Reset()`, set globals, set structural-depth counter, `ApplyPooled`, then `Put` — the same lifecycle `runVM` uses with `defer Put` after `Get`. Result isolation holds because `Reset()` clears stack/frames before reuse.

## Risks / Trade-offs

- **Isolation.** A pooled VM must be fully reset before an `Apply` run or state leaks between calls; the existing `Reset()` + a cross-val/`-race` check covers this — same guarantee the `Eval` path already relies on.

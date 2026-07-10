## Context

`runVM` (the `Eval`/`EvalCached` path) already gets/resets/puts a `*vm.VM` through `be.vmPool`. `bytecodeEvaluator.Apply` — the only path `Engine.Call` takes — instead does `fresh := vm.New(...)` per call, so every Go→Lisp function call allocates and discards a machine (and `vm.New` also eagerly builds a tree-walker it immediately overwrites). ADR 0006's whole premise is that the VM must stop allocating per call.

## Goals / Non-Goals

**Goals:**

- `Engine.Call` runs on a pooled, reset VM, matching the `Eval` path — no per-call `vm.New`.
- Docs stop contradicting shipped behavior (CL-on-VM; `fsm` status).

**Non-Goals:**

- The lazy-tree-walker construction in `vm.New` (a separate low-priority allocation win, backlog).
- Any behavior change to results — pooling is transparent.

## Decisions

- **Reuse `runVM` semantics in `Apply`.** `Apply` needs a VM bound to `fn`/`args`/`env`; get from `be.vmPool`, `Reset()`, set globals, run, put back — the same lifecycle `runVM` uses. Result isolation holds because `Reset()` clears stack/frames before reuse.
- **Docs are task-only.** The CL-on-VM and `fsm` corrections change no spec behavior (the CL-on-VM contract is already specified under `runtime-api`/`bytecode-vm`), so they are tasks, not spec deltas.

## Risks / Trade-offs

- **Isolation.** A pooled VM must be fully reset before an `Apply` run or state leaks between calls; the existing `Reset()` + a cross-val/`-race` check covers this — same guarantee the `Eval` path already relies on.

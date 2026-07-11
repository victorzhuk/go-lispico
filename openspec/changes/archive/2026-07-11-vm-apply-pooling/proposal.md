## Why

ADR 0006 commits the bytecode VM to reusing `*vm.VM` instances via `sync.Pool` so it never "recompiles and allocates a fresh machine per call". That reuse was wired into `Eval`/`EvalCached` (`runVM` gets/resets/puts a pooled VM) but **not** into `bytecodeEvaluator.Apply`, which every `Engine.Call` goes through — `Apply` does `fresh := vm.New(...)` on every invocation, silently reintroducing the exact per-call allocation ADR 0006 exists to eliminate. The struct doc even claims "VM pool reuse ... for reduced allocation", which is false for the `Call` path.

Two docs also now contradict the shipped VM behavior:

- `cl/cl.go` says the VM rejects the CL dialect via an `IsIdentity()` gate and "CL evaluation always runs on the tree-walker". ADR 0006 removed that gate; the default CL dialect runs on the VM (verified by the passing `TestRuntime_DefaultCL_AllowsBytecode`). An embedder reading this skips a real perf option.
- `CLAUDE.md`'s plugin status line names 7 of 8 plugins and omits `fsm`, which ADR 0004 and `README` classify as idle/no-consumer.

- `core/vm/vm.go` supplies a public pooled receiver apply method (`ApplyPooled`) that executes faithfully on the existing receiver without `vm.New`, preserving `VM.Apply`'s documented fresh-isolation contract for callers that want a fresh VM per evaluation.
- `bytecodeEvaluator.Apply` gets/resets/sets-globals/sets-structural-counter/runs via the pooled seam instead of allocating a fresh `vm.New(...)` per call, so the `Engine.Call` path matches the pooled `Eval` path.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: the execution requirement gains a clause that pooled VM instances are reused across **both** the `Eval` and `Apply`/`Call` paths, with result isolation preserved and structural depth counter wired on the pooled path — no per-call VM allocation on either path.

- Code: `runtime/eval.go` (`Apply` uses `be.vmPool`), `cl/cl.go` (doc comment), `CLAUDE.md` (plugin status line).
- ADRs: completes ADR 0006's per-call-allocation elimination; corrects docs that contradict ADR 0006 and ADR 0004.
- Invariants preserved: VM/tree-walker result agreement; result isolation between calls (a pooled VM is `Reset()` before reuse).
- Out of scope: everything in the `engine-resource-limits` and `hashmap-bulk-builder` changes; the backlog items.

## Why

After global lookups (33%) and cancellation checks (10%), the remaining fib(20) bytecode profile is dispatch-loop overhead: `VM.Run` itself is 19% flat, `Chunk.GetConstant` + `GetSymbolConstant` are ~9% (function calls with bounds checks and error construction on every constant/symbol access), and `push`/`pop` carry per-push side work. Structurally, the loop re-derives `frame := &vm.frames[len(vm.frames)-1]` and re-checks `frame.ip` range on **every instruction**, resolves the dialect truthiness hook per branch, and `push` zeroes a `canonicalAt` slot on every stack write.

Fast Go interpreters pay none of this per instruction: GopherLua caches `cf := L.currentFrame` and indexes `Code[cf.Pc]` directly on protos validated at compile time — and maintains forced inlining of tiny hot helpers as an ongoing discipline; goja executes precompiled instruction values with `pc`/`sp` as plain fields; tengo's loop is `switch v.curInsts[v.ip]` over fixed arrays with frames reused in place.

Separately, number boxing is measured (AllocsPerRun on v0.7.0): converting `core.Int` to `Value` is allocation-free for values 0..255 — Go's runtime small-value cache does cover the 8-byte pointer-free struct — but **negative ints, ints ≥ 256, and every runtime float allocate one heap object per conversion**. GopherLua preloads integers 0–127 and bulk-allocates the rest; goja caches −256..−1 (positives ride the same runtime cache); starlark-go preallocates small ints. lispico caches nothing, so counters past 255, sizes, and negative arithmetic all box per operation; `Bool`/`Nil` singletons cost nothing today but preboxing keeps them uniform.

## What Changes

- **Chunk validation at construction**: constant indices, symbol-constant types, jump/loop targets, and sub-chunk references are validated once when a chunk is built or enters the cache; a malformed chunk is rejected there with a typed error. The hot loop then indexes constants and code directly, without per-access error paths. The "never panics" robustness contract is unchanged — validation moves, it does not disappear.
- **Frame-local dispatch state**: `Run` caches chunk/code/ip/base/env in locals, syncing with the frame only at call, return, throw, and handler transitions.
- **Per-frame hook resolution**: the truthiness function is resolved once at frame entry, not per `OpJumpIfFalse`.
- **Preboxed small values** in `core`: package-level singletons for `Nil`, `Bool{true}`, `Bool{false}` and a preboxed `Int` range (−128..1023) returned by the reader, native ops, and stdlib arithmetic; value semantics (`Equals`, printing, hashing) unchanged.
- `canonicalAt` push-zeroing is removed by `vm-resolved-global-bindings`; if this change lands first, that bookkeeping is left untouched here to avoid conflict.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: modified requirement — robustness validation happens at chunk construction/load; execution of a validated chunk never panics and malformed chunks are rejected with typed errors before running.

## Impact

- Code: `core/vm/chunk.go` (validation), `core/vm/vm.go` (Run loop locals, frame sync), `core/types.go` + `core/reader.go` + `plugins/stdlib` (preboxed values).
- Expected effect: 10–20% on VM-bound cells; allocation-count reduction on arithmetic-heavy cells (helps ADR 0008 alloc tiers).
- Invariants: crossval parity, robustness fuzz/malformed-chunk tests move to the load boundary, `-race` suites.
- Sequencing: independent; pairs with `vm-resolved-global-bindings` (whichever lands second deletes the remaining `canonicalAt` code).

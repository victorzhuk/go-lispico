## Why

yagel ADR 0105 charges 10 million reductions and 64 MiB cumulative allocation per Rule load/dispatch with `ctx.Err()` checks every 1,024 reductions. go-lispico currently bounds structural depth, collection length, and cache entries (ADR 0007) but not eval-step count or cumulative allocation, so a tight allocation loop or macro-amplified work cycle exhausts the host without tripping any ceiling. This change adds per-evaluation reduction and allocation metering on top of the existing `ResourceLimits` struct.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxReductions int` and `MaxAllocationBytes int` (zero → conservative default; defaults `10_000_000` reductions, `64 * 1024 * 1024` bytes per evaluation, matching yagel ADR 0105 verbatim).
- Carry the limits into the per-call `evalState` (same context-threading pattern as `MaxStructuralDepth`, ADR 0003/0007).
- Charge one reduction per apply-trampoline iteration (tree-walker), per VM instruction decode, per macro-expansion step, per compiler emit, per plugin `GoFunc` dispatch.
- Check `ctx.Err()` every `defaultReductionCtxCheckInterval = 1024` reductions, reusing the existing batched-cancellation countdown introduced in v0.8.0 (no new per-step clock read).
- Charge approximate allocation bytes at every constructor (Vector / HashMap / List / String / Chunk / evalState); over-allocation raises `*core.LispicoError{Code: CodeResourceLimit}`.
- Counters are per-evaluation (per-call `evalState`), never engine-shared (preserves ADR 0003).
- Reader stays exempt (no ctx; its existing `MaxReaderDepth` covers it).
- Introduces ADR 0011.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new `ResourceLimits` fields + new requirement `Evaluation reductions and cumulative allocation are metered`.
- `core-engine`: new requirement `Per-evaluation reduction and allocation counters`.

## Impact

- Code: `runtime/{engine,eval}.go`, `core/{eval,env}.go`, `compiler/compiler.go`, `vm/vm.go`, every plugin's `GoFunc` dispatch path.
- Default values chosen so the existing test suite and goldset stay green.

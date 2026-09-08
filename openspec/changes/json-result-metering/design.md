## Context

See `proposal.md` for the 16-byte versus 32-byte scalar reproduction at commit `3bcf9c1`. `json/decode` calls `ChargeEvalAllocBytes` after `ValueDeepBytesContext`; the tree-walker and VM then apply their normal shallow return charge. `TestDecodeChargesDeepResultBytes` invokes the plugin function directly, bypassing that second charge.

The accepted baseline for implementation is `json-int64-decoding` after implementation and archive. This change adds one requirement and does not replace that predecessor's JSON requirement blocks.

## Goals / Non-Goals

**Goals:** preserve full deep accounting while suppressing only the duplicate dispatch charge for that same successful result.

**Non-Goals:** numeric conversion changes, global ledger redesign, eager JSON parsing limits, or changes to encoding.

## Decisions

- At the existing successful return boundary, use `core.ChargeGoFuncResultBytes(ctx, deep)` instead of the generic allocation charge. This API charges the full amount and marks only the active dispatch as accounted.
- Preserve depth and deep-size checks before the result charge; propagate their terminal errors without publishing the decoded value. Do not charge zero: the entire decoded result is newly constructed.
- Keep direct plugin tests, but make public `Engine.Call` and `core.Evaluator.Apply` the regression seams. They reach the centralized fallback that a direct `GoFunc.Fn` call omits. Run the named-call seam with `WithTreeWalker()` and `WithBytecode()` explicitly.

## Verification

Testing mode: existing-service-strict. First add a regression proving that decoding `"42"` through dispatch consumes 32 bytes instead of its 16-byte deep value charge. After the fix, assert exact equality with `core.ValueDeepBytes` for scalars, strings, empty containers, and nested vectors/maps.

Use fresh engines and fresh contexts from `core.WithEvalResourceLimits`; give reductions ample headroom. At the named-call boundary there is no source-reader/compiler allocation to confuse result bytes. For each fixture, allocation ceiling equal to its expected deep charge must succeed; one byte below must fail with terminal `core.CodeResourceLimit` and no value. Keep expected result values independent of the failing invocation.

Exercise two decodes sharing one evaluation ledger and a decode followed by another builtin result, so the callee marker cannot suppress later calls. Preserve predecessor numeric tests and `runtime/value_walk_publication_test.go` terminal-refusal coverage.

## Risks / Trade-offs

- A zero-byte marker would suppress required deep charges → assert exact full-result bytes, not merely lower totals.
- Meter setup can hide source overhead → use direct named calls for thresholds and source evaluation only for parity.
- A marker escaping its dispatch would undercharge later calls → retain cumulative and sequential-dispatch assertions.

## Migration Plan

Section 0 of the task list blocks implementation until `json-int64-decoding` is implemented and archived. Update the existing changelog for corrected allocation-limit behavior. No data migration or new architecture document is needed; rollback restores premature budget rejection.

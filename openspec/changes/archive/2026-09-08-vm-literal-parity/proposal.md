## Why

The code review of commit `3bcf9c1` found `(quote 1 2)` returning `1` on the VM while the tree-walker rejects it, and `()` returning `nil` instead of an empty list. Both violate the compiled-subset parity contract on small, deterministic inputs.

## What Changes

- Validate that `quote` has exactly one operand before emitting its constant.
- Preserve the empty `core.List` value when compiling an evaluated empty list.
- Pin valid quotation, malformed arity and empty-list runtime type through both evaluator modes and shipped dialects.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: add exact quote-arity and empty-list evaluation requirements.

## Impact

Primary code: `core/compiler/compiler.go`; compiler and VM cross-validation tests plus public runtime coverage. Update existing `CHANGELOG.md` when implemented. No reader, dependency, API, constant-folding or dialect redesign.

Malformed extra-operand quotations will stop succeeding; evaluated empty lists will retain their type. This change is independent of the other VM repairs and adds its own requirements without replacing `Bytecode VM execution`.

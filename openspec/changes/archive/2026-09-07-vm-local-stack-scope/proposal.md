## Why

The code review of commit `3bcf9c1` found ordinary nested expressions corrupting VM operands: `[10 (let [x 2] x)]` returns `[2 2]` instead of `[10 2]`, and `(+ 10 (let [x 2] x))` returns `4` instead of `12`. Loop bindings also escape their scope and change how sibling initializers resolve names.

## What Changes

- Separate frame-local storage from expression operands and restore operand height at lexical exits and loop back edges.
- Restore enclosing bindings after `loop`; evaluate its initializers in the enclosing scope, matching the tree-walker.
- Preserve sequential kernel `let` and `let*`, captured-cell aliasing, per-iteration capture identity, native operator freezing, and variadic calls.
- Reconcile stale parallel-`let` clauses with the sequential behavior adopted by `align-clojure-dialect-surface`.
- Pin expected values through compiler/VM and public runtime tests before changing execution.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: add local/operand separation and loop-scope requirements; correct `Kernel let binding scope parity` to sequential semantics.
- `core-engine`: correct the stale parallel-`let` phrase in `let/let*/loop accept List or Vector bindings`, preserving its accepted shapes and scenarios.

## Impact

Primary code: `core/compiler/compiler.go`, `core/compiler/stack.go`, `core/vm/vm.go`, `core/vm/chunk.go`, `core/vm/frame.go`; regression coverage in compiler, VM and runtime tests. Update existing `ARCHITECTURE.md` and `CHANGELOG.md` when implementing the repaired storage contract.

No dependency or runtime API additions. Corrected results can change programs that relied on VM corruption. Frame layout, captures and native fusion require existing-service-strict verification; compiled instruction changes may shift reduction counts.

Implement and archive this change before `vm-scoped-definition-fallback` and `vm-runtime-error-parity`. `vm-literal-parity` is independent.

## Why

Kernel `let` binds in parallel on the tree-walking Evaluator (inits are evaluated in the parent scope) but sequentially under the VM: `(def a 10) (let [a 1 b a] b)` evaluates to `10` on the Evaluator and `1` under the VM. This violates the bytecode-vm capability's core requirement — results identical to the tree-walker for every compiled form — and is a known parity defect on the VM correctness floor (ADR 0008), so it blocks consumer gate authorization. Surfaced while authoring the gold-set corpus (release-consumer-gate tasks.md 5.1).

## What Changes

- `core/compiler`: `compileLet` compiles all binding init expressions before registering any of the new locals, so inits resolve names in the enclosing scope. Emitted bytecode is unchanged — only symbol-resolution timing during compilation moves; `compileLetStar` keeps its sequential registration and stops being byte-identical to `compileLet`.
- Pinned cross-mode regression: the shadowing case `(def a 10) (let [a 1 b a] b)` asserted equal under both execution modes, plus `let*` sequential behavior asserted unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: pin kernel `let` parallel-binding scope — init expressions SHALL resolve in the enclosing scope, matching the tree-walker; `let*` SHALL remain sequential.

## Impact

- `core/compiler/compiler.go` (`compileLet`), compiler/VM tests.
- No public API change; no bytecode format change; existing cached chunks compiled from `let` forms that relied on sequential capture were producing wrong results already.
- Unblocks one item of the VM correctness floor for the release-consumer-gate change; the gold-set fixture `safe-parse` already uses `let*` for its intended sequential binding.

## Why

The code review of commit `3bcf9c1` found that `(def x 10) ((fn [] (def x 1))) x` returns `1` on the VM and `10` on the tree-walker. Compiled definitions write to closure globals even when the language requires a binding in the current lexical scope.

## What Changes

- Reject compilation of scoped `def` and `defn` with existing `compiler.CodeUnsupported`, including Lisp-2 function definitions and scopes with no bindings.
- Evaluate the entire enclosing top-level form through existing tree-walker fallback before executing any bytecode from that form.
- Keep definitions in the actual top-level scope compiled, including top-level `do` bodies.
- Preserve results, lexical writes and once-only evaluation side effects without allocating an `Env` for every compiled call.
- Document the narrower compiled subset and pin both evaluator paths.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: add an explicit whole-form fallback requirement for definitions in lexical scopes.

## Impact

Primary code: `core/compiler/compiler.go`; runtime fallback integration in `runtime/eval.go`; compiler, VM and runtime regressions. Update existing `ARCHITECTURE.md`, `docs/adr/0002-bytecode-vm-disposition.md`, `docs/adr/0013-bytecode-default-authorized-by-the-gold-set.md`, and `CHANGELOG.md` to name the fallback surface accurately.

No new runtime API or dependency. Affected functions execute through the tree-walker, so function representation, performance and reduction counts can change. Native lexical-definition support and per-call environment mirroring are outside scope.

Implement and archive `vm-local-stack-scope` first. This change adds its own requirement and does not replace `Bytecode VM execution`.

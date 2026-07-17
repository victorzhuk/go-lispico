## Why

Four known VM defects live at compile time and block the VM correctness floor (PRD stories 4–7, 9): lexical `set!` can update the wrong scope and a `set!` on an undefined binding can silently create state; false `when` and true `unless` paths can underflow the stack instead of leaving `nil`; compiling a `try` normal body can shift the catch slot and corrupt later local-slot layout; and malformed special forms can panic the Compiler because operands are indexed before arity and shape are validated. Each changes observable Rule behavior or terminates the host when the VM is enabled.

## What changes

- Every compiled expression leaves exactly one result on the stack; non-executed `when`/`unless` bodies produce `nil`.
- Definition and mutation get distinct bytecode semantics: a definition writes to the current scope; `set!` updates the scope that already owns the binding and returns a typed error when none exists. Local mutation keeps using the resolved local slot.
- A catch binding exists only in the handler scope: compiling a `try` normal body neither reserves nor shifts the catch slot, and leaving the handler restores the previous local layout.
- The Compiler validates arity and shape for every compiled special form before indexing operands, returning typed errors — no panic on any malformed input.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: the execution requirement gains single-result, definition-versus-mutation, and try/catch slot-layout clauses; the robustness requirement gains pre-indexing shape validation for every compiled special form.

## Impact

- ADRs: part of the VM correctness floor required by ADR 0008 before any consumer performance result counts.
- Invariants preserved: VM/tree-walker result agreement; no panics — all errors typed and returned.
- Out of scope: runtime-state defects (keyword calls, structural depth, pooling isolation) tracked by `vm-runtime-state-parity`; `cond` clause shape tracked by `dialect-cond-form-shape`.

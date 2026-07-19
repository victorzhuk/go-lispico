# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Native arithmetic and comparison opcodes

The VM SHALL execute `+`, `-`, `*`, `/`, `<`, `>`, `<=`, `>=`, `=` through
dedicated opcodes operating on stack slots, with semantics identical to the stdlib
builtins including int/float promotion and division-by-zero errors. The compiler
SHALL emit these opcodes for a canonical native operator whether or not a dialect
is configured — a configured dialect (the shipped runtime path) SHALL NOT suppress
native-opcode emission for an operator that is not a special form and not locally
shadowed. When the operator symbol is locally shadowed or its global binding is no
longer the canonical stdlib builtin, execution SHALL fall back to the ordinary call
path. Canonical status SHALL be determined through the operator's resolved binding,
not re-derived by a per-execution environment walk, and a canonical operator SHALL
take the native path on every execution — not intermittently.

#### Scenario: Hot loop avoids builtin dispatch

- **WHEN** a `loop` body evaluates `(+ acc 1)` under the VM
- **THEN** the addition SHALL execute as an opcode without a `GoFunc` invocation, and the loop result SHALL equal the tree-walker's

#### Scenario: Native opcodes emitted under a configured dialect

- **WHEN** `(+ a b)` or `(< a b)` is compiled with a configured dialect (the default CL dialect or clojure) and run on a `WithBytecode()` engine
- **THEN** the operator SHALL compile to and execute as its native opcode with no `GoFunc` dispatch, matching the tree-walker's result

#### Scenario: Promotion parity

- **WHEN** `(+ 1 2.5)` and `(< 1 1.5)` run under the VM
- **THEN** results SHALL equal the stdlib builtins' results (`3.5`, `true`)

#### Scenario: Rebound operator falls back

- **WHEN** a program rebinds `+` to a custom function and then calls `(+ 1 2)` under the VM
- **THEN** the custom function SHALL be called, matching tree-walker behavior

#### Scenario: Recursive calls keep the native path

- **WHEN** a recursive function's body applies canonical `+`, `-`, and `<` across nested self-calls under the VM
- **THEN** every application SHALL execute as a native opcode, with no fallback to `GoFunc` dispatch for canonical bindings

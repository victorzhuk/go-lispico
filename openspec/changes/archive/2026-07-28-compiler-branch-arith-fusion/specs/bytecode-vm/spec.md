# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Native arithmetic and comparison opcodes

The VM SHALL execute `+`, `-`, `*`, `/`, `<`, `>`, `<=`, `>=`, `=` through
dedicated opcodes operating on stack slots, with semantics identical to the stdlib
builtins including int/float promotion and division-by-zero errors. The VM MAY
specialize execution for a subset of argument shapes — for example two arguments
that are both integers — but a specialized path SHALL be indistinguishable from
the general path in every observable respect: the same result value, the same
error, and the same allocation charged to the evaluation ledger. In particular
integer overflow SHALL wrap identically on both paths, since the general path
relies on Go's wrapping arithmetic and a specialized path that instead reported
an error or saturated would change observable behavior. The compiler
SHALL emit these opcodes for a canonical native operator whether or not a dialect
is configured — a configured dialect (the shipped runtime path) SHALL NOT suppress
native-opcode emission for an operator that is not a special form and not locally
shadowed. The operator head SHALL be resolved through the cell the active dialect
uses for head position — the value cell for a Lisp-1 dialect, the function cell for
a Lisp-2 dialect (the default CL surface) — so that a rebind through that cell is
observed, and the resolution SHALL occur before argument evaluation, freezing both
the canonical decision and, when non-canonical, the operator value, so a rebind
during argument evaluation affects neither. Resolving a canonical operator head
SHALL NOT materialize the operator value onto the value stack — the canonical
path SHALL touch only the argument slots. When the operator symbol is locally
shadowed or its head-cell binding is no longer the canonical stdlib builtin,
execution SHALL fall back to the ordinary call path over the head-time-resolved
value. Canonical status SHALL be determined through the operator's resolved
binding, not re-derived by a per-execution environment walk, and a canonical
operator SHALL take the native path on every execution — not intermittently.

In addition, the compiler MAY fuse a native operator's canonicality-freeze
and its two operands into a single instruction when both operands are a
local slot or a compile-time constant (local×local or local×const; an
arbitrary sub-expression operand — "stack×stack" — is out of scope, since it
can execute code and rebind the operator mid-evaluation, which the freeze
protocol exists to prevent):

- a native comparison whose boolean result feeds a conditional branch, and
  a native arithmetic op, SHALL both be emittable this way — one fused
  instruction that resolves the operator, reads both operands, and pushes
  the single result the unfused sequence would have produced. The
  conditional branch (or whatever consumes an arithmetic result) SHALL
  remain a separate, ordinary instruction, emitted exactly as it is today:
  a rebind to a VM-compiled closure pushes a new call frame and returns
  asynchronously, so no single instruction can both compute a result and
  branch on one that may not exist until a later frame returns.

A fused instruction SHALL preserve, bit-for-bit, the semantics of the
sequence it replaces: the same canonicality freeze point, the same
non-canonical fallback (including a fallback callee that is itself a
VM-compiled closure), the same numeric edge behavior (division by zero,
float promotion), and the same allocation-ledger charge as the unfused fused
native op. Shapes not covered by a fusion SHALL compile exactly as before.
Chunk validation SHALL verify every fused instruction's operand indices
before the chunk runs, preserving the validated-chunk invariant that the
dispatch loop performs no per-instruction bounds checks.

#### Scenario: Hot loop avoids builtin dispatch

- **WHEN** a `loop` body evaluates `(+ acc 1)` under the VM
- **THEN** the addition SHALL execute as an opcode without a `GoFunc` invocation, and the loop result SHALL equal the tree-walker's

#### Scenario: Native opcodes emitted under a configured dialect

- **WHEN** `(+ a b)` or `(< a b)` is compiled with a configured dialect (the default CL dialect or clojure) and run on a `WithBytecode()` engine
- **THEN** the operator SHALL compile to and execute as its native opcode with no `GoFunc` dispatch, matching the tree-walker's result

#### Scenario: Canonical operator adds no stack traffic

- **WHEN** a compiled body applies a canonical native operator under the VM
- **THEN** execution SHALL push and pop only the operator's arguments and result — no operator value SHALL transit the value stack

#### Scenario: Promotion parity

- **WHEN** `(+ 1 2.5)` and `(< 1 1.5)` run under the VM
- **THEN** results SHALL equal the stdlib builtins' results (`3.5`, `true`)

#### Scenario: Specialized and general paths agree

- **WHEN** the same operator is applied to two integers, including values at the `int64` limits where the result wraps, and to argument shapes the specialization does not cover
- **THEN** results, errors, and charged allocation SHALL be identical to what the general path produces for the same arguments

#### Scenario: Rebound operator falls back

- **WHEN** a program rebinds `+` to a custom function and then calls `(+ 1 2)` under the VM
- **THEN** the custom function SHALL be called, matching tree-walker behavior

#### Scenario: Mid-argument rebind does not flip the decision

- **WHEN** an argument expression of a canonical `(+ ...)` application rebinds `+` while the arguments are being evaluated
- **THEN** that application SHALL complete under the decision and value resolved before argument evaluation, matching the tree-walker's head-then-arguments order

#### Scenario: Lisp-2 function-cell rebind falls back

- **WHEN** under the CL dialect a program rebinds `+` in the function cell (`(defun + (a b) (- a b))`) and then calls `(+ 5 3)` under the VM
- **THEN** the rebound function SHALL be called (result `2`), matching the tree-walker — the native opcode SHALL NOT execute over the stale canonical value-cell binding

#### Scenario: Recursive calls keep the native path

- **WHEN** a recursive function's body applies canonical `+`, `-`, and `<` across nested self-calls under the VM
- **THEN** every application SHALL execute as a native opcode, with no fallback to `GoFunc` dispatch for canonical bindings

#### Scenario: Fused compare matches unfused semantics

- **WHEN** `(if (< n 2) a b)` executes with `<` canonical, and separately after `<` has been rebound to a builtin, a tree-walker closure, and a VM-compiled closure
- **THEN** the result SHALL be identical to the tree-walker in every case — the fused instruction takes the native path only under the same conditions the unfused sequence would, and the trailing conditional branch consumes whichever result the fused instruction or its fallback call eventually produced

#### Scenario: Fused arithmetic matches unfused semantics

- **WHEN** `(- n 1)` compiles to a fused local/const instruction and executes, including division-by-zero and float-operand edge cases for the other arithmetic ops
- **THEN** results and errors SHALL be identical to the unfused native-op sequence, and the allocation ledger SHALL be charged identically

#### Scenario: Validation covers fused operands

- **WHEN** a chunk containing fused instructions with out-of-range slot, constant, or operator-site operands is loaded
- **THEN** `Validate` SHALL reject it before execution

#### Scenario: Uncovered shapes compile as before

- **WHEN** an operand of a native comparison or arithmetic op is neither a local slot nor a compile-time constant (a nested call or other sub-expression)
- **THEN** the compiler SHALL emit the existing unfused sequence unchanged

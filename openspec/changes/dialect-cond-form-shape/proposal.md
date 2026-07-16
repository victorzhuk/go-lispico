## Why

Clojure `cond` uses flat test/expression pairs; the kernel parses Common Lisp-style nested clauses. Under the Clojure dialect a flat `cond` is rejected or misread, and the Evaluator and Compiler each parse clause shape on their own — two parsers that can drift is exactly the failure ADR 0009 rejects. Rule authors writing `.clj` source against YAGEL's documented dialect hit this first (PRD stories 2, 3).

## What changes

- The Dialect gains a **Form-shape rule** for `cond`: a dialect-owned normalizer invoked at special-form dispatch that produces canonical clauses for both Evaluator and Compiler.
- A canonical clause is one test plus one body expression; a Common Lisp clause with a multi-expression implicit-progn body normalizes by wrapping the body in kernel `do` (ADR 0009).
- The Clojure dialect accepts flat pairs; the Common Lisp dialect retains nested clauses. Reader output is not rewritten, so quoted `cond` data stays untouched.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: gains the Form-shape rule requirement — one dialect-owned clause normalizer shared by both execution paths, leaving quoted data unchanged.
- `bytecode-vm`: Dialect-axis execution extends to `cond` clause shape — both dialects' `cond` forms compile and run with results equal to the tree-walker.

## Impact

- ADRs: implements ADR 0009; respects ADR 0005 (dialect fixed at construction) and ADR 0006 (VM honors all dialect axes).
- Invariants preserved: Evaluator/VM result agreement; quoted data never rewritten; no dependence on stdlib bootstrap for a kernel form.
- Out of scope: other special forms' shapes; reader changes; the VM correctness-floor defects tracked by `vm-compile-shape-and-scope` and `vm-runtime-state-parity`.

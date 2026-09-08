## Context

See `proposal.md` for the failures at commit `3bcf9c1`. `compileList` emits `OpNil` for an empty list and only checks for a missing `quote` operand. `evalQuote` requires exactly one operand; `engine.Eval` returns an evaluated empty `List` unchanged.

## Goals / Non-Goals

**Goals:** Exact quote arity and empty-list value/type parity with the tree-walker.

**Non-Goals:** Reader changes, broader special-form arity cleanup, altered truthiness, quasiquote redesign, or new literal allocation charges.

## Decisions

1. Require one `quote` operand before indexing or emitting bytecode. Use existing typed compile-error construction. Extra operands are rejected rather than evaluated or discarded; valid quotation still returns its datum without evaluation.
2. Emit an empty-list constant through the existing constant path instead of `OpNil`. Preserve `core.List` type and empty contents. Do not add collection construction or allocation charging where the reference evaluator returns the existing datum.
3. Test runtime type and structural equality, not only rendering. Drive the same valid and malformed forms through both shipped dialects and both evaluator modes, including repeated evaluations. Compiler error codes may retain their existing compile/evaluation distinction; neither mode may accept malformed arity.

## Risks / Trade-offs

- Treating the empty list as a newly constructed collection could add a charge or depth check → retain the plain constant behavior used by quotation.
- Dialect truthiness can make list/nil substitution observable beyond printing → assert the actual returned value type without changing dialect predicates.
- Consumers may rely on ignored extra operands → release notes state that malformed quotation now fails.

## Migration Plan

Add failing compiler and runtime cases, make the two local compiler corrections, run bounded parity checks, and add an existing changelog entry. No predecessor or persistent migration is required.

# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Resolved global bindings

Compiled chunks SHALL continue to resolve global reads through per-site
resolved-binding caches guarded by cell mutation versions, with redefinition,
tombstoning, rebuild, and hot-reload observed exactly as by per-call
resolution.

A global read whose value is immediately invoked as the callee of a call at
the same site MAY additionally be executed as one fused global-call
instruction. The fused instruction SHALL resolve the callee through the same
versioned site cache, freeze the resolution at the same point in evaluation
order as the unfused sequence (before argument evaluation), and on a cache
hit whose callee is a compiled closure MAY push the callee frame directly.
Any other outcome — version mismatch, tombstone, non-closure callee, arity
error — SHALL follow the exact unfused resolution and application semantics,
including Lisp-2 function-cell-first head resolution and the rule that a
value-cell fallback resolution is never cached. Fused call sites SHALL be
covered by chunk validation.

#### Scenario: Fused site observes redefinition

- **WHEN** the callee global at a fused call site is redefined (or deleted and redefined) between two executions
- **THEN** the second execution SHALL invoke the new binding, identically to per-call resolution

#### Scenario: Head freeze order is preserved

- **WHEN** an argument expression of a fused call rebinds the callee name during argument evaluation
- **THEN** the call SHALL use the binding resolved before argument evaluation, exactly as the unfused sequence does under both evaluators

#### Scenario: Non-closure callee falls back

- **WHEN** a fused call site's callee resolves to a keyword, GoFunc, or other non-closure callable
- **THEN** the call SHALL behave identically to the unfused `GET_GLOBAL` + `CALL` sequence

#### Scenario: Hot-reload invalidates fused sites

- **WHEN** hot-reload or `UnloadPlugin` changes what a fused site's name resolves to
- **THEN** subsequent executions SHALL behave exactly as on an engine that resolves per call

# Active change dependency order

OpenSpec 1.11 validates each change independently but does not enforce
cross-change `Blocked by` metadata. Treat this file and each change's section 0
task as the promotion gate.

```text
builtin-resource-accounting             -> cl-collection-adapters
builtin-resource-accounting             -> stdlib-nil-lookup-semantics
stdlib-bootstrap-evaluator-ownership    -> stdlib-nil-lookup-semantics
cl-collection-adapters                  -> stdlib-typed-error-compliance
stdlib-nil-lookup-semantics             -> stdlib-typed-error-compliance
stdlib-nil-lookup-semantics             -> stdlib-bootstrap-cache-retirement
stdlib-typed-error-compliance           -> stdlib-nil-sequence-semantics
stdlib-nil-sequence-semantics           -> stdlib-builtin-resource-migration
```

An arrow means the predecessor must be implemented, validated, and archived into
the canonical specs before work starts on the successor. Transitive predecessors
need not be repeated in every proposal.

The active deltas intentionally do not modify the same canonical requirement.
Before promotion, search all active `spec.md` files for duplicate requirement
headings within the same capability and change type. Stop if any are found: the
archive command applies deltas independently, so prose ordering cannot make
overlapping replacements safe.

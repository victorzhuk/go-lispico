# Active change dependency order

This set addresses the code and architecture review of commit `3bcf9c1`.
Every change contains a proposal, design, capability deltas, and unchecked
implementation tasks. Testing mode is existing-service-strict. The previously
listed dependency chain is archived; the table below covers the active set.

## Changes and suggested merge order

| Order | Change | Accepted outcome | Capabilities |
| --- | --- | --- | --- |
| 1 | [metered-call-deadlines](metered-call-deadlines/proposal.md) | Attaching a meter preserves the configured and inherited call deadlines | runtime-api |
| 2 | [evaluation-outcome-settlement](evaluation-outcome-settlement/proposal.md) | Events and statistics describe the final settled result exactly once | runtime-api |
| 3 | [reader-budget-enforcement](reader-budget-enforcement/proposal.md) | Scan work, workspace, and AST construction obey existing budgets during reading | core-engine, runtime-api |
| 4 | [plugin-binding-rollback](plugin-binding-rollback/proposal.md) | Failed registration restores owned bindings and lazy state while preserving concurrent host writes | runtime-api |
| 5 | [stdlib-integer-minmax](stdlib-integer-minmax/proposal.md) | Integer-only extrema preserve the full int64 range | stdlib-plugin |
| 6 | [json-int64-decoding](json-int64-decoding/proposal.md) | Integral JSON values in int64 range decode exactly; other supported values retain finite Float fallback | json-plugin |
| 7 | [json-result-metering](json-result-metering/proposal.md) | Decoded results receive one complete deep allocation charge | json-plugin |

The four bytecode-VM semantic changes that opened this set — `vm-local-stack-scope`,
`vm-scoped-definition-fallback`, `vm-runtime-error-parity`, and `vm-literal-parity` —
are implemented and archived. Their accepted text lives in the canonical specs.

This order coordinates shared-file edits. Independent changes may proceed in
parallel when their implementation files do not overlap. The dependencies below
are mandatory, regardless of the suggested order.

## Implementation and archive prerequisites

```text
metered-call-deadlines        -> reader-budget-enforcement
evaluation-outcome-settlement -> reader-budget-enforcement
json-int64-decoding           -> json-result-metering
```

An arrow requires the predecessor to be implemented, verified, and archived into
the canonical specs before the successor starts. Reader enforcement requires
both runtime predecessors. Each successor repeats its gate in task section 0.
Deadline composition, outcome publication, and plugin rollback otherwise have no
semantic dependency on each other; their order coordinates runtime file edits.

## Requirement ownership

The active deltas have one owner per capability/requirement pair. Only these
changes replace existing requirement blocks:

- `reader-budget-enforcement`: **Structural recursion is bounded** and
  **Evaluation reductions and cumulative allocation are metered**.
- `json-int64-decoding`: the three existing JSON requirement blocks, reconciling
  integer detection and float fallback without weakening deep allocation or
  linear object construction.

All other deltas add uniquely named requirements. In particular,
`json-result-metering` does not replace its predecessor's numeric requirements.
Before archive, reject duplicate active owners, additions whose names already
exist, or modifications whose canonical requirement is absent. Rebase changes
against accepted predecessor text without reintroducing old clauses.

## Compatibility decisions

- Reader scan, workspace, conversion, and construction charges consume existing
  ceilings, with no new limit fields. Guarded numeric diagnostics truncate long
  tokens. Legacy context-free reads and filesystem source acquisition remain
  outside the guarded parsing contract.
- JSON exact integer detection expands from the tested 2^53 cutoff to int64.
  Fractional and out-of-range values keep finite Float fallback; overflow remains
  an error. Public numeric types therefore change for some inputs.
- Plugin rollback concerns engine-owned registration effects, not arbitrary Go
  state or external effects. The forwarding environment supplied to `Init` is
  not pointer-identical to `RootEnv()`; the canonical root and existing live
  binding identities remain stable. Concurrent host writes survive rollback.

## Verification

Validate the complete proposal set with:

```sh
openspec validate --changes --strict --json --concurrency 2
openspec status --all --json
```

Strict validation and completed planning artifacts do not mean implementation
tasks are complete. Each change requires regression tests and the bounded
verification commands in its task list before archive. The review's pre-existing
ignored `bin/` probe sources can break `make test` package discovery; resolve that
workspace obstruction before treating the full wrapper as an implementation
gate. This proposal set does not remove those files or weaken the gate.

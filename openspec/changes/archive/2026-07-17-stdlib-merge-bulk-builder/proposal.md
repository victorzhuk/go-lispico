## Why

stdlib `merge` builds its fresh result through repeated immutable `Assoc`, copying the accumulating map once per key — fresh-map construction is O(n²) in bytes and allocations. This dominates both execution paths in YAGEL's map-heavy envelopes, so it is the first profile-proven **Shared-path fix** under ADR 0008 (PRD story 25): fixed for consumer health, measured and reported separately, no credit toward VM thresholds. The mutable construction escape hatch it needs already exists (hashmap-bulk-builder change).

## What changes

- `merge` constructs its fresh result through the mutable bulk-builder escape hatch, then returns it as an ordinary immutable map.
- Observable semantics are unchanged: inputs stay immutable, iteration stays deterministic, right-most map wins on duplicate keys, zero-map and nil-argument behavior and existing type errors are preserved.
- A benchmark over increasing map sizes demonstrates bytes and allocation growth is no longer quadratic.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: gains a requirement that `merge` builds its result in linear allocation cost while preserving its documented semantics.

## Impact

- ADRs: the Shared-path fix named by ADR 0008; reported separately from VM gains.
- Invariants preserved: value immutability at the `Equals` level (the builder is internal construction, per the existing escape-hatch contract); deterministic evaluation.
- Out of scope: any other collection or stdlib cleanup — ADR 0008 forbids a broad pass without a profile-proven pathology.

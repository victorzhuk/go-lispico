## Context

Extends the archived `metering-amplifying-builtin-charge` pass to two paths it
missed. Both are small, but they touch two subsystems (stdlib and the VM) with
different charge mechanisms, so the approach is worth stating.

## Goals / Non-Goals

- Goal: `assoc` and fused native-op results are charged consistently with their
  already-charged siblings (`conj`/`merge` and the GoFunc dispatch path).
- Goal: no allocation-gate regression — charges are size computations, not
  allocations.
- Non-Goal: reworking the size table or the metering interface.

## Decisions

### assoc uses the existing collection charge

`assoc` routes through `chargeCollectionResult` exactly as `conj`'s map branch
and `merge` do — same deep-bytes measure, same length check, same failure mode.
No new mechanism; it is a missed call site, not a new policy.

### Fused ops charge a fixed scalar size

`execNativeFastFused` charges a fixed scalar size (the same the GoFunc path
charges for a scalar result) at the dispatch site, rather than a deep walk — a
fused arithmetic/comparison result is always a scalar (`Int`/`Float`/`Bool`), so
a fixed size is exact and O(1). Preboxed results are charged the same fixed size
for consistency; the goal is parity with the non-fused path, not a new
exemption. This keeps the fused hot path allocation-free (a counter add, no Go
allocation), preserving the goldset posture.

## Risks / Trade-offs

- The fused-op charge is on the hottest arithmetic path; it must be a plain
  ledger add with no allocation or lock. Verify the goldset VM cells stay
  non-increasing — this is the one place a careless charge could regress the gate.

## Migration

None. Internal accounting only; no observable value change under budget.

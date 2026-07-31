# vm-allocation-parity

## Why

With the gate's cell tiers corrected against the checked-in classification
profile (`gate-tier-reclassification`, profile `30614184386` at head
`4607f1e`), ten of thirteen `Goldset/*` cells pass. The three that fail all
fail on the same axis — allocated bytes — under the tier that describes them
honestly, and no tier assignment makes them pass:

| cell | tier | latency | bytes | why it fails |
| --- | --- | --- | --- | --- |
| `Goldset/guard-nil` | data-dominated | +2.65% | **+19.40%** | the VM allocates more than the Evaluator |
| `Goldset/kw-lookup` | engine-sensitive | −20.00% | −9.04% | short of ADR 0008's 20% floor |
| `Goldset/merge-config` | engine-sensitive | −20.59% | −19.96% | short of the same floor by 0.04 points |

These are findings about the engine, not about `internal/perfgate/tiers.json`.
The reclassification could not resolve them and deliberately did not try: a
tier that passes a cell by describing it falsely is worth less than a failing
verdict that is true.

The consequence is concrete. `release-gate-activation` tasks 1.2, 4.2, and 5.1
each wait on a release whose gate passes, because only a passing gate reaches
`release.yml`'s "Store VM baseline on the authorized release" step. While these
three cells fail, cutting a release skips that step exactly as `v0.11.0` did,
and the project still carries no stored non-regression baseline.

A second finding surfaced while checking whether any tier could take the three
cells: `evaluateStartup` in `internal/perfgate/perfgate.go` never applies the
bytes or allocation-count bound at all, and every cell in this corpus clears
its 1 ms / 256 KiB absolute overhead bound. Any cell classified `startup`
therefore passes the gate regardless of how much it allocates. Only
`Goldset/rule-load` carries that tier today and it passes on its own merits, so
nothing is currently mis-gated — but the hole is exactly the shape of an
accidental green verdict, and it sits next to a floor three cells are failing.

ADR 0008 states the startup tier as "within 5%, or at most 1 ms and 256 KiB
absolute overhead", with the section's opening rule that no cell may regress
beyond its tier's budget; whether the bytes and allocation-count bound the
other three tiers carry was meant to apply here as well is the question this
change has to settle before changing the code.

## What Changes

- Profile where the VM's extra allocation on `Goldset/guard-nil` comes from.
  The cell's latency is mode-invariant (+2.65%) while its bytes are not
  (+19.40%, 1160 B/op under the Evaluator against 1385 under the VM), so the
  cost is structural rather than a hot-path difference. Either reduce it below
  the Evaluator's, or record with evidence why the VM cannot match the
  Evaluator on this shape and what the gate should assert instead.
- Do the same for `Goldset/kw-lookup` (bytes −9.04%) and `Goldset/merge-config`
  (bytes −19.96%). Both improve latency by 20% or more, so their tier is not in
  question; what is in question is whether the allocation half of the
  engine-sensitive floor is reachable for them. `merge-config` misses by 0.04
  points and a sub-1% allocation win on the VM path flips it.
- Settle whether the `startup` tier should carry the bytes and
  allocation-count non-increase bound the other tiers carry, and make
  `evaluateStartup` match the answer. If ADR 0008 intends the absolute
  overhead bound to stand alone, say so in the ADR and in the tier comment
  rather than leaving the reader to infer it from the code.
- Produce a fresh classification profile once the allocation work lands, per
  `consumer-release-gate`'s rule that a profile is checked in before the
  release whose candidate results its tiers judge. The existing profile
  measures a tree without these fixes.
- Do not adjust any cell's tier to buy a passing verdict. The tiers are
  licensed by a committed profile and describe measured shapes; this change
  moves the engine, not the labels.

## Impact

- Affected specs: `consumer-release-gate` (the startup tier's bound made
  explicit, whichever way it is settled).
- Affected code: `core/vm` and whatever allocation site the profiling
  implicates, `internal/perfgate/perfgate.go` for the startup tier,
  `internal/perfgate/testdata/` for the replacement profile.
- Unblocks: `release-gate-activation` 1.2, 4.2, and 5.1 become reachable once a
  release is cut whose gate passes. The cut is not this change's to make.
- Risk: the honest outcome may be that one or more cells cannot reach the
  floor, in which case this change ends by amending ADR 0008's threshold with
  measured justification rather than by changing the engine. That is a
  legitimate result and is not the same as relabelling the cell.
- Sequencing: after `gate-tier-reclassification`, which produced the profile
  and the finding. Before any release that expects to store a baseline.

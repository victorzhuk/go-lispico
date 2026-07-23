## Context

The metering program deliberately charged `GoFunc` results shallowly at the two
apply sites, to avoid per-plugin edits and to keep the hot path cheap
(`ValueShallowBytes` is O(1) for scalars and one-level for containers). That is
correct for the overwhelming majority of builtins, which return a scalar or a
small container proportional to their input. It is wrong only for the handful
that amplify — small input, large constructed output — because the shallow,
post-hoc charge either under-counts the structure (`json/decode`) or fires only
after the allocation already happened (`format`).

## Why not deep-charge every result

Making the generic apply-site charge deep (`ValueDeepBytes` on every result)
adds a recursive size walk to every builtin call, including the vast majority
that return scalars — a hot-path tax to fix a few outliers. Keep the generic
shallow charge; add explicit eager charging only where amplification is
possible. `core.ChargeEvalAllocBytes(ctx, n)` and `core.ValueDeepBytes` are
already exported for exactly this.

## json/decode

Charge `core.ValueDeepBytes(result)` after `fromJSONValue` builds the tree and
before returning. The deep walk is O(result), which is bounded by the O(input)
work `json.Unmarshal` + `fromJSONValue` already did — no new complexity class.
The result is charged for what it actually occupies, so the ledger rejects an
over-budget decode.

- Alternative considered: thread `ctx` through `fromJSONValue` and charge per
  node, failing closed mid-build. More precise (stops before the whole tree
  exists) but touches the recursion and complicates the immutability-safe
  builder. Recommend the single post-build deep charge first; escalate to
  incremental only if a single decode can exceed the budget by enough to matter
  before the deep charge fires.

## format

Parse the format string's `%`-verb width/precision specifiers, sum an
upper-bound output estimate, and `ChargeEvalAllocBytes` that estimate before
`fmt.Sprintf`. Over budget → return a `ResourceLimitError` without building the
string. This charges before the amplifying allocation, not after.

- Simpler fallback if precise estimation is fiddly: cap the total of
  width/precision specifiers (or the verb count) at a documented bound and
  reject beyond it. Recommend estimate-and-charge so the single knob stays
  `MaxAllocationBytes`, not a new format-specific limit.

## Determinism

`ValueDeepBytes` uses the fixed arch-independent size table the metering program
established (never `unsafe.Sizeof`), so charges are reproducible across
platforms — the ledger stays deterministic, consistent with the existing
metering requirements.

## Audit list (same change)

- `str`/string concatenation builders — output is sum of inputs, not
  amplifying; shallow charge of the result string (its real length) already
  accounts for it. Confirm, no change expected.
- Any `repeat`/`make-string`/count-driven collection constructor — small
  numeric input, large output. These are the format-class case: charge the
  computed output size before building.
- `range` — already bounded by `MaxCollectionLen` before allocation
  (stdlib-plugin spec "range is bounded and cancellable"); confirm no gap.

## Non-goals

- Changing the generic shallow result charge for non-amplifying builtins.
- Adding a new `ResourceLimits` field — the existing `MaxAllocationBytes` is the
  one knob.

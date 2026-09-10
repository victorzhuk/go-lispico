## Context

See proposal.md — Why. This section records only what the measurement added.

The gap was measured on the current tree (commit `e45c916`) with a throwaway probe
in `package core`, since `trieFromBuildMap` and the two large forms are
unexported. One `Assoc` against a retained receiver, receiver built by `Set`
(builder form) versus by repeated `Assoc` (trie form):

| n | charge, builder | charge, trie | allocs/op, builder | allocs/op, trie | B/op, builder | B/op, trie |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 9 | 3 792 | 696 | 37 | 7 | 8 183 | 1 481 |
| 100 | 75 440 | 624 | 554 | 8 | 126 018 | 891 |
| 1 000 | 1 085 048 | 1 600 | 6 480 | 10 | 1 794 146 | 2 325 |

Benchmarks at `-benchtime=500x -benchmem`. Allocations and bytes are the verdict
axis and are exact on this machine; the timing followed the same shape (354 ns
versus 4.2 µs at n=9, 681 ns versus 506 µs at n=1000) but a latency claim is not
decidable here and is recorded as observation only.

The trie arm is flat — 7 to 10 allocations across two orders of magnitude, which
is the bound the requirement states. The builder arm rises linearly with entry
count: at n=1000 a single update allocates 6 480 objects and charges 1.06 MB.
Against `DefaultMaxAllocationBytes` (64 MiB) that is **62 updates before one
evaluation's entire allocation budget is gone**, for storage no caller retains.
The failure mode is therefore not only slowness: a Lisp program doing fan-out
`assoc` against a large map literal can take a `ResourceLimitError` it did nothing
to deserve.

## Goals / Non-Goals

**Goals:**

- Repeated updates against one bulk-built receiver charge and allocate per update
  on the same order as the trie form already does.
- The conversion is paid once per map value, the way promotion past
  `hashMapSmallLimit` is already paid once.
- The gap stays measured: a committed benchmark covers both arms at sizes spanning
  two orders of magnitude.

**Non-Goals:**

- Changing which representation any builder produces. Reader and `Set` both keep
  landing in builder form; the parity requirement that landed with
  `reader-map-promotion-parity` is not reopened.
- Touching `getByHashKey`, `Len`, iteration order, equality or printing.
- Making the *first* update on a bulk-built map O(depth). Building a trie from n
  entries is inherently O(n); the goal is that it happens once, not that it stops
  happening.

## Decisions

### Memoise the converted trie on the receiver, rather than making the conversion cheaper

Building the trie in one bottom-up pass instead of n path-copying `assoc` calls
was the obvious contained alternative, and it is not enough. It removes the
`ceil(log32 n)` factor and the throwaway paths, so n=1000 would fall from ~6 480
allocations to roughly the ~65 nodes the finished trie needs — a large constant
win, but still O(n) *per update*. The measured shape stays linear, the budget
hazard survives at larger n, and the requirement's bound is still not met. It
would be optimising the symptom.

Memoisation is what changes the shape: the first update on a value pays O(n), and
every later update against that same receiver pays O(depth), which is the trie
arm already measured. Fan-out of k updates goes from O(k·n) to O(n + k·depth).

The one-pass build stays available as a later, independent improvement to the
once-per-value cost. It is not in this change.

### The memo lives beside `large.m`, not in `large.root`

`large.root` is not a cache slot. `getByHashKey` tests `root` before `m`, and
`Len()` reads `large.count` — zero for a builder-form map — so writing `root`
would silently move the read path onto the trie and break `Len`. The memo is a
separate field on `largeMap`, written and read only by `Assoc` and `Dissoc`.
`large.m` stays authoritative for every read, for `Len`, and for iteration.

Consequence worth stating: the two large forms remain mutually exclusive as the
spec describes them. A memoised map is still a builder-form map; it merely knows
what its trie would be.

### Publication is atomic, and a lost race is discarded

Two goroutines may reach the conversion at once. The memo is an
`atomic.Pointer`-style slot: each converts, one compare-and-swap wins, the loser
drops its copy and proceeds with its own root. Both allocated, so both charge —
that is honest accounting, not a leak. The fixed-seed hash makes the two tries
structurally identical, so which one wins is not observable.

This is why a plain field plus a mutex is not proposed: reads of the memo sit on
the update path of an immutable value that callers are entitled to share across
goroutines, and ADR 0003 already forbids introducing a lock there.

### The conversion is charged once per value, and that is a metering change

Today every update re-charges the conversion. After this change the first update
charges it and later ones do not, which is a truer statement of what was
allocated. Two consequences are real and must be written down rather than
discovered:

- Exact-charge tests that assoc twice against one receiver will see different
  totals for the two calls. They are pinning a defect today.
- When a map value is shared across evaluations, whichever evaluation updates it
  first bears the conversion. Today every evaluation bears it. First-toucher-pays
  is the same rule already used for other lazily obtained storage, but ADR 0011
  must say so, because "the same source charges the same total" no longer holds
  for a *shared receiver* the way it holds for a source read.

Recorded as a decision rather than an open question because it changes ADR 0011's
text and the task breakdown.

### Immutability gets an explicit carve-out, not a silent exception

`CLAUDE.md` lists immutable data structures as a kernel invariant and
`trieFromBuildMap`'s own comment promises the receiver is untouched. A memo
mutates a field of a shared object. The carve-out is that the memo is derived
state: it is a pure function of `large.m`, which never changes after bulk
construction ends, and no observable — value, equality, iteration order, printing,
`Len` — differs before or after it is populated. ADR 0003 states the carve-out in
those terms, so the invariant keeps meaning something.

The narrow claim this rests on: `Set` is documented as the bulk-construction
escape hatch used "while nothing is shared yet". A `Set` after a memo is populated
would invalidate it. Implementation clears the memo on `Set`, which is one store
on a path that is already mutating.

## Risks / Trade-offs

- **A `Set` call after the memo is populated leaves a stale trie** → `Set` clears
  the memo. Cheap, and the case is already outside the documented contract.
- **First-toucher-pays makes one evaluation's charge depend on another's
  timing** → stated in ADR 0011 and pinned by a test that shares a map across two
  meters and asserts exactly one of them is charged the conversion.
- **The memo keeps a trie alive for a map that is never updated again** → it is
  only ever populated by an update, so a read-only map never grows one. A map
  updated once and then retained holds both forms; that is the same trade the
  trie form already makes, bounded by the map's own size.
- **The benchmark becomes a flaky gate if it asserts timing** → it asserts
  allocations and bytes, which are exact here; timing is reported, never gated.
  This follows the same rule ADR 0008's gate already uses.
- **Regression risk to the metering tests is broad** → the change is sequenced so
  the charge tests are amended in the same step that changes the charge, the way
  `reader-map-promotion-parity` sequenced its own charge change.

## Migration Plan

No data or API migration. The change is additive to `largeMap` and invisible to
embedders except through the charge, which is recorded in `CHANGELOG.md` under
`[Unreleased]` as a metering change, with the measured before and after from the
table above.

Rollback is deleting the memo field and its two call sites; nothing persists it
and no format depends on it.

## Open Questions

None. The two questions that were open at proposal time — whether amortisation is
worth its cost, and which mechanism to use — are answered above by the
measurement.

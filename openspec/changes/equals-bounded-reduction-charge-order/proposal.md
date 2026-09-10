## Why

`equalsBounded` charges a different number of reduction units for the same pair of
values depending on how Go happened to order a map range.

`core/equals_bounded.go`, the `*HashMap` arm, walks the receiver with `eachRaw` and
short-circuits inside the callback:

```go
av.eachRaw(func(e entry) {
	if !equal || walkErr != nil {
		return
	}
	other, found := bv.getByHashKey(e.hk)
	...
	eq, err := equalsBounded(e.v, other, budget, depth+1)
```

Every surviving entry recurses, and every recursion charges `budget.Step()`. Once a
mismatch is found the callback returns immediately for the remaining entries, so the
number of nested charges is decided by *where in the walk the mismatch fell*. For a
receiver in builder form — `h.large != nil && h.large.root == nil`, the state a map
built by a literal, `hash-map`, `merge` or `json/decode` sits in until its first
`Assoc` — `eachRaw` ranges the Go map `h.large.m`, and Go randomises that order per
range. Comparing two unequal maps of n entries therefore charges anywhere between 0
and n-1 nested reductions for identical input.

The trie form is unaffected: `eachRaw` takes `h.large.root.each(fn)` there, which is a
deterministic trie walk. So is the equal case, where every entry is visited whatever
the order. The defect is exactly: unequal maps, receiver in builder form.

This contradicts ADR 0011, which states the reduction ledger is deterministic by
construction, and it is reachable from Lisp as `(= m1 m2)` over a map above
`hashMapSmallLimit` that has not yet been updated. A reduction budget is a denial-of-
service bound, so an embedder sizing one against observed usage is sizing against a
number that moves.

Found while planning `trie-conversion-charge-determinism`, whose task 0.1 asserted
that no `eachRaw` caller charges by traversal order. That is true of the allocation
ledger and false of this one; that change narrowed its claim and left this here.

## What Changes

- Make the reduction units `equalsBounded` charges for a pair of values a function of
  the pair, not of the order the receiver's entries were visited in.
- Pin it with a test that compares two unequal maps whose receiver is in builder form
  and requires the charged reduction count to be identical across repeats.
- State the reduction ledger's order-independence as a requirement, next to the
  allocation-side property `trie-conversion-charge-determinism` adds.

Mechanism is not fixed here. At least two shapes exist: visit in a data-derived order
so the mismatch is always found at the same point, or charge the comparison up front
from the sizes so the count does not depend on where the walk stopped. They differ in
whether the early exit survives, which is a performance property worth measuring
before choosing. The design stage picks one.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: a reduction charge is reproducible for the same input, and bounded
  equality over hash maps meets that.

## Impact

Affected code: `core/equals_bounded.go` (`equalsBounded`, the `*HashMap` arm), plus a
test in `core`. `core/depth.go`'s `boundedEquals` walks the same way but takes no
budget and charges nothing, so it is out of scope.

The reduction count charged for an unequal-map comparison will change value, because a
reproducible count cannot also be the current unstable one. Whether it moves up or
down depends on the mechanism: preserving the early exit keeps the count low,
charging up front raises it to n. That is an ADR 0011 note and a `CHANGELOG.md` entry.

Depends on nothing. Does not block `trie-conversion-charge-determinism`, which fixes
the allocation ledger on a different function.

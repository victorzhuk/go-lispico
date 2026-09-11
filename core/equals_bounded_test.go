package core

import (
	"context"
	"strconv"
	"testing"
)

// equalsNodeList builds an n-element List of distinct Ints. EqualsBounded
// charges the List node itself plus one unit per element, so comparing two of
// these costs n+1 units.
func equalsNodeList(n int) List {
	vs := make([]Value, n)
	for i := range vs {
		vs[i] = Int{V: int64(i)}
	}
	return NewList(vs)
}

// hostEqValue is an arbitrary host Value. EqualsBounded reaches it through the
// default branch, which is a trusted-host boundary: whatever Equals does
// inside is not ours to charge.
type hostEqValue struct {
	eq    bool
	calls *int
}

func (h hostEqValue) Type() Keyword  { return Keyword{V: "host-eq"} }
func (h hostEqValue) String() string { return "#<host-eq>" }

func (h hostEqValue) Equals(Value) bool {
	*h.calls++
	return h.eq
}

// TestEqualsBounded_StepsPerComparedNode pins the charge rate at exactly one
// unit per compared node: two equal 200-element Lists cost 201 units, so a
// 100-reduction ceiling makes the comparison terminal mid-walk while a
// generous ceiling lets it run to (true, nil).
//
// Green on arrival: EqualsBounded already ships. This is the contract that
// holds its behaviour still while the numeric family migrates onto it.
func TestEqualsBounded_StepsPerComparedNode(t *testing.T) {
	t.Parallel()
	const nodes = 200

	t.Run("terminalUnderReductionCeiling", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 100))
		eq, err := EqualsBounded(equalsNodeList(nodes), equalsNodeList(nodes), b)
		if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
			t.Fatalf("EqualsBounded over two equal %d-element Lists under a 100-reduction ceiling: want terminal %s, got (%v, %v)", nodes, CodeResourceLimit, eq, err)
		}
	})

	t.Run("completesUnderGenerousCeiling", func(t *testing.T) {
		t.Parallel()
		ctx := budgetCtx(context.Background(), 1_000_000)
		b := NewBuiltinWorkBudget(ctx)
		eq, err := EqualsBounded(equalsNodeList(nodes), equalsNodeList(nodes), b)
		if err != nil || !eq {
			t.Fatalf("EqualsBounded over two equal %d-element Lists under a 1_000_000-reduction ceiling: want (true, nil), got (%v, %v)", nodes, eq, err)
		}
		if err := b.Flush(); err != nil {
			t.Fatalf("Flush after a completed comparison: %v", err)
		}
		if got := EvalMeterFrom(ctx).Snapshot().Reductions; got != nodes+1 {
			t.Fatalf("reductions charged comparing two equal %d-element Lists = %d, want %d: one unit per compared node, the List itself plus each element", nodes, got, nodes+1)
		}
	})
}

// nestedList (core/depth_test.go) wraps an Int in depth layers of List, so the
// innermost scalar is the node boundedEquals reaches at exactly that depth.
//
// TestEqualsBounded_MatchesEqualsAtDepthLimit pins EqualsBounded to the same
// structural depth cap Value.Equals enforces through boundedEquals. Without
// one, = flips false to true past DefaultMaxStructuralDepth and stops agreeing
// with core's own equality on the same pair, and the only remaining backstop is
// the reduction ceiling — millions of Go stack frames away.
//
// The expectation is read from Equals rather than hardcoded, so the test tracks
// the cap instead of duplicating it.
func TestEqualsBounded_MatchesEqualsAtDepthLimit(t *testing.T) {
	t.Parallel()

	atCap, pastCap := DefaultMaxStructuralDepth, DefaultMaxStructuralDepth+1
	if !nestedList(atCap).Equals(nestedList(atCap)) || nestedList(pastCap).Equals(nestedList(pastCap)) {
		t.Fatalf("depths %d and %d no longer straddle the Equals depth cap: the parity check below would prove nothing", atCap, pastCap)
	}

	for _, tt := range []struct {
		name  string
		depth int
	}{
		{"atCap", atCap},
		{"pastCap", pastCap},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, b := nestedList(tt.depth), nestedList(tt.depth)
			want := a.Equals(b)
			budget := NewBuiltinWorkBudget(budgetCtx(context.Background(), DefaultMaxReductions))
			got, err := EqualsBounded(a, b, budget)
			if err != nil {
				t.Fatalf("EqualsBounded at nesting depth %d: unexpected error %v", tt.depth, err)
			}
			if got != want {
				t.Fatalf("EqualsBounded at nesting depth %d = %v, want %v: it must honour the same structural depth cap Value.Equals enforces", tt.depth, got, want)
			}
		})
	}
}

// TestEqualsBounded_HostValueNotStepped pins the trusted-host boundary: the
// default branch charges the entry unit for the node and nothing for whatever
// the host's own Equals walks inside, and reports that Equals result unchanged.
//
// Green on arrival: EqualsBounded already ships with the unstepped default
// branch. This pins it so a later budget migration cannot start charging host
// work to our ledger.
func TestEqualsBounded_HostValueNotStepped(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		want bool
	}{
		{"equal", true},
		{"unequal", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := tt.want
			calls := 0
			b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1_000_000))
			got, err := EqualsBounded(hostEqValue{eq: want, calls: &calls}, Int{V: 1}, b)
			if err != nil {
				t.Fatalf("EqualsBounded over a host Value: unexpected error %v", err)
			}
			if got != want {
				t.Fatalf("EqualsBounded over a host Value = %v, want %v: the host's own Equals result must be reported unchanged", got, want)
			}
			if calls != 1 {
				t.Fatalf("host Equals called %d times, want exactly 1", calls)
			}
			if b.pending != 1 {
				t.Fatalf("units charged for a host Value = %d, want exactly 1: the node itself is stepped, its interior is not", b.pending)
			}
		})
	}
}

// TestEqualsBounded_ReturnsBudgetErrorUnchanged pins that the budget's error
// leaves EqualsBounded by identity, not merely by errors.Is: settling it
// against a builtin's own error precedence is the caller's job, and a wrapped
// copy would read as non-terminal there. A second call after the latch replays
// that identical value.
//
// Green on arrival: EqualsBounded already returns the budget error unchanged.
// This pins the identity so the numeric migration cannot start rewrapping it.
func TestEqualsBounded_ReturnsBudgetErrorUnchanged(t *testing.T) {
	t.Parallel()
	const nodes = 200

	b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 100))
	_, first := EqualsBounded(equalsNodeList(nodes), equalsNodeList(nodes), b)
	if first == nil {
		t.Fatalf("EqualsBounded over two equal %d-element Lists under a 100-reduction ceiling: want the budget's terminal error, got nil", nodes)
	}
	if first != b.latched {
		t.Fatalf("returned error %v is not the budget's latched value %v: EqualsBounded must return it unchanged, by identity", first, b.latched)
	}

	_, second := EqualsBounded(equalsNodeList(nodes), equalsNodeList(nodes), b)
	if second != first {
		t.Fatalf("second EqualsBounded after the latch returned %v, want the identical value %v", second, first)
	}
}

// builderMapOf builds an n-entry map in builder form — staging map populated,
// trie root still nil — from independent key and value functions, so two
// fixtures can differ at a chosen key without one Set corrupting the other's
// receiver.
func builderMapOf(t *testing.T, n int, key, val func(i int64) Value) *HashMap {
	t.Helper()
	if n <= hashMapSmallLimit {
		t.Fatalf("n = %d does not exceed hashMapSmallLimit (%d), builder form unreachable", n, hashMapSmallLimit)
	}
	m := NewHashMap()
	for i := range int64(n) {
		if err := m.Set(key(i), val(i)); err != nil {
			t.Fatal(err)
		}
	}
	assertBuilderForm(t, m, n)
	return m
}

// trieMapOf is builderMapOf's trie-form twin: Assoc alone never converts, so the
// receiver walks hamt nodes instead of a Go map.
func trieMapOf(t *testing.T, n int, key, val func(i int64) Value) *HashMap {
	t.Helper()
	if n <= hashMapSmallLimit {
		t.Fatalf("n = %d does not exceed hashMapSmallLimit (%d), trie form unreachable", n, hashMapSmallLimit)
	}
	m := NewHashMap()
	for i := range int64(n) {
		var err error
		m, _, err = m.Assoc(key(i), val(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	assertTrieForm(t, m)
	return m
}

// chargeRepeats compares each pair this many times. The builder form ranges a Go
// map, so a run where every repeat happens to visit the mismatching entry last
// has probability (1/n)^8 — 4.6e-8 at the smallest size tested.
const chargeRepeats = 8

// TestEqualsBounded_ReductionChargeIgnoresIterationOrder pins the charged total
// to the compared pair alone. The builder form ranges a Go map, whose iteration
// order is randomised per range, so a walk that returns at the first mismatch
// bills the position that mismatch happened to fall at: the same pair costs a
// different amount on every comparison, and a wholly absent key costs nothing.
// A caller cannot budget against that. The total must be a sum over the
// receiver's entries — one unit for an absent key, units(value) for a present
// one — which is exactly n+1 for a flat n-entry pair of scalar-valued maps, in
// every storage form and whatever order the entries are visited in.
func TestEqualsBounded_ReductionChargeIgnoresIterationOrder(t *testing.T) {
	t.Parallel()

	ident := func(i int64) Value { return Int{V: i} }

	for _, n := range []int{9, 100, 1000} {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			want := int64(n + 1)
			chargesFor := func(t *testing.T, name string, a, b *HashMap, wantEqual bool) int64 {
				t.Helper()
				charges := make([]int64, 0, chargeRepeats)
				for range chargeRepeats {
					ctx := budgetCtx(context.Background(), DefaultMaxReductions)
					budget := NewBuiltinWorkBudget(ctx)
					got, err := EqualsBounded(a, b, budget)
					if err != nil {
						t.Fatalf("%s n=%d: EqualsBounded returned unexpected error %v", name, n, err)
					}
					if got != wantEqual {
						t.Fatalf("%s n=%d: EqualsBounded = %v, want %v", name, n, got, wantEqual)
					}
					if err := budget.Flush(); err != nil {
						t.Fatalf("%s n=%d: Flush after a completed comparison: %v", name, n, err)
					}
					charges = append(charges, EvalMeterFrom(ctx).Snapshot().Reductions)
				}
				t.Logf("%s n=%d: reductions per repeat = %v", name, n, charges)
				for _, c := range charges {
					if c != want || c != charges[0] {
						t.Fatalf("%s n=%d: reductions per repeat = %v, want every repeat to charge exactly %d: the total is a sum over the receiver's entries, so it cannot vary with the order eachRaw visits them in", name, n, charges, want)
					}
				}
				return charges[0]
			}

			a := builderMapOf(t, n, ident, ident)
			bMismatch := builderMapOf(t, n, ident, func(i int64) Value {
				if i == 0 {
					return Int{V: -1}
				}
				return Int{V: i}
			})

			var valueMismatch, trieReceiver int64
			mismatchOK := t.Run("valueMismatch", func(t *testing.T) {
				valueMismatch = chargesFor(t, "valueMismatch", a, bMismatch, false)
			})
			t.Run("disjointKeys", func(t *testing.T) {
				b := builderMapOf(t, n, func(i int64) Value { return Int{V: int64(n) + i} }, ident)
				chargesFor(t, "disjointKeys", a, b, false)
			})
			t.Run("equalMaps", func(t *testing.T) {
				chargesFor(t, "equalMaps", a, builderMapOf(t, n, ident, ident), true)
			})
			trieOK := t.Run("trieReceiver", func(t *testing.T) {
				trieReceiver = chargesFor(t, "trieReceiver", trieMapOf(t, n, ident, ident), bMismatch, false)
			})
			if mismatchOK && trieOK && trieReceiver != valueMismatch {
				t.Fatalf("n=%d: a trie receiver charged %d and a builder receiver charged %d for the same contents, want the same total: the charge must not read the storage form", n, trieReceiver, valueMismatch)
			}
		})
	}

	// Green on arrival: the early return already stops at the first mismatch, so
	// this answers false today. It guards the rewrite that removes it — with the
	// walk exhaustive, a surviving bare `equal = eq` would let the four matching
	// entries overwrite the mismatch and resurrect true. Small-form entries are
	// held sorted by hashKey, so the differing key is visited first on every run,
	// which makes the guard deterministic rather than the ~4.6e-8 the size loop
	// leaves. This pair's own charge is 6, not n+1, so it asserts no total.
	t.Run("smallFormMismatchFirst", func(t *testing.T) {
		const keys = 5
		a, b := NewHashMap(), NewHashMap()
		for i := range int64(keys) {
			if err := a.Set(Int{V: i}, Int{V: i}); err != nil {
				t.Fatal(err)
			}
			v := Int{V: i}
			if i == 0 {
				v = Int{V: -1}
			}
			if err := b.Set(Int{V: i}, v); err != nil {
				t.Fatal(err)
			}
		}
		if a.large != nil || b.large != nil {
			t.Fatalf("%d-key fixtures are %s and %s form, want small form so the mismatch is visited first", keys, mapForm(a), mapForm(b))
		}

		budget := NewBuiltinWorkBudget(budgetCtx(context.Background(), DefaultMaxReductions))
		got, err := EqualsBounded(a, b, budget)
		if err != nil {
			t.Fatalf("EqualsBounded over a %d-key small-form pair: unexpected error %v", keys, err)
		}
		if got {
			t.Fatalf("EqualsBounded over a %d-key small-form pair differing at the first-visited key = true, want false: a later matching entry must never overwrite a mismatch already found", keys)
		}
	})
}

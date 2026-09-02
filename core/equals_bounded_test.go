package core

import (
	"context"
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

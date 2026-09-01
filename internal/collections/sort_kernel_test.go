package collections

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// naturalLess wraps NaturalCmp as a SortLessFunc.
func naturalLessForSort(a, b core.Value) (bool, error) {
	c, err := NaturalCmp(a, b)
	if err != nil {
		return false, err
	}
	return c < 0, nil
}

// valuesEqual returns true when a and b hold the same dynamic Values in
// order. It compares element-wise via core.Value.Equals so that struct-
// shaped core.Int / core.String / core.Keyword values compare by content
// without leaking interface identity.
func valuesEqual(a, b []core.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equals(b[i]) {
			return false
		}
	}
	return true
}

// shuffledIntItems returns a deterministic permutation of [1..n] as
// core.Int values, using the multiplicative permutation (i*step+off)
// mod n. gcd(step, n) must be 1 so the map is a permutation.
func shuffledIntItems(n int) []core.Value {
	out := make([]core.Value, n)
	for i := range out {
		v := ((i * 31) + 7) % n
		out[i] = core.Int{V: int64(v + 1)}
	}
	return out
}

// sortedAscendingInt reports whether items holds core.Int values in
// strictly ascending order 1..len(items).
func sortedAscendingInt(items []core.Value) error {
	for i, v := range items {
		iv, ok := v.(core.Int)
		if !ok {
			return fmt.Errorf("index %d: want core.Int, got %T", i, v)
		}
		if iv.V != int64(i+1) {
			return fmt.Errorf("index %d: want %d, got %d", i, i+1, iv.V)
		}
	}
	return nil
}

// TestStableSort_NaturalOrderAndMixedKind exercises the kernel directly
// with a NaturalCmp-wrapping less and key=nil: ints, strings, and
// keywords sort ascending; the caller's input slice retains its original
// order (the kernel sorts an internal copy); and a mixed-kind input
// surfaces a non-terminal "sort: ..." error.
func TestStableSort_NaturalOrderAndMixedKind(t *testing.T) {
	t.Parallel()

	t.Run("ints", func(t *testing.T) {
		t.Parallel()
		items := []core.Value{core.Int{V: 3}, core.Int{V: 1}, core.Int{V: 2}}
		out, err := StableSort(context.Background(), items, nil, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort: unexpected error: %v", err)
		}
		if !valuesEqual(out, []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}) {
			t.Fatalf("StableSort ints: want [1 2 3], got %v", out)
		}
		// Caller's input slice backing array must be unchanged.
		if !valuesEqual(items, []core.Value{core.Int{V: 3}, core.Int{V: 1}, core.Int{V: 2}}) {
			t.Fatalf("StableSort ints: input slice mutated, got %v", items)
		}
	})

	t.Run("strings", func(t *testing.T) {
		t.Parallel()
		items := []core.Value{core.String{V: "pear"}, core.String{V: "apple"}}
		out, err := StableSort(context.Background(), items, nil, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort: unexpected error: %v", err)
		}
		if !valuesEqual(out, []core.Value{core.String{V: "apple"}, core.String{V: "pear"}}) {
			t.Fatalf("StableSort strings: want [apple pear], got %v", out)
		}
		if !valuesEqual(items, []core.Value{core.String{V: "pear"}, core.String{V: "apple"}}) {
			t.Fatalf("StableSort strings: input slice mutated, got %v", items)
		}
	})

	t.Run("keywords", func(t *testing.T) {
		t.Parallel()
		items := []core.Value{core.Keyword{V: "z"}, core.Keyword{V: "a"}}
		out, err := StableSort(context.Background(), items, nil, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort: unexpected error: %v", err)
		}
		if !valuesEqual(out, []core.Value{core.Keyword{V: "a"}, core.Keyword{V: "z"}}) {
			t.Fatalf("StableSort keywords: want [a z], got %v", out)
		}
		if !valuesEqual(items, []core.Value{core.Keyword{V: "z"}, core.Keyword{V: "a"}}) {
			t.Fatalf("StableSort keywords: input slice mutated, got %v", items)
		}
	})

	t.Run("mixed-kind", func(t *testing.T) {
		t.Parallel()
		items := []core.Value{core.String{V: "x"}, core.Int{V: 1}}
		out, err := StableSort(context.Background(), items, nil, naturalLessForSort)
		if err == nil {
			t.Fatalf("StableSort mixed-kind: want error, got nil result %v", out)
		}
		var le *core.LispicoError
		if !errors.As(err, &le) {
			t.Fatalf("StableSort mixed-kind: want *core.LispicoError, got %T: %v", err, err)
		}
		if !strings.HasPrefix(le.Message, "sort: ") {
			t.Fatalf("StableSort mixed-kind: want message prefix %q, got %q", "sort: ", le.Message)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("StableSort mixed-kind: want non-terminal, got terminal %v", err)
		}
	})
}

// TestStableSort_KeyProjectedOnceOriginalOrder proves the key-projection
// phase calls each closure exactly once per element, in original input
// order, and that the comparator sees the projected core.Int keys (never
// the tagged originals). Equal keys preserve original order (stability).
// A key=nil run produces the same output as the identity-key run.
func TestStableSort_KeyProjectedOnceOriginalOrder(t *testing.T) {
	t.Parallel()

	// Tagged items with an encoded sort key in the keyword name ("2:a" -> key 2).
	tagged := []core.Value{
		core.Keyword{V: "2:a"},
		core.Keyword{V: "1:b"},
		core.Keyword{V: "2:c"},
	}
	expected := []core.Value{
		core.Keyword{V: "1:b"},
		core.Keyword{V: "2:a"},
		core.Keyword{V: "2:c"},
	}

	keyFor := func(v core.Value) (core.Value, error) {
		k := v.(core.Keyword).V
		colon := strings.IndexByte(k, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("malformed tag %q", k)
		}
		var n int64
		if _, err := fmt.Sscanf(k[:colon], "%d", &n); err != nil {
			return nil, fmt.Errorf("malformed key in %q: %w", k, err)
		}
		return core.Int{V: n}, nil
	}

	t.Run("tagged-run", func(t *testing.T) {
		t.Parallel()

		var keyOrder []core.Value
		var lessPairs [][2]core.Value
		var lessTypeMismatch bool

		key := func(v core.Value) (core.Value, error) {
			keyOrder = append(keyOrder, v)
			return keyFor(v)
		}
		less := func(a, b core.Value) (bool, error) {
			lessPairs = append(lessPairs, [2]core.Value{a, b})
			if _, ok := a.(core.Int); !ok {
				lessTypeMismatch = true
			}
			if _, ok := b.(core.Int); !ok {
				lessTypeMismatch = true
			}
			ai, bi := a.(core.Int).V, b.(core.Int).V
			return ai < bi, nil
		}

		out, err := StableSort(context.Background(), tagged, key, less)
		if err != nil {
			t.Fatalf("StableSort tagged: unexpected error: %v", err)
		}
		if !valuesEqual(out, expected) {
			t.Fatalf("StableSort tagged: want %v, got %v", expected, out)
		}
		if n := len(keyOrder); n != len(tagged) {
			t.Fatalf("key invocation count: want %d, got %d", len(tagged), n)
		}
		if !valuesEqual(keyOrder, tagged) {
			t.Fatalf("key invocation order: want original input order %v, got %v", tagged, keyOrder)
		}
		if lessTypeMismatch {
			t.Fatalf("comparator received non-Int (tagged) argument: pairs=%v", lessPairs)
		}
		if len(lessPairs) == 0 {
			t.Fatalf("comparator was never invoked")
		}
		for _, p := range lessPairs {
			if _, ok := p[0].(core.Int); !ok {
				t.Fatalf("comparator argument 0: want core.Int (projected), got %T", p[0])
			}
			if _, ok := p[1].(core.Int); !ok {
				t.Fatalf("comparator argument 1: want core.Int (projected), got %T", p[1])
			}
		}
	})

	t.Run("identity-key-run", func(t *testing.T) {
		t.Parallel()
		var keyCalls int
		key := func(v core.Value) (core.Value, error) {
			keyCalls++
			return v, nil
		}
		out, err := StableSort(context.Background(), tagged, key, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort identity-key: unexpected error: %v", err)
		}
		if keyCalls != len(tagged) {
			t.Fatalf("identity-key invocations: want %d, got %d", len(tagged), keyCalls)
		}
		if !valuesEqual(out, expected) {
			t.Fatalf("StableSort identity-key: want %v, got %v", expected, out)
		}
	})

	t.Run("key-nil-run", func(t *testing.T) {
		t.Parallel()
		// key=nil -> no key projection closure; with naturalLess the output
		// is the lexicographic ascending order of the keyword names.
		out, err := StableSort(context.Background(), tagged, nil, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort key-nil: unexpected error: %v", err)
		}
		wantKeyNil := []core.Value{
			core.Keyword{V: "1:b"},
			core.Keyword{V: "2:a"},
			core.Keyword{V: "2:c"},
		}
		if !valuesEqual(out, wantKeyNil) {
			t.Fatalf("StableSort key-nil: want %v, got %v", wantKeyNil, out)
		}
	})
}

// TestStableSort_FirstErrorLatchStopsCallbacks pins the first-error latch:
// a failing key call suppresses all later callback invocations, and a
// failing comparator call (after projection completes) suppresses all
// later comparator invocations. Latched errors surface unchanged.
func TestStableSort_FirstErrorLatchStopsCallbacks(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sentinel")
	n := 5
	items := shuffledIntItems(n)

	t.Run("key-error-suppresses-less", func(t *testing.T) {
		t.Parallel()
		const failAt = 2
		var keyCalls, lessCalls int

		key := func(v core.Value) (core.Value, error) {
			keyCalls++
			if keyCalls == failAt {
				return nil, sentinel
			}
			return core.Int{V: v.(core.Int).V}, nil
		}
		less := func(a, b core.Value) (bool, error) {
			lessCalls++
			ai, bi := a.(core.Int).V, b.(core.Int).V
			return ai < bi, nil
		}

		out, err := StableSort(context.Background(), items, key, less)
		if !errors.Is(err, sentinel) {
			t.Fatalf("key-error: want errors.Is(sentinel), got %v", err)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("key-error: want non-terminal, got terminal %v", err)
		}
		if out != nil {
			t.Fatalf("key-error: want nil result, got %v", out)
		}
		if keyCalls != failAt {
			t.Fatalf("key invocations: want %d, got %d", failAt, keyCalls)
		}
		if lessCalls != 0 {
			t.Fatalf("less invocations: want 0 after key latch, got %d", lessCalls)
		}
	})

	t.Run("less-error-after-full-projection", func(t *testing.T) {
		t.Parallel()
		var keyCalls, lessCalls int

		key := func(v core.Value) (core.Value, error) {
			keyCalls++
			return core.Int{V: v.(core.Int).V}, nil
		}
		less := func(a, b core.Value) (bool, error) {
			lessCalls++
			return false, sentinel
		}

		out, err := StableSort(context.Background(), items, key, less)
		if !errors.Is(err, sentinel) {
			t.Fatalf("less-error: want errors.Is(sentinel), got %v", err)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("less-error: want non-terminal, got terminal %v", err)
		}
		if out != nil {
			t.Fatalf("less-error: want nil result, got %v", out)
		}
		if keyCalls != n {
			t.Fatalf("key invocations: want %d (projection completes first), got %d", n, keyCalls)
		}
		if lessCalls != 1 {
			t.Fatalf("user-less invocations: want exactly 1 (latched), got %d", lessCalls)
		}
	})
}

// TestStableSort_TerminalFlushWinsOverPending replays the consumer-side
// precedence from core/builtin_budget_test.go:170-178 at the kernel
// boundary. A pending non-Terminal sentinel from the key closure surfaces
// when the Flush succeeds; a Terminal ResourceLimitError from the Flush
// replaces it when the budget is exceeded.
func TestStableSort_TerminalFlushWinsOverPending(t *testing.T) {
	t.Parallel()

	const n = 300
	items := intItems(n)
	sentinel := errors.New("pending-key-sentinel")

	key := func(v core.Value) (core.Value, error) {
		// Index by the input's underlying value: succeed on the first 200,
		// then return the sentinel for invocations 201..300.
		idx := int(v.(core.Int).V) - 1
		if idx >= 200 {
			return nil, sentinel
		}
		return core.Int{V: v.(core.Int).V}, nil
	}
	less := naturalLessForSort

	t.Run("flush-success-surfaces-sentinel", func(t *testing.T) {
		t.Parallel()
		ctx := core.WithEvalResourceLimits(context.Background(), 550, 1<<30)
		out, err := StableSort(ctx, items, key, less)
		if !errors.Is(err, sentinel) {
			t.Fatalf("want errors.Is(sentinel), got %v", err)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("want non-terminal (Flush succeeded), got terminal %v", err)
		}
		if out != nil {
			t.Fatalf("want nil result, got %v", out)
		}
	})

	t.Run("terminal-flush-replaces-sentinel", func(t *testing.T) {
		t.Parallel()
		ctx := core.WithEvalResourceLimits(context.Background(), 450, 1<<30)
		out, err := StableSort(ctx, items, key, less)
		assertTerminalLimit(t, err)
		if errors.Is(err, sentinel) {
			t.Fatalf("Terminal Flush must replace the pending sentinel, got %v", err)
		}
		if out != nil {
			t.Fatalf("want nil result, got %v", out)
		}
	})
}

// TestStableSort_BudgetBound pins the kernel-level Step/Flush contract:
// with key=nil and a natural less, the n-copy phase alone trips a tight
// budget (300 copy Steps exceeds a 10-unit limit on the first batch sync),
// while a generous budget lets the sort complete and yields ascending
// [1..n].
func TestStableSort_BudgetBound(t *testing.T) {
	t.Parallel()

	const n = 300
	items := shuffledIntItems(n)

	t.Run("tight-budget-errors", func(t *testing.T) {
		t.Parallel()
		ctx := core.WithEvalResourceLimits(context.Background(), 10, 1<<30)
		out, err := StableSort(ctx, items, nil, naturalLessForSort)
		assertTerminalLimit(t, err)
		if out != nil {
			t.Fatalf("want nil result on terminal, got %v", out)
		}
	})

	t.Run("generous-budget-succeeds", func(t *testing.T) {
		t.Parallel()
		ctx := core.WithEvalResourceLimits(context.Background(), 10000, 1<<30)
		out, err := StableSort(ctx, items, nil, naturalLessForSort)
		if err != nil {
			t.Fatalf("StableSort generous-budget: unexpected error: %v", err)
		}
		if err := sortedAscendingInt(out); err != nil {
			t.Fatalf("StableSort generous-budget: output not ascending 1..%d: %v", n, err)
		}
	})
}

// countingLessFunc returns a strict core.Int ordering that counts every
// invocation.
func countingLessFunc(count *int) func(a, b core.Value) (bool, error) {
	return func(a, b core.Value) (bool, error) {
		*count++
		return a.(core.Int).V < b.(core.Int).V, nil
	}
}

// TestStableSort_OneLessCallPerComparison pins one Step and exactly one
// less invocation per comparator call. With key=nil and n=8 the total is
// 16+c units (8 copy + c comparator + 8 result), so 16+c succeeds and
// 15+c trips at the final Flush; a kernel that called less twice per
// comparison or left the copy/result phases uncharged would pass under
// 15+c.
func TestStableSort_OneLessCallPerComparison(t *testing.T) {
	const n = 8
	items := shuffledIntItems(n)

	c := 0
	t.Run("generous budget records comparator count", func(t *testing.T) {
		out, err := StableSort(context.Background(), items, nil, countingLessFunc(&c))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := sortedAscendingInt(out); err != nil {
			t.Fatalf("output not ascending 1..%d: %v", n, err)
		}
		if c == 0 {
			t.Fatal("expected at least one less invocation")
		}
	})

	t.Run("budget covering copy comparator and result succeeds", func(t *testing.T) {
		out, err := StableSort(core.WithEvalResourceLimits(context.Background(), 16+c, 1<<30), items, nil, countingLessFunc(new(int)))
		if err != nil {
			t.Fatalf("expected success under budget %d, got error: %v", 16+c, err)
		}
		if err := sortedAscendingInt(out); err != nil {
			t.Fatalf("output not ascending 1..%d: %v", n, err)
		}
	})

	t.Run("budget one under total trips at final flush", func(t *testing.T) {
		out, err := StableSort(core.WithEvalResourceLimits(context.Background(), 15+c, 1<<30), items, nil, countingLessFunc(new(int)))
		assertTerminalLimit(t, err)
		if out != nil {
			t.Fatalf("want nil result on terminal, got %v", out)
		}
	})
}

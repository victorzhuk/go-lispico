package collections

import (
	"context"
	"errors"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func intItems(n int) []core.Value {
	items := make([]core.Value, n)
	for i := range items {
		items[i] = core.Int{V: int64(i + 1)}
	}
	return items
}

func assertTerminalLimit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected terminal resource limit error, got nil")
	}
	if !core.IsTerminalEvalError(err) {
		t.Fatalf("expected terminal eval error, got %v", err)
	}
	var le *core.LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *core.LispicoError, got %v", err)
	}
	if le.Code != core.CodeResourceLimit {
		t.Fatalf("expected code %q, got %q", core.CodeResourceLimit, le.Code)
	}
}

type scriptedEval struct {
	returns []core.Value
	failAt  int
	failErr error
	calls   [][]core.Value
}

func (s *scriptedEval) Eval(_ context.Context, form core.Value, _ *core.Env) (core.Value, error) {
	return form, nil
}

func (s *scriptedEval) Apply(_ context.Context, _ core.Value, args []core.Value, _ *core.Env) (core.Value, error) {
	s.calls = append(s.calls, args)
	if s.failAt == len(s.calls) {
		return nil, s.failErr
	}
	return s.returns[(len(s.calls)-1)%len(s.returns)], nil
}

func TestIndexedAccess_Outcomes(t *testing.T) {
	l3 := core.NewList(intItems(3))
	v3 := core.NewVector(intItems(3))

	t.Run("hits", func(t *testing.T) {
		cases := []struct {
			subject core.Value
			idx     int64
			want    core.Value
		}{
			{l3, 0, core.Int{V: 1}},
			{l3, 1, core.Int{V: 2}},
			{l3, 2, core.Int{V: 3}},
			{v3, 0, core.Int{V: 1}},
			{v3, 1, core.Int{V: 2}},
			{v3, 2, core.Int{V: 3}},
		}
		for _, tc := range cases {
			val, outcome, err := IndexedAccess(context.Background(), tc.subject, tc.idx)
			if err != nil {
				t.Fatalf("IndexedAccess(%T, %d): unexpected error: %v", tc.subject, tc.idx, err)
			}
			if outcome != AccessHit {
				t.Fatalf("IndexedAccess(%T, %d): expected AccessHit, got %v", tc.subject, tc.idx, outcome)
			}
			if !val.Equals(tc.want) {
				t.Fatalf("IndexedAccess(%T, %d): expected %v, got %v", tc.subject, tc.idx, tc.want, val)
			}
		}
	})

	t.Run("out of range", func(t *testing.T) {
		for _, subject := range []core.Value{l3, v3} {
			for _, idx := range []int64{-1, 3, 4} {
				val, outcome, err := IndexedAccess(context.Background(), subject, idx)
				if err != nil {
					t.Fatalf("IndexedAccess(%T, %d): unexpected error: %v", subject, idx, err)
				}
				if outcome != AccessOutOfRange {
					t.Fatalf("IndexedAccess(%T, %d): expected AccessOutOfRange, got %v", subject, idx, outcome)
				}
				if val != nil {
					t.Fatalf("IndexedAccess(%T, %d): expected nil value, got %v", subject, idx, val)
				}
			}
		}
	})

	t.Run("unsupported subjects", func(t *testing.T) {
		for _, subject := range []core.Value{core.Nil{}, core.String{V: "x"}, core.Int{V: 1}} {
			val, outcome, err := IndexedAccess(context.Background(), subject, 0)
			if err != nil {
				t.Fatalf("IndexedAccess(%T): unexpected error: %v", subject, err)
			}
			if outcome != AccessUnsupported {
				t.Fatalf("IndexedAccess(%T): expected AccessUnsupported, got %v", subject, outcome)
			}
			if val != nil {
				t.Fatalf("IndexedAccess(%T): expected nil value, got %v", subject, val)
			}
		}
	})

	t.Run("empty list at index 0", func(t *testing.T) {
		_, outcome, err := IndexedAccess(context.Background(), core.NewList(nil), 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != AccessOutOfRange {
			t.Fatalf("expected AccessOutOfRange, got %v", outcome)
		}
	})
}

func TestIndexedAccess_BudgetSteps(t *testing.T) {
	cases := []struct {
		name         string
		subject      core.Value
		idx          int64
		maxReduction int64
		wantTerminal bool
	}{
		{"list of 2 at idx 1 with max 1 is terminal", core.NewList(intItems(2)), 1, 1, true},
		{"list of 2 at idx 1 with max 3 succeeds", core.NewList(intItems(2)), 1, 3, false},
		{"vector of 300 at idx 299 with max 2 succeeds", core.NewVector(intItems(300)), 299, 2, false},
		{"list of 300 at idx 299 with max 2 is terminal", core.NewList(intItems(300)), 299, 2, true},
		{"list of 200 at idx 150 with max 100 is terminal", core.NewList(intItems(200)), 150, 100, true},
		{"list of 200 at idx 150 with max 400 succeeds", core.NewList(intItems(200)), 150, 400, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := core.WithEvalResourceLimits(context.Background(), int(tc.maxReduction), 1<<30)
			val, outcome, err := IndexedAccess(ctx, tc.subject, tc.idx)
			if tc.wantTerminal {
				assertTerminalLimit(t, err)
				return
			}
			if err != nil {
				t.Fatalf("expected success, got error: %v", err)
			}
			if outcome != AccessHit {
				t.Fatalf("expected AccessHit, got %v", outcome)
			}
			if !val.Equals(core.Int{V: tc.idx + 1}) {
				t.Fatalf("expected element %v, got %v", core.Int{V: tc.idx + 1}, val)
			}
		})
	}
}

func TestMapSequences_AlignedTuples(t *testing.T) {
	ctx := context.Background()
	env := core.NewEnv(nil)
	fn := core.String{V: "callback"}

	t.Run("single list applies once per element with one argument", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 100}, core.Int{V: 200}, core.Int{V: 300}}}
		res, err := MapSequences(ctx, ev, env, fn, []core.Value{core.NewList(intItems(3))})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ev.calls) != 3 {
			t.Fatalf("expected exactly 3 Applies, got %d", len(ev.calls))
		}
		for i, args := range ev.calls {
			if len(args) != 1 {
				t.Fatalf("Apply %d: expected 1 argument, got %d", i+1, len(args))
			}
			if !args[0].Equals(core.Int{V: int64(i + 1)}) {
				t.Fatalf("Apply %d: expected argument %v, got %v", i+1, core.Int{V: int64(i + 1)}, args[0])
			}
		}
		lst, ok := res.(core.List)
		if !ok {
			t.Fatalf("expected core.List result, got %T", res)
		}
		if lst.Len() != 3 {
			t.Fatalf("expected result length 3, got %d", lst.Len())
		}
		for i, want := range []core.Value{core.Int{V: 100}, core.Int{V: 200}, core.Int{V: 300}} {
			if !lst.At(i).Equals(want) {
				t.Fatalf("result[%d]: expected %v, got %v", i, want, lst.At(i))
			}
		}
	})

	t.Run("list and vector align arguments in sequence order", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}, core.Int{V: 0}}}
		res, err := MapSequences(ctx, ev, env, fn, []core.Value{
			core.NewList(intItems(3)),
			core.NewVector([]core.Value{core.Int{V: 10}, core.Int{V: 20}}),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ev.calls) != 2 {
			t.Fatalf("expected exactly 2 Applies, got %d", len(ev.calls))
		}
		wantCalls := [][]core.Value{
			{core.Int{V: 1}, core.Int{V: 10}},
			{core.Int{V: 2}, core.Int{V: 20}},
		}
		for i, args := range ev.calls {
			if len(args) != 2 {
				t.Fatalf("Apply %d: expected 2 arguments, got %d", i+1, len(args))
			}
			for j, want := range wantCalls[i] {
				if !args[j].Equals(want) {
					t.Fatalf("Apply %d arg %d: expected %v, got %v", i+1, j+1, want, args[j])
				}
			}
		}
		lst, ok := res.(core.List)
		if !ok {
			t.Fatalf("expected core.List result, got %T", res)
		}
		if lst.Len() != 2 {
			t.Fatalf("expected result length 2, got %d", lst.Len())
		}
	})

	t.Run("shortest list terminates mapping", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}}
		res, err := MapSequences(ctx, ev, env, fn, []core.Value{
			core.NewList(intItems(3)),
			core.NewList([]core.Value{core.Int{V: 9}}),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ev.calls) != 1 {
			t.Fatalf("expected exactly 1 Apply, got %d", len(ev.calls))
		}
		if len(ev.calls[0]) != 2 || !ev.calls[0][0].Equals(core.Int{V: 1}) || !ev.calls[0][1].Equals(core.Int{V: 9}) {
			t.Fatalf("expected aligned args (1, 9), got %v", ev.calls[0])
		}
		lst, ok := res.(core.List)
		if !ok || lst.Len() != 1 {
			t.Fatalf("expected core.List of length 1, got %T", res)
		}
	})

	t.Run("nil sequence maps to empty list", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}}
		res, err := MapSequences(ctx, ev, env, fn, []core.Value{core.Nil{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ev.calls) != 0 {
			t.Fatalf("expected 0 Applies, got %d", len(ev.calls))
		}
		lst, ok := res.(core.List)
		if !ok {
			t.Fatalf("expected core.List result, got %T", res)
		}
		if lst.Len() != 0 {
			t.Fatalf("expected empty result, got length %d", lst.Len())
		}
	})
}

func TestMapSequences_CallbackStopOnError(t *testing.T) {
	ctx := context.Background()
	env := core.NewEnv(nil)
	fn := core.String{V: "callback"}
	sentinel := errors.New("callback sentinel")

	t.Run("no callback after first error", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}, failAt: 2, failErr: sentinel}
		_, err := MapSequences(ctx, ev, env, fn, []core.Value{core.NewList(intItems(5))})
		if len(ev.calls) != 2 {
			t.Fatalf("expected exactly 2 Applies, got %d", len(ev.calls))
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected callback sentinel unchanged, got %v", err)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("callback error must not be terminal: %v", err)
		}
	})

	t.Run("all empty sequences never call the callback", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}}
		res, err := MapSequences(ctx, ev, env, fn, []core.Value{core.Nil{}, core.Nil{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ev.calls) != 0 {
			t.Fatalf("expected 0 Applies, got %d", len(ev.calls))
		}
		lst, ok := res.(core.List)
		if !ok || lst.Len() != 0 {
			t.Fatalf("expected empty core.List result, got %T", res)
		}
	})
}

func TestMapSequences_BudgetPerTuple(t *testing.T) {
	env := core.NewEnv(nil)
	fn := core.String{V: "callback"}
	sentinel := errors.New("callback sentinel")

	t.Run("one step per tuple charges the reduction budget", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}}
		_, err := MapSequences(core.WithEvalResourceLimits(context.Background(), 100, 1<<30), ev, env, fn, []core.Value{core.NewList(intItems(200))})
		assertTerminalLimit(t, err)
	})

	t.Run("budget above tuple count succeeds", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}}
		_, err := MapSequences(core.WithEvalResourceLimits(context.Background(), 256, 1<<30), ev, env, fn, []core.Value{core.NewList(intItems(200))})
		if err != nil {
			t.Fatalf("expected success under budget 256, got error: %v", err)
		}
		if len(ev.calls) != 200 {
			t.Fatalf("expected 200 Applies, got %d", len(ev.calls))
		}
	})

	t.Run("flush on error return path", func(t *testing.T) {
		ev := &scriptedEval{returns: []core.Value{core.Int{V: 0}}, failAt: 1, failErr: sentinel}
		_, err := MapSequences(core.WithEvalResourceLimits(context.Background(), 300, 1<<30), ev, env, fn, []core.Value{core.NewList(intItems(300))})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected callback sentinel unchanged, got %v", err)
		}
		if core.IsTerminalEvalError(err) {
			t.Fatalf("callback error must not be masked as terminal: %v", err)
		}
	})
}

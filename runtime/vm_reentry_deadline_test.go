package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// spinDef returns a pure-bytecode spin definition: the loop runs entirely in
// the VM before any GoFunc is entered, so the engine deadline is armed and
// time passes before the re-entry boundary is crossed.
const spinDef = "(defn spin [] (loop [n 500000] (if (= n 0) n (recur (- n 1)))))"

func newDeadlineEngine(t *testing.T, timeout time.Duration) Engine {
	t.Helper()
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	bindBuiltin(t, eng, "=")
	bindBuiltin(t, eng, "-")
	return eng
}

// TestCallReentrancy_VMAbsoluteDeadlineNotRestarted proves the VM run's
// already-resolved absolute deadline governs a Builtin work budget built
// inside a GoFunc: substantial bytecode work runs before the GoFunc entry,
// the GoFunc then spends local time without observing the eval state, and
// the budget's first Flush must report DeadlineExceeded against the original
// deadline rather than a deadline restarted at first observation.
func TestCallReentrancy_VMAbsoluteDeadlineNotRestarted(t *testing.T) {
	const timeout = 500 * time.Millisecond
	eng := newDeadlineEngine(t, timeout)

	var flushErr, callErr error
	var probeRan bool
	require.NoError(t, eng.Bind("probe", core.GoFunc{
		Name: "probe",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			probeRan = true
			// Spend well past the engine timeout without observing the
			// eval state: no clock read, no sync, just local work.
			time.Sleep(timeout + timeout/2)
			b := core.NewBuiltinWorkBudget(ctx)
			for range 5 {
				if err := b.Step(); err != nil {
					flushErr = err
					return core.Nil{}, nil
				}
			}
			flushErr = b.Flush()
			return core.Nil{}, nil
		},
	}))

	ctx := context.Background()
	_, err := eng.Eval(ctx, "def-spin", spinDef)
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-probe", "(defn probe-run [] (if (= (spin) 0) (probe) nil))")
	require.NoError(t, err)
	_, callErr = eng.Call(ctx, "probe-run")
	if !probeRan || callErr != nil {
		t.Fatalf("probe did not dispatch as expected: ran=%v callErr=%v", probeRan, callErr)
	}
	assert.True(t, errors.Is(flushErr, context.DeadlineExceeded),
		"first Flush after dispatch must inherit the run's absolute deadline (expired), got %v", flushErr)
}

// TestCallReentrancy_NestedCallbackInheritsVMAbsoluteDeadline proves a
// nested evaluator callback (GoFunc -> eval.Eval -> GoFunc) sees the same
// absolute VM deadline: the inner budget's first Flush fires against the
// run's deadline, not a deadline derived at the nested observation.
func TestCallReentrancy_NestedCallbackInheritsVMAbsoluteDeadline(t *testing.T) {
	const timeout = 500 * time.Millisecond
	eng := newDeadlineEngine(t, timeout)

	innerForms, err := clojure.Dialect().ReadWithMaxDepth("(inner)", 200)
	require.NoError(t, err)
	require.Len(t, innerForms, 1)
	innerForm := innerForms[0]

	var flushErr error
	require.NoError(t, eng.Bind("inner", core.GoFunc{
		Name: "inner",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			b := core.NewBuiltinWorkBudget(ctx)
			_ = b.Step()
			time.Sleep(400 * time.Millisecond)
			flushErr = b.Flush()
			return core.Nil{}, nil
		},
	}))
	require.NoError(t, eng.Bind("outer", core.GoFunc{
		Name: "outer",
		Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			// Spend part of the budget before the nested callback so the
			// nested observation happens strictly after run start.
			time.Sleep(300 * time.Millisecond)
			return eval.Eval(ctx, innerForm, env)
		},
	}))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "def-spin", spinDef)
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-outer", "(defn outer-run [] (if (= (spin) 0) (outer) nil))")
	require.NoError(t, err)

	_, _ = eng.Call(ctx, "outer-run")
	assert.True(t, errors.Is(flushErr, context.DeadlineExceeded),
		"nested callback must inherit the run's absolute deadline, got %v", flushErr)
}

// TestCallReentrancy_VMAbsoluteDeadline_OuterEarlierWins proves an outer
// non-zero eval deadline earlier than the engine timeout governs alone: the
// VM's own deadline must not be installed over it, and a Builtin budget
// inside the GoFunc flushes against the outer deadline.
func TestCallReentrancy_VMAbsoluteDeadline_OuterEarlierWins(t *testing.T) {
	eng := newDeadlineEngine(t, 500*time.Millisecond)

	outerDeadline := time.Now().Add(100 * time.Millisecond)

	var flushErr error
	var observed time.Time
	require.NoError(t, eng.Bind("probe", core.GoFunc{
		Name: "probe",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			observed = core.EvalDeadlineFrom(ctx)
			b := core.NewBuiltinWorkBudget(ctx)
			_ = b.Step()
			time.Sleep(300 * time.Millisecond)
			flushErr = b.Flush()
			return core.Nil{}, nil
		},
	}))

	base := context.Background()
	_, err := eng.Eval(base, "def-probe", "(defn probe-run [] (probe))")
	require.NoError(t, err)

	ctx := core.WithEvalDeadline(base, outerDeadline)
	_, _ = eng.Call(ctx, "probe-run")

	assert.True(t, observed.Equal(outerDeadline),
		"outer earlier deadline must govern alone (observed %v, want %v)", observed, outerDeadline)
	assert.True(t, errors.Is(flushErr, context.DeadlineExceeded),
		"first Flush must fire against the outer deadline, got %v", flushErr)
}

// TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline covers both
// late-entry shapes: an unarmed run (timeout disabled) installs no deadline
// at all, and a second GoFunc entry in one armed run keeps the deadline the
// first entry saw — never a fresh now+timeout derivation.
func TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline(t *testing.T) {
	t.Run("unarmed run installs no deadline", func(t *testing.T) {
		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(0))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		var flushErr error
		var observed time.Time
		require.NoError(t, eng.Bind("probe", core.GoFunc{
			Name: "probe",
			Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				observed = core.EvalDeadlineFrom(ctx)
				b := core.NewBuiltinWorkBudget(ctx)
				for range 5 {
					if err := b.Step(); err != nil {
						flushErr = err
						return core.Nil{}, nil
					}
				}
				flushErr = b.Flush()
				return core.Nil{}, nil
			},
		}))

		ctx := context.Background()
		_, err = eng.Eval(ctx, "def-probe", "(defn probe-run [] (probe))")
		require.NoError(t, err)
		_, err = eng.Call(ctx, "probe-run")
		require.NoError(t, err)

		assert.True(t, observed.IsZero(), "unarmed run must not install a deadline, got %v", observed)
		assert.NoError(t, flushErr, "unarmed run budget flush must succeed")
	})

	t.Run("second entry keeps first entry's deadline", func(t *testing.T) {
		const timeout = 500 * time.Millisecond
		eng := newDeadlineEngine(t, timeout)

		var first, second time.Time
		var flushErr error
		require.NoError(t, eng.Bind("first", core.GoFunc{
			Name: "first",
			Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				first = core.EvalDeadlineFrom(ctx)
				return core.Int{V: 1}, nil
			},
		}))
		require.NoError(t, eng.Bind("second", core.GoFunc{
			Name: "second",
			Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				second = core.EvalDeadlineFrom(ctx)
				b := core.NewBuiltinWorkBudget(ctx)
				_ = b.Step()
				time.Sleep(timeout + timeout/2)
				flushErr = b.Flush()
				return core.Nil{}, nil
			},
		}))

		ctx := context.Background()
		_, err := eng.Eval(ctx, "def-two", "(defn two-runs [] (if (= (first) 1) (second) nil))")
		require.NoError(t, err)

		_, _ = eng.Call(ctx, "two-runs")

		assert.True(t, second.Equal(first),
			"late GoFunc entry must retain the same absolute deadline (first %v, second %v)", first, second)
		assert.True(t, errors.Is(flushErr, context.DeadlineExceeded),
			"retained deadline must be expired by the late entry's flush, got %v", flushErr)
	})
}

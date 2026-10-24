package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestBytecodeComputedHead_EffectCountsColdWarm pins the computed-head
// contract on the bytecode engine: macro discovery at compile time runs the
// computed head zero times, and every evaluation — cold or warm cache — runs
// it exactly once.
func TestBytecodeComputedHead_EffectCountsColdWarm(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	calls := 0
	require.NoError(t, eng.Bind("make-adder", core.GoFunc{
		Name: "make-adder",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			calls++
			return core.GoFunc{Name: "add", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				sum := core.Int{V: 0}
				for _, a := range args {
					sum = core.Int{V: sum.V + a.(core.Int).V}
				}
				return sum, nil
			}}, nil
		},
	}))

	src := "((make-adder) 1 2)"
	got, err := eng.Eval(t.Context(), "cold", src)
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 3}, got)
	assert.Equal(t, 1, calls, "cold eval: compile-time discovery must not execute the computed head")

	got, err = eng.Eval(t.Context(), "warm", src)
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 3}, got)
	assert.Equal(t, 2, calls, "warm eval runs the computed head exactly once again")
}

// TestTreeFallbackCallsCompiledClosure pins the SetCallableApplier seam: a
// form the bytecode compiler refuses falls back to the tree-walker, and a
// call on an already-compiled closure from that fallback dispatches through
// the installed applier instead of failing with a TypeError.
func TestTreeFallbackCallsCompiledClosure(t *testing.T) {
	t.Parallel()

	for _, d := range []core.Dialect{clojure.Dialect(), cl.Dialect()} {
		t.Run(fmt.Sprintf("lisp2=%v", d.IsLisp2()), func(t *testing.T) {
			eng, err := New(nil, WithBytecode(), WithDialect(d))
			require.NoError(t, err)

			require.NoError(t, eng.Bind("add2", core.GoFunc{
				Name: "add2",
				Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
					sum := core.Int{V: 0}
					for _, a := range args {
						sum = core.Int{V: sum.V + a.(core.Int).V}
					}
					return sum, nil
				},
			}))
			_, err = eng.Eval(t.Context(), "defs", "(defn dbl (x) (add2 x x))")
			require.NoError(t, err)

			// The scoped defmacro inside let makes the whole form CodeUnsupported,
			// so the tree-walker evaluates it and must call the compiled dbl.
			src := "(let () (defmacro once () 1) (dbl 21))"
			got, err := eng.Eval(t.Context(), "fallback", src)
			require.NoError(t, err, "tree fallback must call the compiled closure through the applier")
			assert.Equal(t, core.Int{V: 42}, got)
			_ = eng.Close()
		})
	}
}

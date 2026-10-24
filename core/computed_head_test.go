package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// computedHeadFixture binds make-adder, a GoFunc that counts every call and
// returns an adder closure, and builds the form ((make-adder) 1 2) whose head
// is a computed call rather than a symbol.
func computedHeadFixture(t *testing.T) (*engine, *Env, List, *int) {
	ev := NewEvaluator()
	env := NewEnv(nil)
	calls := 0
	head := GoFunc{Name: "make-adder", Fn: func(_ context.Context, _ Evaluator, _ []Value, _ *Env) (Value, error) {
		calls++
		return GoFunc{Name: "add", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
			sum := Int{V: 0}
			for _, a := range args {
				sum = Int{V: sum.V + a.(Int).V}
			}
			return sum, nil
		}}, nil
	}}
	require.NoError(t, env.Set("make-adder", head))
	form := NewList([]Value{
		NewList([]Value{Symbol{V: "make-adder"}}),
		Int{V: 1},
		Int{V: 2},
	})
	return ev, env, form, &calls
}

func TestComputedHead_ExecutesOncePerEvaluation(t *testing.T) {
	ev, env, form, calls := computedHeadFixture(t)

	res, err := ev.Eval(context.Background(), form, env)
	require.NoError(t, err)
	assert.Equal(t, Int{V: 3}, res)
	assert.Equal(t, 1, *calls, "cold evaluation runs the computed head once")

	res, err = ev.Eval(context.Background(), form, env)
	require.NoError(t, err)
	assert.Equal(t, Int{V: 3}, res)
	assert.Equal(t, 2, *calls, "second evaluation runs the computed head once more")
}

func TestMacroDiscovery_DoesNotExecuteComputedHead(t *testing.T) {
	ev, env, form, calls := computedHeadFixture(t)

	ctx := context.Background()
	expanded, err := ev.MacroExpand(ctx, form, env)
	require.NoError(t, err)
	assert.Equal(t, 0, *calls, "MacroExpand must not execute a computed head")
	assert.Equal(t, form, expanded, "no macro at head leaves the form unchanged")

	deep, err := ev.MacroExpandDeep(ctx, form, env)
	require.NoError(t, err)
	assert.Equal(t, 0, *calls, "MacroExpandDeep must not execute a computed head")
	assert.Equal(t, form, deep)
}

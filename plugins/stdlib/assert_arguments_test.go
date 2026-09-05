package stdlib

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/victorzhuk/go-lispico/core"
)

// errAssertReentry is what the refusing evaluator returns from either method:
// assert is handed arguments the apply site already evaluated, so reaching the
// evaluator again is the defect, whatever the argument shape.
var errAssertReentry = errors.New("assert re-entered the evaluator")

type assertRefusingEvaluator struct{}

func (assertRefusingEvaluator) Eval(context.Context, core.Value, *core.Env) (core.Value, error) {
	return nil, errAssertReentry
}

func (assertRefusingEvaluator) Apply(context.Context, core.Value, []core.Value, *core.Env) (core.Value, error) {
	return nil, errAssertReentry
}

// TestAssert_DoesNotReEnterTheEvaluator drives every argument shape straight
// through the registered GoFunc with an evaluator that refuses to be entered.
func TestAssert_DoesNotReEnterTheEvaluator(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "assert")

	list := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})

	cases := []struct {
		name string
		args []core.Value
	}{
		{name: "truthy condition", args: []core.Value{core.Bool{V: true}}},
		{name: "false condition", args: []core.Value{core.Bool{V: false}}},
		{name: "nil condition", args: []core.Value{core.Nil{}}},
		{name: "symbol condition", args: []core.Value{core.Symbol{V: "x"}}},
		{name: "list condition", args: []core.Value{list}},
		{name: "string message", args: []core.Value{core.Bool{V: false}, core.String{V: "boom"}}},
		{name: "symbol message", args: []core.Value{core.Bool{V: false}, core.Symbol{V: "x"}}},
		{name: "list message", args: []core.Value{core.Bool{V: false}, list}},
		{name: "message on a truthy condition", args: []core.Value{core.Bool{V: true}, core.Symbol{V: "x"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
			_, err := fn.Fn(ctx, assertRefusingEvaluator{}, tc.args, env)
			assert.False(t, errors.Is(err, errAssertReentry),
				"assert must use the arguments it was given, not evaluate them again: %v", err)
		})
	}
}

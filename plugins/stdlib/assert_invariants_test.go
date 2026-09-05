package stdlib

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestAssert_InvariantsUnchanged: assert no longer re-enters the evaluator on
// its own arguments, and only the shapes that were being re-evaluated may
// change. Arity, truthiness, the success return and every scalar message
// rendering must report exactly what the re-entering build reported.
//
// A Symbol or List condition is deliberately absent: those two shapes do change
// here, and they are pinned by TestAssert_DoesNotReEnterTheEvaluator.
func TestAssert_InvariantsUnchanged(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "assert")
	ev := core.NewEvaluator()

	tests := []struct {
		name    string
		args    []core.Value
		code    string
		message string
	}{
		{
			name:    "no arguments",
			code:    "ArityError",
			message: "assert: requires at least 1 argument",
		},
		{
			name: "truthy condition returns nil",
			args: []core.Value{core.Bool{V: true}},
		},
		{
			name:    "falsy bool condition",
			args:    []core.Value{core.Bool{V: false}},
			code:    "EvalError",
			message: "assertion failed",
		},
		{
			name:    "nil condition",
			args:    []core.Value{core.Nil{}},
			code:    "EvalError",
			message: "assertion failed",
		},
		{
			name:    "string message is emitted raw",
			args:    []core.Value{core.Bool{V: false}, core.String{V: "boom"}},
			code:    "EvalError",
			message: "assertion failed: boom",
		},
		{
			name:    "keyword message",
			args:    []core.Value{core.Bool{V: false}, core.Keyword{V: "k"}},
			code:    "EvalError",
			message: "assertion failed: :k",
		},
		{
			name:    "int message",
			args:    []core.Value{core.Bool{V: false}, core.Int{V: 7}},
			code:    "EvalError",
			message: "assertion failed: 7",
		},
		{
			name:    "bool message",
			args:    []core.Value{core.Bool{V: false}, core.Bool{V: true}},
			code:    "EvalError",
			message: "assertion failed: true",
		},
		{
			name:    "float message",
			args:    []core.Value{core.Bool{V: false}, core.Float{V: 1.5}},
			code:    "EvalError",
			message: "assertion failed: 1.5",
		},
		{
			name:    "nil message",
			args:    []core.Value{core.Bool{V: false}, core.Nil{}},
			code:    "EvalError",
			message: "assertion failed: nil",
		},
		{
			name:    "extra arguments are legal and ignored",
			args:    []core.Value{core.Bool{V: false}, core.String{V: "a"}, core.String{V: "b"}},
			code:    "EvalError",
			message: "assertion failed: a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
			got, err := fn.Fn(ctx, ev, tt.args, env)

			if tt.code == "" {
				require.NoError(t, err)
				require.Equalf(t, core.Nil{}, got, "a passing assert must return nil, got %v", got)
				return
			}

			var lerr *core.LispicoError
			require.ErrorAs(t, err, &lerr)
			require.Equal(t, tt.code, lerr.Code)
			require.Equal(t, tt.message, lerr.Message)
		})
	}
}

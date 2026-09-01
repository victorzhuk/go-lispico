package stdlib

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// callBuiltin invokes a registered builtin's GoFunc directly, so the assertion
// observes exactly the error that builtin returned: no reader, no form
// dispatch, no engine boundary in between.
func callBuiltin(ctx context.Context, t *testing.T, env *core.Env, name string, args ...core.Value) (core.Value, error) {
	t.Helper()
	v, ok := env.Get(name)
	require.Truef(t, ok, "builtin %q is not registered", name)
	fn, ok := v.(core.GoFunc)
	require.Truef(t, ok, "builtin %q is %T, want core.GoFunc", name, v)
	return fn.Fn(ctx, core.NewEvaluator(), args, env)
}

func builtinErr(t *testing.T, env *core.Env, name string, args ...core.Value) error {
	t.Helper()
	_, err := callBuiltin(context.Background(), t, env, name, args...)
	require.Errorf(t, err, "%s: expected an error, got nil", name)
	return err
}

// failingCallback returns a builtin-shaped callback that always fails with err,
// used to drive the higher-order passthrough contracts.
func failingCallback(err error) core.GoFunc {
	return core.GoFunc{
		Name: "failing-callback",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return nil, err
		},
	}
}

type classRow struct {
	name    string
	builtin string
	args    []core.Value
	wantMsg string
}

func runClassRows(t *testing.T, wantCode string, rows []classRow) {
	t.Helper()
	env := setupEnv(t)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := builtinErr(t, env, row.builtin, row.args...)
			var le *core.LispicoError
			require.ErrorAs(t, err, &le)
			require.Equal(t, wantCode, le.Code)
			require.Contains(t, le.Message, row.wantMsg)
		})
	}
}

func TestBuiltinArityErrorClass_Direct(t *testing.T) {
	list12 := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
	cb := failingCallback(errors.New("unused"))

	runClassRows(t, "ArityError", []classRow{
		{"sort exact none", "sort", nil, "sort:"},
		{"sort exact two", "sort", []core.Value{list12, list12}, "sort:"},
		{"mod exact one", "mod", []core.Value{core.Int{V: 1}}, "mod:"},
		{"quot exact three", "quot", []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}, "quot:"},
		{"count exact none", "count", nil, "count:"},
		{"map exact one", "map", []core.Value{cb}, "map:"},
		{"filter exact three", "filter", []core.Value{cb, list12, list12}, "filter:"},

		{"nth ranged one", "nth", []core.Value{list12}, "nth:"},
		{"nth ranged four", "nth", []core.Value{list12, core.Int{V: 0}, core.Nil{}, core.Nil{}}, "nth:"},
		{"range ranged none", "range", nil, "range:"},
		{"range ranged four", "range", []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}, core.Int{V: 4}}, "range:"},
		{"reduce ranged one", "reduce", []core.Value{cb}, "reduce:"},
		{"reduce ranged four", "reduce", []core.Value{cb, core.Int{V: 0}, list12, list12}, "reduce:"},

		{"divide variadic min", "/", []core.Value{core.Int{V: 1}}, "/:"},
		{"apply variadic min", "apply", []core.Value{cb}, "apply:"},
		{"equal variadic min", "=", nil, "=:"},
		{"less variadic min", "<", nil, "<:"},
		{"assert variadic min", "assert", nil, "assert:"},
	})
}

func TestBuiltinTypeErrorClass_Direct(t *testing.T) {
	list12 := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
	cb := failingCallback(errors.New("unused"))

	runClassRows(t, "TypeError", []classRow{
		{"nth float index", "nth", []core.Value{list12, core.Float{V: 1.5}}, "nth:"},
		{"nth int subject", "nth", []core.Value{core.Int{V: 5}, core.Int{V: 0}}, "nth:"},
		{"sort int subject", "sort", []core.Value{core.Int{V: 5}}, "sort:"},
		{"count keyword subject", "count", []core.Value{core.Keyword{V: "k"}}, "count:"},
		{"range string bound", "range", []core.Value{core.String{V: "3"}}, "range:"},
		{"mod float operand", "mod", []core.Value{core.Float{V: 1.5}, core.Int{V: 2}}, "mod:"},
		{"quot string operand", "quot", []core.Value{core.String{V: "1"}, core.Int{V: 2}}, "quot:"},
		{"divide non-number dividend", "/", []core.Value{core.String{V: "1"}, core.Int{V: 2}}, "/:"},
		{"divide non-number divisor", "/", []core.Value{core.Int{V: 1}, core.String{V: "2"}}, "/:"},
		{"string->int non-string", "string->int", []core.Value{core.Int{V: 1}}, "string->int:"},
		{"string->float non-string", "string->float", []core.Value{core.Int{V: 1}}, "string->float:"},
		{"map non-collection", "map", []core.Value{cb, core.Int{V: 5}}, "map:"},
		{"filter non-collection", "filter", []core.Value{cb, core.Int{V: 5}}, "filter:"},
		{"reduce non-collection", "reduce", []core.Value{cb, core.Int{V: 5}}, "reduce:"},
		{"apply non-collection tail", "apply", []core.Value{cb, core.Int{V: 5}}, "apply:"},
	})
}

// The domain class covers well-typed values outside an operation's domain:
// out-of-range indices, a zero divisor, and an incomparable pair. Comparability
// is a domain property, so sort's mixed-kind pair is EvalError, not TypeError.
func TestBuiltinDomainErrorClass_Direct(t *testing.T) {
	list12 := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
	mixed := core.NewList([]core.Value{core.Int{V: 1}, core.String{V: "a"}})

	runClassRows(t, "EvalError", []classRow{
		{"nth past end", "nth", []core.Value{list12, core.Int{V: 5}}, "nth:"},
		{"nth negative index", "nth", []core.Value{list12, core.Int{V: -1}}, "nth:"},
		{"divide int zero", "/", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "/:"},
		{"divide float zero", "/", []core.Value{core.Int{V: 1}, core.Float{V: 0}}, "/:"},
		{"mod zero divisor", "mod", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "mod:"},
		{"quot zero divisor", "quot", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "quot:"},
		{"sort incomparable pair", "sort", []core.Value{mixed}, "sort:"},
	})
}

// A malformed conversion classifies as EvalError and keeps the strconv cause
// reachable through errors.Is, so callers can still discriminate syntax from
// range failures.
func TestBuiltinParseErrorClass_Direct(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name      string
		builtin   string
		input     string
		wantCause error
	}{
		{"string->int syntax", "string->int", "abc", strconv.ErrSyntax},
		{"string->int range", "string->int", "99999999999999999999", strconv.ErrRange},
		{"string->float syntax", "string->float", "abc", strconv.ErrSyntax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := builtinErr(t, env, tt.builtin, core.String{V: tt.input})

			var le *core.LispicoError
			require.ErrorAs(t, err, &le)
			require.Equal(t, "EvalError", le.Code)
			require.Contains(t, le.Message, tt.builtin+":")
			require.ErrorIs(t, err, tt.wantCause)
			require.False(t, core.IsTerminalEvalError(err))
		})
	}
}

// A callback failure crosses the enclosing builtin untouched: the higher-order
// builtin is a conduit, not a classifier, so it must not re-code the error.
func TestBuiltinCallbackErrorPassthrough_Direct(t *testing.T) {
	env := setupEnv(t)
	list12 := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
	cause := &core.LispicoError{Code: "UndefinedError", Message: "callback exploded"}
	cb := failingCallback(cause)

	tests := []struct {
		name    string
		builtin string
		args    []core.Value
	}{
		{"map", "map", []core.Value{cb, list12}},
		{"filter", "filter", []core.Value{cb, list12}},
		{"reduce", "reduce", []core.Value{cb, list12}},
		{"reduce with init", "reduce", []core.Value{cb, core.Int{V: 0}, list12}},
		{"apply", "apply", []core.Value{cb, list12}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := builtinErr(t, env, tt.builtin, tt.args...)

			var le *core.LispicoError
			require.ErrorAs(t, err, &le)
			require.Equal(t, "UndefinedError", le.Code)
			require.Equal(t, "callback exploded", le.Message)
			require.False(t, core.IsTerminalEvalError(err))
		})
	}
}

// A terminal callback error stays terminal across the enclosing builtin:
// downgrading it to a catchable EvalError would make an uncatchable failure
// recoverable from Lisp.
func TestBuiltinCallbackTerminalPassthrough_Direct(t *testing.T) {
	env := setupEnv(t)
	list12 := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})

	terminals := []struct {
		name string
		err  error
	}{
		{"resource limit", core.NewResourceLimitError("callback exceeded a ceiling")},
		{"cancelled context", context.Canceled},
	}
	for _, term := range terminals {
		cb := failingCallback(term.err)
		args := map[string][]core.Value{
			"map":    {cb, list12},
			"filter": {cb, list12},
			"reduce": {cb, list12},
			"apply":  {cb, list12},
		}
		for _, builtin := range []string{"map", "filter", "reduce", "apply"} {
			t.Run(term.name+"/"+builtin, func(t *testing.T) {
				err := builtinErr(t, env, builtin, args[builtin]...)
				require.True(t, core.IsTerminalEvalError(err))
			})
		}
	}
}

// A cancelled context reaches the caller as a terminal error, unwrapped into
// neither a TypeError nor an EvalError.
func TestBuiltinCancelledContext_Direct(t *testing.T) {
	env := setupEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := callBuiltin(ctx, t, env, "range", core.Int{V: 5})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, core.IsTerminalEvalError(err))
}

// A resource-limit checkpoint stays terminal and keeps CodeResourceLimit: it is
// a ceiling breach, not a domain failure, and try/catch must not recover it.
func TestBuiltinResourceLimitCheckpoint_Direct(t *testing.T) {
	env := setupEnv(t)

	err := builtinErr(t, env, "range", core.Int{V: defaultStdlibCollectionLen + 1})

	var le *core.LispicoError
	require.ErrorAs(t, err, &le)
	require.Equal(t, core.CodeResourceLimit, le.Code)
	require.True(t, core.IsTerminalEvalError(err))
	require.Contains(t, le.Message, "collection limit")
}

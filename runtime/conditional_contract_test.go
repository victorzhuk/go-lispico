package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

type valueCase struct {
	src  string
	want core.Value
}

var falsyCases = []valueCase{
	{"nil", core.Nil{}},
	{"false", core.Bool{V: false}},
}

var truthyCases = []valueCase{
	{"true", core.Bool{V: true}},
	{"0", core.Int{V: 0}},
	{`""`, core.String{V: ""}},
	{":k", core.Keyword{V: "k"}},
	{"'()", core.NewList(nil)},
}

func assertValue(t *testing.T, want, got core.Value) {
	t.Helper()
	require.True(t, want.Equals(got), "want %s, got %s", want.String(), got.String())
}

// runCase builds a fresh engine per fixture, binds the mark GoFunc through
// the normal environment API when requested, evaluates src, and returns the
// result and the tags mark recorded in order.
func runCase(t *testing.T, mode []EngineOption, dialect []EngineOption, src string, withMark bool) (core.Value, []string) {
	t.Helper()
	e, err := New(nil, append(mode, dialect...)...)
	require.NoError(t, err)

	var tags []string
	if withMark {
		require.NoError(t, e.Bind("mark", core.GoFunc{
			Name: "mark",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				if s, ok := args[0].(core.String); ok {
					tags = append(tags, s.V)
				}
				return args[1], nil
			},
		}))
	}

	v, err := e.Eval(context.Background(), "test", src)
	require.NoError(t, err)
	return v, tags
}

func condForm(dialect string, test, then, els string) string {
	if dialect == "clojure" {
		return fmt.Sprintf("(cond %s %s :else %s)", test, then, els)
	}
	return fmt.Sprintf("(cond (%s %s) (:else %s))", test, then, els)
}

func TestDialect_ConditionalTruthinessParity(t *testing.T) {
	modes := []struct {
		name string
		opts []EngineOption
	}{
		{"bytecode", []EngineOption{WithBytecode()}},
		{"treewalker", []EngineOption{WithTreeWalker()}},
	}
	dialects := []struct {
		name string
		opts []EngineOption
	}{
		{"identity", nil},
		{"cl", []EngineOption{WithDialect(cl.Dialect())}},
		{"clojure", []EngineOption{WithDialect(clojure.Dialect())}},
	}

	for _, mode := range modes {
		for _, dialect := range dialects {
			t.Run(mode.name+"/"+dialect.name, func(t *testing.T) {
				for _, tc := range falsyCases {
					t.Run("falsy/"+tc.src, func(t *testing.T) {
						v, _ := runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(if %s 11 22)", tc.src), false)
						assertValue(t, core.Int{V: 22}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, condForm(dialect.name, tc.src, "11", "22"), false)
						assertValue(t, core.Int{V: 22}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(when %s 11 12)", tc.src), false)
						assertValue(t, core.Nil{}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(and %s 9)", tc.src), false)
						assertValue(t, tc.want, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(or %s 9)", tc.src), false)
						assertValue(t, core.Int{V: 9}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(not %s)", tc.src), false)
						assertValue(t, core.Bool{V: true}, v)
					})
				}

				for _, tc := range truthyCases {
					t.Run("truthy/"+tc.src, func(t *testing.T) {
						v, _ := runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(if %s 11 22)", tc.src), false)
						assertValue(t, core.Int{V: 11}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, condForm(dialect.name, tc.src, "11", "22"), false)
						assertValue(t, core.Int{V: 11}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(when %s 11 12)", tc.src), false)
						assertValue(t, core.Int{V: 12}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(and %s 9)", tc.src), false)
						assertValue(t, core.Int{V: 9}, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(or %s 9)", tc.src), false)
						assertValue(t, tc.want, v)

						v, _ = runCase(t, mode.opts, dialect.opts, fmt.Sprintf("(not %s)", tc.src), false)
						assertValue(t, core.Bool{V: false}, v)
					})
				}

				t.Run("zero-operands", func(t *testing.T) {
					v, _ := runCase(t, mode.opts, dialect.opts, "(and)", false)
					assertValue(t, core.Bool{V: true}, v)

					v, _ = runCase(t, mode.opts, dialect.opts, "(or)", false)
					assertValue(t, core.Nil{}, v)

					v, _ = runCase(t, mode.opts, dialect.opts, "(and 1 2)", false)
					assertValue(t, core.Int{V: 2}, v)

					v, _ = runCase(t, mode.opts, dialect.opts, "(or nil false)", false)
					assertValue(t, core.Bool{V: false}, v)

					v, _ = runCase(t, mode.opts, dialect.opts, "(cond)", false)
					assertValue(t, core.Nil{}, v)

					v, _ = runCase(t, mode.opts, dialect.opts, "(if false 11)", false)
					assertValue(t, core.Nil{}, v)
				})

				t.Run("side-effects", func(t *testing.T) {
					// if records only the selected branch.
					v, tags := runCase(t, mode.opts, dialect.opts, `(if nil (mark "then" 11) (mark "else" 22))`, true)
					assertValue(t, core.Int{V: 22}, v)
					require.Equal(t, []string{"else"}, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, `(if 0 (mark "then" 11) (mark "else" 22))`, true)
					assertValue(t, core.Int{V: 11}, v)
					require.Equal(t, []string{"then"}, tags)

					// cond stops after the selected clause.
					v, tags = runCase(t, mode.opts, dialect.opts, condForm(dialect.name, `nil`, `(mark "c1" 11)`, `(mark "else" 22)`), true)
					assertValue(t, core.Int{V: 22}, v)
					require.Equal(t, []string{"else"}, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, condForm(dialect.name, `0`, `(mark "c1" 11)`, `(mark "else" 22)`), true)
					assertValue(t, core.Int{V: 11}, v)
					require.Equal(t, []string{"c1"}, tags)

					// when records no body for falsy, both bodies in order for truthy.
					v, tags = runCase(t, mode.opts, dialect.opts, `(when nil (mark "b1" 11) (mark "b2" 12))`, true)
					assertValue(t, core.Nil{}, v)
					require.Empty(t, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, `(when 0 (mark "b1" 11) (mark "b2" 12))`, true)
					assertValue(t, core.Int{V: 12}, v)
					require.Equal(t, []string{"b1", "b2"}, tags)

					// and skips its second operand for falsy.
					v, tags = runCase(t, mode.opts, dialect.opts, `(and nil (mark "second" 9))`, true)
					assertValue(t, core.Nil{}, v)
					require.Empty(t, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, `(and 0 (mark "second" 9))`, true)
					assertValue(t, core.Int{V: 9}, v)
					require.Equal(t, []string{"second"}, tags)

					// or skips its second operand for truthy.
					v, tags = runCase(t, mode.opts, dialect.opts, `(or 0 (mark "second" 9))`, true)
					assertValue(t, core.Int{V: 0}, v)
					require.Empty(t, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, `(or nil (mark "second" 9))`, true)
					assertValue(t, core.Int{V: 9}, v)
					require.Equal(t, []string{"second"}, tags)

					// not evaluates its operand exactly once.
					v, tags = runCase(t, mode.opts, dialect.opts, `(not (mark "operand" nil))`, true)
					assertValue(t, core.Bool{V: true}, v)
					require.Equal(t, []string{"operand"}, tags)

					v, tags = runCase(t, mode.opts, dialect.opts, `(not (mark "operand" 0))`, true)
					assertValue(t, core.Bool{V: false}, v)
					require.Equal(t, []string{"operand"}, tags)
				})
			})
		}
	}
}

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func evalTruthiness(t *testing.T, d core.Dialect, src string) core.Value {
	t.Helper()
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()
	got, err := e.Eval(context.Background(), "truth", src)
	require.NoError(t, err)
	return got
}

func TestDialect_Truthiness_IfFalseIsFalsy(t *testing.T) {
	got := evalTruthiness(t, core.FullDialect(), "(if false :yes :no)")
	assert.True(t, core.Keyword{V: "no"}.Equals(got), "false is falsy")
}

func TestDialect_Truthiness_AllConditionalForms(t *testing.T) {
	d := core.FullDialect()

	cases := []struct {
		name string
		src  string
		want core.Value
	}{
		{"when", "(when false :yes)", core.Nil{}},
		{"unless", "(unless false :yes)", core.Keyword{V: "yes"}},
		{"cond", "(cond (false :a) (true :b))", core.Keyword{V: "b"}},
		{"and", "(and false :y)", core.Bool{V: false}},
		{"or", "(or false :y)", core.Keyword{V: "y"}},
		{"not", "(not false)", core.Bool{V: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalTruthiness(t, d, tc.src)
			assert.True(t, tc.want.Equals(got), "%s: got %v", tc.name, got)
		})
	}
}

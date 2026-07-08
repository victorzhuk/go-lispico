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

// The truthiness axis diverges only on false: a nil-only dialect treats false
// as a true value, the default dialect keeps it falsy.
func TestDialect_Truthiness_IfDivergesOnFalse(t *testing.T) {
	nilOnly := evalTruthiness(t, core.FullDialect().NilOnlyFalsy(), "(if false :yes :no)")
	assert.True(t, core.Keyword{V: "yes"}.Equals(nilOnly), "nil-only: false is truthy")

	def := evalTruthiness(t, core.FullDialect(), "(if false :yes :no)")
	assert.True(t, core.Keyword{V: "no"}.Equals(def), "default: false is falsy")
}

func TestDialect_Truthiness_AllConditionalForms(t *testing.T) {
	nilOnly := core.FullDialect().NilOnlyFalsy()
	def := core.FullDialect()

	cases := []struct {
		name        string
		src         string
		wantNilOnly core.Value
		wantDefault core.Value
	}{
		{"when", "(when false :yes)", core.Keyword{V: "yes"}, core.Nil{}},
		{"unless", "(unless false :yes)", core.Nil{}, core.Keyword{V: "yes"}},
		{"cond", "(cond (false :a) (true :b))", core.Keyword{V: "a"}, core.Keyword{V: "b"}},
		{"and", "(and false :y)", core.Keyword{V: "y"}, core.Bool{V: false}},
		{"or", "(or false :y)", core.Bool{V: false}, core.Keyword{V: "y"}},
		{"not", "(not false)", core.Bool{V: false}, core.Bool{V: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalTruthiness(t, nilOnly, tc.src)
			assert.True(t, tc.wantNilOnly.Equals(got), "nil-only %s: got %v", tc.name, got)

			got = evalTruthiness(t, def, tc.src)
			assert.True(t, tc.wantDefault.Equals(got), "default %s: got %v", tc.name, got)
		})
	}
}

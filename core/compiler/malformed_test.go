package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// Every malformed special form must be rejected with a typed *core.LispicoError
// before any operand indexing — the compiler never panics on bad arity or shape.
func TestCompiler_MalformedForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		form core.Value
	}{
		{"if no args", mlist("if")},
		{"if cond only", mlist("if", msym("x"))},
		{"let no args", mlist("let")},
		{"when no args", mlist("when")},
		{"unless no args", mlist("unless")},
		{"fn no args", mlist("fn")},
		{"fn non-vector params", mlist("fn", core.Int{V: 42})},
		{"set! one arg", mlist("set!", msym("x"))},
		{"set! non-symbol name", mlist("set!", core.Int{V: 42}, core.Int{V: 1})},
		{"try no catch", mlist("try")},
		{"try body no catch", mlist("try", core.Int{V: 5})},
		{"defn no params", mlist("defn", msym("foo"))},
		{"def one arg", mlist("def", core.Int{V: 42})},
		{"def non-symbol name", mlist("def", core.Int{V: 42}, core.Int{V: 1})},
		{"loop no body", mlist("loop", mvec())},
		{"loop no args", mlist("loop")},
		{"quote no args", mlist("quote")},
		{"catch outside try", mlist("catch", msym("e"), msym("e"))},
		{"throw no args", mlist("throw")},
		{"throw two args", mlist("throw", core.Int{V: 1}, core.Int{V: 2})},
		{"quasiquote no args", mlist("quasiquote")},
		{"quasiquote two args", mlist("quasiquote", core.Int{V: 1}, core.Int{V: 2})},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewCompiler("test")
			err := c.Compile(tc.form)
			require.Error(t, err, "malformed form must error, not panic")
			var le *core.LispicoError
			require.ErrorAs(t, err, &le, "malformed form must return *core.LispicoError")
			assert.Equal(t, CodeCompileError, le.Code)
		})
	}
}

func msym(v string) core.Symbol { return core.Symbol{V: v} }

func mlist(items ...any) core.List {
	out := make([]core.Value, 0, len(items))
	for _, it := range items {
		out = append(out, mval(it))
	}
	return core.List{Items: out}
}

func mvec(items ...any) core.Vector {
	out := make([]core.Value, 0, len(items))
	for _, it := range items {
		out = append(out, mval(it))
	}
	return core.Vector{Items: out}
}

func mval(it any) core.Value {
	switch v := it.(type) {
	case core.Value:
		return v
	case string:
		return msym(v)
	case int:
		return core.Int{V: int64(v)}
	}
	return core.Nil{}
}

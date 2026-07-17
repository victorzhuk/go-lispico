package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// Red→green hardening table: every malformed special form must surface a typed
// *core.LispicoError through Engine.Eval under WithBytecode() and never panic
// on out-of-bounds operand indexing during compilation.
func TestBytecodeRuntime_MalformedForms(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	cases := []struct {
		name string
		src  string
	}{
		{"if no args", "(if)"},
		{"if cond only", "(if x)"},
		{"let no args", "(let)"},
		{"when no args", "(when)"},
		{"unless no args", "(unless)"},
		{"fn no args", "(fn)"},
		{"fn non-vector params", "(fn 42)"},
		{"set! one arg", "(set! x)"},
		{"set! non-symbol name", "(set! 42 1)"},
		{"try no catch", "(try)"},
		{"try body no catch", "(try 5)"},
		{"defn no params", "(defn foo)"},
		{"def one arg", "(def 42)"},
		{"def non-symbol name", "(def 42 1)"},
		{"loop no body", "(loop [])"},
		{"loop no args", "(loop)"},
		{"quote no args", "(quote)"},
		{"catch outside try", "(catch e e)"},
		{"throw no args", "(throw)"},
		{"throw two args", "(throw 1 2)"},
		{"quasiquote no args", "(quasiquote)"},
		{"quasiquote two args", "(quasiquote 1 2)"},
		{"let non-vector bindings", "(let 5 1)"},
		{"let odd binding count", "(let [x] 1)"},
		{"let* non-vector bindings", "(let* 5 1)"},
		{"let* odd binding count", "(let* [x] 1)"},
		{"recur outside loop", "(recur 1)"},
		{"not no args", "(not)"},
		{"not two args", "(not 1 2)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			_, err := eng.Eval(ctx, tc.name, tc.src)
			require.Error(t, err, "malformed form must error, not panic")
			var le *core.LispicoError
			if !assert.ErrorAs(t, err, &le, "malformed form must surface *core.LispicoError") {
				t.Logf("got %T: %v", err, err)
			}
		})
	}
}

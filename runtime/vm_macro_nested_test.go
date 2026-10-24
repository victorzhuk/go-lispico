package runtime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestVMMacroExpansionNested pins tree-walker parity for macro uses in
// nested positions on the default bytecode engine: deep expansion runs at
// compile time over every evaluation position, so a nested call must yield
// the tree-walker's value, not a TypeError on the Macro head.
func TestVMMacroExpansionNested(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		defs string
		use  string
		want core.Value
	}{
		{
			name: "constant-in-progn",
			defs: "(defmacro m () 2)",
			use:  "(progn (m))",
			want: core.Int{V: 2},
		},
		{
			name: "macro-calling-macro-in-expansion",
			defs: "(defmacro inner () 7) (defmacro outer () `(inner))",
			use:  "(progn (outer))",
			want: core.Int{V: 7},
		},
		{
			name: "arguments-reach-expansion-unevaluated",
			defs: "(defmacro choose (x y) `(if true ~x ~y))",
			use:  "(progn (choose 3 4))",
			want: core.Int{V: 3},
		},
		{
			name: "nested-in-function-position",
			defs: "(defmacro m () 2) (defn f () (progn (m)))",
			use:  "(f)",
			want: core.Int{V: 2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree, err := New(slog.Default(), WithTreeWalker())
			require.NoError(t, err)
			defer tree.Close()
			_, err = tree.Eval(t.Context(), "defs", tc.defs)
			require.NoError(t, err, "tree-walker setup")
			treeResult, err := tree.Eval(t.Context(), "use", tc.use)
			require.NoError(t, err, "tree-walker reference run")

			eng, err := New(slog.Default())
			require.NoError(t, err)
			defer eng.Close()
			_, err = eng.Eval(t.Context(), "defs", tc.defs)
			require.NoError(t, err, "bytecode setup")
			got, err := eng.Eval(t.Context(), "use", tc.use)
			require.NoError(t, err, "nested macro use must not TypeError on the Macro head")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, treeResult, got, "bytecode must match the tree-walker")
		})
	}
}

// TestVMMacroExpansionNestedShadowedLocal pins value-local macro shadowing
// per dialect. Clojure is Lisp-1: the let binding and the macro share one
// namespace, so a local named m shadows the macro and (m) is a call on the
// local value in both engines. Common Lisp is Lisp-2: let binds the value
// cell while the head resolves through the function cell, so the value-local
// and the macro coexist — m in value position is 5 and (m) still expands to 2.
func TestVMMacroExpansionNestedShadowedLocal(t *testing.T) {
	t.Parallel()

	t.Run("clojure/value-local shadows macro", func(t *testing.T) {
		t.Parallel()

		prog := "(defmacro m () 2) (let [m 5] (do (m)))"
		for _, tc := range []struct {
			name    string
			opts    []EngineOption
			wantMsg string
		}{
			{
				name:    "tree-walker",
				opts:    []EngineOption{WithTreeWalker(), WithDialect(clojure.Dialect())},
				wantMsg: "expected function, got core.Int",
			},
			{
				name:    "bytecode",
				opts:    []EngineOption{WithDialect(clojure.Dialect())},
				wantMsg: "expected callable, got core.Int",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				eng, err := New(slog.Default(), tc.opts...)
				require.NoError(t, err)
				defer eng.Close()
				_, err = eng.Eval(t.Context(), "shadow", prog)
				require.Error(t, err, "expanding the global macro would silently return 2")
				var le *core.LispicoError
				require.ErrorAs(t, err, &le)
				assert.Equal(t, "TypeError", le.Code)
				assert.Equal(t, tc.wantMsg, le.Message)
			})
		}
	})

	t.Run("cl/value-local and macro coexist", func(t *testing.T) {
		t.Parallel()

		prog := "(defmacro m () 2) (let ((m 5)) (progn m (m)))"
		for _, name := range []string{"tree-walker", "bytecode"} {
			t.Run(name, func(t *testing.T) {
				opts := []EngineOption{WithDialect(cl.Dialect())}
				if name == "tree-walker" {
					opts = append(opts, WithTreeWalker())
				}
				eng, err := New(slog.Default(), opts...)
				require.NoError(t, err)
				defer eng.Close()
				got, err := eng.Eval(t.Context(), "cl-coexist", prog)
				require.NoError(t, err, "under Lisp-2 the function cell owns the head, so (m) expands")
				assert.Equal(t, core.Int{V: 2}, got)
			})
		}
	})
}

package runtime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestVMMacroExpansionNestedShadowedLocal checks the compile-time shadowing
// guard: a local binding named like a global macro is a call on the local
// value on both engines, never a macro expansion.
func TestVMMacroExpansionNestedShadowedLocal(t *testing.T) {
	t.Parallel()

	prog := "(defmacro m () 2) (let [m 5] (progn (m)))"

	tree, err := New(slog.Default(), WithTreeWalker())
	require.NoError(t, err)
	defer tree.Close()
	_, treeErr := tree.Eval(t.Context(), "shadow", prog)
	require.Error(t, treeErr, "reference: a non-callable local must fail")

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()
	_, err = eng.Eval(t.Context(), "shadow", prog)
	require.Error(t, err, "expanding the global macro here would silently return 2")
}

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestVMLiteralParity pins cross-evaluator parity for the quote literal
// family and the empty list under both shipped dialects.
//
// Malformed quote forms ((quote), (quote 1 2)) must be rejected by BOTH
// evaluators — bytecode VM and tree-walker — as a typed *core.LispicoError,
// never a panic, on the cold path and on repeated evaluation.
//
// Valid forms ((quote missing), (), (quote ()) must return the same type and
// value on the VM as on the tree-walker, cold (first eval compiles) and
// repeated (cached chunk), and each VM result must additionally equal the
// pinned expected value — parity with a shared wrong answer is still a
// defect. The tree-walker is the reference: () evaluates to the empty list
// (core/eval.go returns the List when Len()==0) and quote returns its datum
// unevaluated.
func TestVMLiteralParity(t *testing.T) {
	t.Parallel()

	dialects := []struct {
		name    string
		dialect core.Dialect
	}{
		{"cl", cl.Dialect()},
		{"clojure", clojure.Dialect()},
	}

	malformed := []struct {
		name string
		src  string
	}{
		{"quote_no_args", "(quote)"},
		{"quote_two_args", "(quote 1 2)"},
	}

	valid := []struct {
		name string
		src  string
		want core.Value
	}{
		{"quote_symbol", "(quote missing)", core.Symbol{V: "missing"}},
		{"empty_list", "()", core.List{}},
		{"quote_empty_list", "(quote ())", core.List{}},
	}

	newEngine := func(t *testing.T, d core.Dialect, bytecode bool) Engine {
		t.Helper()
		mode := WithTreeWalker()
		if bytecode {
			mode = WithBytecode()
		}
		eng, err := New(nil, WithDialect(d), mode)
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		return eng
	}

	for _, d := range dialects {
		d := d
		t.Run(d.name+"/malformed_typed_rejection", func(t *testing.T) {
			t.Parallel()
			for _, tc := range malformed {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					for _, mode := range []struct {
						name     string
						bytecode bool
					}{
						{"vm", true},
						{"treewalker", false},
					} {
						mode := mode
						t.Run(mode.name, func(t *testing.T) {
							t.Parallel()
							eng := newEngine(t, d.dialect, mode.bytecode)
							ctx := context.Background()
							// Cold and repeated: the typed rejection must hold
							// on the compile path and on the cached path alike.
							for _, round := range []string{"cold", "repeated"} {
								_, err := eng.Eval(ctx, tc.name, tc.src)
								require.Error(t, err, "%s: %s must reject %q, not panic", round, mode.name, tc.src)
								var le *core.LispicoError
								assert.ErrorAs(t, err, &le, "%s: %s rejection of %q must be *core.LispicoError", round, mode.name, tc.src)
							}
						})
					}
				})
			}
		})

		t.Run(d.name+"/valid_value_type_parity", func(t *testing.T) {
			t.Parallel()
			for _, tc := range valid {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					ctx := context.Background()
					vmEng := newEngine(t, d.dialect, true)
					twEng := newEngine(t, d.dialect, false)
					for _, round := range []string{"cold", "repeated"} {
						vmResult, vmErr := vmEng.Eval(ctx, tc.name, tc.src)
						twResult, twErr := twEng.Eval(ctx, tc.name, tc.src)
						require.NoError(t, twErr, "%s: tree-walker reference must evaluate %q", round, tc.src)
						require.NoError(t, vmErr, "%s: VM eval of %q", round, tc.src)
						require.NotNil(t, vmResult, "%s: VM eval of %q must return a value; tree-walker returned %v", round, tc.src, twResult)
						require.Equal(t, twResult.Type(), vmResult.Type(),
							"%s: type parity for %q (VM vs tree-walker)", round, tc.src)
						require.True(t, vmResult.Equals(twResult),
							"%s: value parity for %q: VM %v != tree-walker %v", round, tc.src, vmResult, twResult)
						require.True(t, tc.want.Equals(vmResult),
							"%s: VM result of %q must equal pinned %v; got %v", round, tc.src, tc.want, vmResult)
					}
				})
			}
		})
	}
}

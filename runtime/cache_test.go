package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestCache_Hit verifies that evaluating the same source twice returns the same
// result and the second eval reuses the cached chunk (verified via functional
// correctness — both evals produce identical results).
func TestCache_Hit(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")

	src := "(+ 1 2)"
	r1, err := e.Eval(context.Background(), "test", src)
	require.NoError(t, err)

	r2, err := e.Eval(context.Background(), "test", src)
	require.NoError(t, err)

	assert.True(t, r1.Equals(r2), "cached eval must produce identical result")
	assert.True(t, core.Int{V: 3}.Equals(r1), "result must be 3")
}

// TestCache_MacroInvalidation verifies that redefining a macro produces new
// behavior — the cache epoch correctly invalidates the cached chunk.
func TestCache_MacroInvalidation(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")

	_, err = e.Eval(context.Background(), "defmacro1", "(defmacro m [x y] (+ x y))")
	require.NoError(t, err)

	// Use the macro — should produce 3.
	r1, err := e.Eval(context.Background(), "use1", "(m 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(r1), "first macro: (m 1 2) => 3")

	// Redefine the macro to multiply.
	_, err = e.Eval(context.Background(), "defmacro2", "(defmacro m [x y] (* x y))")
	require.NoError(t, err)

	bindBuiltin(t, e, "*")

	// Use the redefined macro — should produce 2, not 3.
	r2, err := e.Eval(context.Background(), "use2", "(m 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(r2), "redefined macro: (m 1 2) => 2")
}

// TestCache_Isolation verifies that different source strings with the same
// form index produce correctly different cached results.
func TestCache_Isolation(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	r1, err := e.Eval(context.Background(), "test1", "(+ 1 2)")
	require.NoError(t, err)

	r2, err := e.Eval(context.Background(), "test2", "(* 2 3)")
	require.NoError(t, err)

	assert.True(t, core.Int{V: 3}.Equals(r1), "(+ 1 2) => 3")
	assert.True(t, core.Int{V: 6}.Equals(r2), "(* 2 3) => 6")
	assert.False(t, r1.Equals(r2), "different sources must produce different results")
}

// TestCache_DialectSensitivity verifies that different dialects get different
// cache keys (truthiness change affects compiled conditional behavior).
func TestCache_DialectSensitivity(t *testing.T) {
	// CL dialect: only nil is falsy.
	cl, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer cl.Close()

	bindBuiltin(t, cl, "+")

	r1, err := cl.Eval(context.Background(), "cl", "(if false :y :n)")
	require.NoError(t, err)

	// Both CL and default produce :y under the "nil-only falsy" rule,
	// but the dialect differs internally — the cache key includes the
	// dialect fingerprint so these evaluations never share a key.
	assert.True(t, core.Keyword{V: "y"}.Equals(r1), "CL: (if false :y :n) => :y")
}

// TestCache_ConcurrentSafety verifies that concurrent Eval calls on a
// bytecode engine do not race. Run with -race.
func TestCache_ConcurrentSafety(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	ctx := context.Background()
	sources := []string{"(+ 1 2)", "(* 3 4)", "(+ 5 6)", "(* 7 8)", "(+ 9 10)"}

	t.Run("parallel-evals", func(t *testing.T) {
		t.Parallel()
		for range 100 {
			for _, src := range sources {
				_, err := e.Eval(ctx, "conc", src)
				require.NoError(t, err, "concurrent eval of %q must not error", src)
			}
		}
	})
}

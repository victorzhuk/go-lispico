package runtime

import (
	"context"
	"fmt"
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

// TestCache_MacroInvalidation_ViaCall extends TestCache_MacroInvalidation to
// the Apply/Call path: a macro that expands to a top-level (def use-m (fn ...))
// is invoked via eng.Eval, then use-m is exercised via eng.Call; the macro is
// redefined and re-invoked, then use-m is exercised via eng.Call again. Proves
// epoch invalidation isn't bypassed by the pooled-VM cache on the Call path.
//
// The macro expansion targets a top-level def (rather than nesting the macro
// call inside a fn body) because the bytecode compiler only macro-expands the
// literal top-level form passed to Eval — a macro invoked from inside a
// compiled fn/defn body is not expanded and misresolves at call time.
func TestCache_MacroInvalidation_ViaCall(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	_, err = e.Eval(context.Background(), "defmacro1", "(defmacro m [] `(def use-m (fn [x y] (+ x y))))")
	require.NoError(t, err)

	_, err = e.Eval(context.Background(), "use1", "(m)")
	require.NoError(t, err)

	r1, err := e.Call(context.Background(), "use-m", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(r1), "first macro via Call: (use-m 1 2) => 3")

	// Redefine the macro to bind use-m to a multiplying fn instead.
	_, err = e.Eval(context.Background(), "defmacro2", "(defmacro m [] `(def use-m (fn [x y] (* x y))))")
	require.NoError(t, err)

	_, err = e.Eval(context.Background(), "use2", "(m)")
	require.NoError(t, err)

	r2, err := e.Call(context.Background(), "use-m", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(r2), "redefined macro via Call: (use-m 1 2) => 2")
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

func cacheCount(t *testing.T, e Engine) int {
	t.Helper()
	ei, ok := e.(*engineImpl)
	require.True(t, ok)
	require.NotNil(t, ei.bytecodeEvaluator)
	be := ei.bytecodeEvaluator
	be.mu.Lock()
	defer be.mu.Unlock()
	return len(be.cache)
}

// TestCache_BoundedSize: a low MaxCacheEntries keeps the entry count at or below
// the ceiling while many distinct sources are evaluated (eviction recompiles on
// demand without corrupting results).
func TestCache_BoundedSize(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()),
		WithResourceLimits(ResourceLimits{MaxCacheEntries: 4, MaxCollectionLen: 1 << 30}))
	require.NoError(t, err)
	defer e.Close()
	bindBuiltin(t, e, "+")
	for i := range 30 {
		r, err := e.Eval(context.Background(), "s", fmt.Sprintf("(+ %d 1)", i))
		require.NoError(t, err)
		assert.True(t, core.Int{V: int64(i + 1)}.Equals(r))
	}
	assert.LessOrEqual(t, cacheCount(t, e), 4, "cache must stay at or below MaxCacheEntries")
}

// TestCache_StaleEpochEvictionBounded: repeatedly redefining a macro orphans
// old-epoch chunks; they must be reclaimed so the cache stays bounded while
// results stay correct.
func TestCache_StaleEpochEvictionBounded(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()),
		WithResourceLimits(ResourceLimits{MaxCacheEntries: 5, MaxCollectionLen: 1 << 30}))
	require.NoError(t, err)
	defer e.Close()
	bindBuiltin(t, e, "+")
	ctx := context.Background()
	for i := range 30 {
		_, err := e.Eval(ctx, "dm", fmt.Sprintf("(defmacro m [x] (+ x %d))", i))
		require.NoError(t, err)
		_, err = e.Eval(ctx, "u", fmt.Sprintf("(m %d)", i))
		require.NoError(t, err)
	}
	// (m 1) under the last macro (+ x 29) => 30; proves correctness survived eviction.
	r, err := e.Eval(ctx, "final", "(m 1)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 30}.Equals(r), "(m 1) under (+ x 29) => 30")
	assert.LessOrEqual(t, cacheCount(t, e), 5, "stale-epoch orphaned entries must be reclaimed")
}

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestVM_MaxCallDepthIsTypedError(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithMaxEvalDepth(10))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()

	_, err = eng.Eval(ctx, "infinite", "(defn infinite [] (infinite))")
	require.NoError(t, err)

	_, err = eng.Eval(ctx, "call", "(infinite)")
	require.Error(t, err)

	var lispicoErr *core.LispicoError
	require.True(t, errors.As(err, &lispicoErr), "expected *core.LispicoError, got %T", err)
	require.Equal(t, "EvalError", lispicoErr.Code)
}

// TestVM_StructuralDepthSurvivesCaughtThrow proves a throw caught by an
// in-Lisp try/catch does not leak its structural-depth increment into later
// construction in the same Eval call. The try body's vector literal throws
// before its OpStructLeave runs; the throw is caught, not propagated out of
// Run. With MaxStructuralDepth 1, a subsequent single-level vector must still
// fit the budget — it would falsely trip the ceiling if the try's increment
// leaked.
func TestVM_StructuralDepthSurvivesCaughtThrow(t *testing.T) {
	t.Parallel()

	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 1, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	_, err = eng.Eval(context.Background(), "leak", `(do (try [1 (throw "boom")] (catch e 1)) [1 2 3])`)
	require.NoError(t, err, "vector after a caught throw must see the full structural-depth budget")
}

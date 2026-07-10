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

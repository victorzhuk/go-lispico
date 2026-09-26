package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestDialect_RejectsEmptyAdapterID asserts NewDialect refuses an adapter
// that carries no semantic ID, so no engine can be built from it.
func TestDialect_RejectsEmptyAdapterID(t *testing.T) {
	noop := core.GoFunc{
		Name: "x-noop",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, nil
		},
	}
	_, err := core.NewDialect(spec{Adapters: map[string]core.Adapter{"x": {ID: "", Value: noop}}})
	require.Error(t, err, "NewDialect must reject an adapter whose ID is empty")
	require.ErrorContains(t, err, "has no semantic ID")
}

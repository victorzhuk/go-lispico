package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestNew_RejectsEmptyAdapterID asserts engine construction refuses a dialect
// whose adapter entry carries no semantic ID, at New before any plugin load.
func TestNew_RejectsEmptyAdapterID(t *testing.T) {
	noop := core.GoFunc{
		Name: "x-noop",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, nil
		},
	}
	d := core.FullDialect().WithAdapter("x", "", noop)
	_, err := New(nil, WithDialect(d))
	require.Error(t, err, "New must reject a dialect adapter entry whose AdapterID is empty")
	require.ErrorContains(t, err, "has no semantic ID")
}

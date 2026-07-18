package goldset

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGoldset runs every fixture under both execution modes against its
// golden. Neither mode is the oracle: the golden is the independent expected
// result (ADR 0008).
func TestGoldset(t *testing.T) {
	t.Parallel()

	fixtures, err := Fixtures()
	require.NoError(t, err)

	for _, mode := range Modes {
		for _, fx := range fixtures {
			t.Run(string(mode)+"/"+fx.Name, func(t *testing.T) {
				t.Parallel()
				eng, err := NewEngine(mode)
				require.NoError(t, err)
				t.Cleanup(func() { _ = eng.Close() })

				got, err := eng.Eval(context.Background(), fx.Name, fx.Source)
				require.NoError(t, err)
				require.Equal(t, fx.Want, got.String())
			})
		}
	}
}

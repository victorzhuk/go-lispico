package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

// newDialectEngine builds an engine over an explicit dialect, mirroring
// newEngine's construction but letting the control engine use the identity
// (default) dialect.
func newDialectEngine(t *testing.T, d core.Dialect, opts ...runtime.EngineOption) runtime.Engine {
	t.Helper()
	options := append([]runtime.EngineOption{runtime.WithDialect(d)}, opts...)
	e, err := runtime.New(nil, options...)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	require.NoError(t, e.Use(stdlib.New()))
	return e
}

// TestCL_UnrenamedBuiltinsKeepStdlibBinding pins the contract that cons, list,
// reverse, apply and type are callable under the CL dialect exactly as under
// the default dialect, that their materialized cells carry the same canonical
// flags (including the Lisp-2 function cell), and that the CL dialect
// vocabulary no longer carries identity entries for them.
func TestCL_UnrenamedBuiltinsKeepStdlibBinding(t *testing.T) {
	modes := []struct {
		name string
		opts []runtime.EngineOption
	}{
		{"treewalker", []runtime.EngineOption{runtime.WithTreeWalker()}},
		{"bytecode", []runtime.EngineOption{runtime.WithBytecode()}},
	}

	probes := []struct {
		name string
		src  string
	}{
		{"cons", "(cons 1 '(2))"},
		{"list", "(list 1 2)"},
		{"reverse", "(reverse '(1 2))"},
		{"apply", "(apply list '(1 2))"},
		{"type", "(type 1)"},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			for _, probe := range probes {
				t.Run(probe.name, func(t *testing.T) {
					ctx := context.Background()
					clEng := newDialectEngine(t, cl.Dialect(), mode.opts...)
					ctrlEng := newDialectEngine(t, core.Dialect{}, mode.opts...)

					clGot, err := clEng.Eval(ctx, "cl", probe.src)
					require.NoError(t, err, "%s: %s must evaluate under CL", mode.name, probe.src)
					ctrlGot, err := ctrlEng.Eval(ctx, "control", probe.src)
					require.NoError(t, err, "%s: %s must evaluate under the default dialect", mode.name, probe.src)
					assert.True(t, ctrlGot.Equals(clGot),
						"%s: %s must be control-identical: control %v, CL %v",
						mode.name, probe.src, ctrlGot, clGot)

					// (b) Materialized cells after the probe.
					clRoot := clEng.RootEnv()
					ctrlRoot := ctrlEng.RootEnv()

					clVal, clOK, clCanon := clRoot.GetMaterializedCanonical(probe.name)
					ctrlVal, ctrlOK, ctrlCanon := ctrlRoot.GetMaterializedCanonical(probe.name)
					require.True(t, clOK, "%s: %s must be materialized in the CL value cell", mode.name, probe.name)
					require.True(t, ctrlOK, "%s: %s must be materialized in the control value cell", mode.name, probe.name)
					assert.Equal(t, ctrlCanon, clCanon,
						"%s: %s value-cell canonical flag must equal the control's", mode.name, probe.name)
					assert.True(t, ctrlVal.Equals(clVal),
						"%s: %s value-cell value must equal the control's: control %v, CL %v",
						mode.name, probe.name, ctrlVal, clVal)

					funcVal, funcOK, funcCanon := clRoot.GetMaterializedFuncCanonical(probe.name)
					require.True(t, funcOK,
						"%s: %s must be materialized in the CL Lisp-2 function cell", mode.name, probe.name)
					assert.Equal(t, clCanon, funcCanon,
						"%s: %s function-cell canonical flag must equal the value cell's", mode.name, probe.name)
					assert.True(t, funcVal.Equals(clVal),
						"%s: %s function-cell value must equal the value cell's", mode.name, probe.name)
				})
			}
		})
	}
	t.Run("vocab", func(t *testing.T) {
		vocab := cl.Dialect().Vocab()
		for _, name := range []string{"cons", "list", "reverse", "apply", "type"} {
			_, ok := cl.Dialect().VocabEntry(name)
			assert.False(t, ok, "%s must not appear in the CL vocabulary", name)
		}
		for name, entry := range vocab {
			assert.NotEqual(t, name, entry.Canonical,
				"vocabulary entry %q must not be an identity rename to %q", name, entry.Canonical)
		}
	})
}

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

type spec = core.DialectSpec

var newDialect = core.NewDialect

func buildDialect(s spec) (core.Dialect, error) { return newDialect(s) }

func mustDialect(t *testing.T, s spec) core.Dialect {
	t.Helper()
	d, err := buildDialect(s)
	require.NoError(t, err, "NewDialect")
	return d
}

func TestNewDialect_SpecDialectBothEvaluators(t *testing.T) {
	d := mustDialect(t, spec{
		Forms: map[string]string{"si": "if"},
		Hide:  []string{"if"},
		Vocab: map[string]string{"car": "first"},
	})

	modes := []struct {
		name string
		opt  EngineOption
	}{
		{name: "tree-walker", opt: WithTreeWalker()},
		{name: "vm", opt: WithBytecode()},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			e, err := New(nil, mode.opt, WithDialect(d))
			require.NoError(t, err)
			t.Cleanup(func() { _ = e.Close() })
			require.NoError(t, e.Use(stdlib.New()))

			got, err := e.Eval(context.Background(), "spec", "(si false 1 2)")
			require.NoError(t, err)
			assert.True(t, core.Int{V: 2}.Equals(got), "(si false 1 2) = %v, want 2", got)

			got, err = e.Eval(context.Background(), "spec", "(car (quote (7 8)))")
			require.NoError(t, err)
			assert.True(t, core.Int{V: 7}.Equals(got), "(car (quote (7 8))) = %v, want 7", got)

			_, err = e.Eval(context.Background(), "spec", "(if false 1 2)")
			require.Error(t, err, "hidden if must be uncallable")
		})
	}
}

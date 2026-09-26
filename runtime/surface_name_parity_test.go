package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// TestSurfaceNameCondParity pins cond body evaluation across both evaluator
// paths without a surface `do` name: a Hide-do dialect and an empty-base
// dialect exposing only if/cond. Multi-body nested clauses must yield the
// last body value under tree-walker and VM alike; the cond normalizer must
// not route multi-expression bodies through a visible `do` symbol.
func TestSurfaceNameCondParity(t *testing.T) {
	modes := []struct {
		name string
		opt  EngineOption
	}{
		{name: "tree-walker", opt: WithTreeWalker()},
		{name: "vm", opt: WithBytecode()},
	}

	hideDo, err := core.NewDialect(core.DialectSpec{Hide: []string{"do"}})
	require.NoError(t, err, "NewDialect Hide-do")

	emptyBase, err := core.NewDialect(core.DialectSpec{
		Base:  core.BaseEmpty,
		Forms: map[string]string{"if": "if", "cond": "cond"},
	})
	require.NoError(t, err, "NewDialect empty-base")

	// Each row evaluates a multi-body cond and expects the last body value,
	// identical under both evaluator paths. The empty-base dialect needs no
	// stdlib: tests and bodies are self-evaluating.
	dialects := []struct {
		name    string
		d       core.Dialect
		stdlib  bool
		rows    []struct {
			name string
			src  string
			want core.Value
		}
	}{
		{
			name:   "hide-do",
			d:      hideDo,
			stdlib: true,
			rows: []struct {
				name string
				src  string
				want core.Value
			}{
				{name: "multi-body", src: "(cond (true 1 2))", want: core.Int{V: 2}},
			},
		},
		{
			name:   "empty-base",
			d:      emptyBase,
			stdlib: false,
			rows: []struct {
				name string
				src  string
				want core.Value
			}{
				{name: "multi-body self-evaluating", src: "(cond (1 2 3))", want: core.Int{V: 3}},
			},
		},
	}

	for _, tc := range dialects {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range modes {
				t.Run(mode.name, func(t *testing.T) {
					e, err := New(nil, WithDialect(tc.d), mode.opt)
					require.NoError(t, err)
					t.Cleanup(func() { _ = e.Close() })
					if tc.stdlib {
						require.NoError(t, e.Use(stdlib.New()))
					}

					for _, row := range tc.rows {
						t.Run(row.name, func(t *testing.T) {
							got, err := e.Eval(context.Background(), "cond-parity", row.src)
							require.NoError(t, err, "%s/%s: %s", tc.name, mode.name, row.src)
							assert.True(t, row.want.Equals(got),
								"%s/%s: %s = %v, want %v", tc.name, mode.name, row.src, got, row.want)
						})
					}
				})
			}
		})
	}
}

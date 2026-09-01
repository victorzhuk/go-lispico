package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// TestCLAdapters_EmptyBaseNoAdapters: an empty-base dialect exposes no CL
// collection adapter names — only the forms its delta explicitly adds are
// callable, and stdlib names absent from the allowlist are undefined.

func TestCLAdapters_EmptyBaseNoAdapters(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote")
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	ctx := context.Background()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"nth", "(nth 0 nil)"},
		{"mapcar", "(mapcar 0 nil)"},
		{"sort", "(sort nil nil)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Eval(ctx, tc.name, tc.src)
			require.Error(t, err, "%s must be undefined under an empty-base allowlist", tc.name)
			assert.Contains(t, err.Error(), "undefined", "expected an undefined-name error, got: %v", err)
		})
	}

	got, err := e.Eval(ctx, "control", "(if true 7 8)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "allowlisted special forms remain callable")
}

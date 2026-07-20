package stdlib

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func TestBootstrapReusableEntriesExpandDeterministically(t *testing.T) {
	ctx := context.Background()
	env1 := setupEnv(t)
	env2 := setupEnv(t)
	macro1 := core.NewEvaluator()
	macro2 := core.NewEvaluator()

	reusable := 0
	for _, entry := range stdlibBootstrapEntries() {
		forms1, err := core.Read(entry.source)
		require.NoError(t, err)
		forms2, err := core.Read(entry.source)
		require.NoError(t, err)
		require.Len(t, forms2, len(forms1))

		for i := range forms1 {
			expanded1, err := macro1.MacroExpand(ctx, forms1[i], env1)
			require.NoError(t, err)
			expanded2, err := macro2.MacroExpand(ctx, forms2[i], env2)
			require.NoError(t, err)
			assert.True(t, expanded1.Equals(expanded2), "expansion mismatch for %q", entry.source)
			if entry.reusable {
				reusable++
				assertReusableBootstrapFormCompiles(t, expanded1)
			}
		}

		if strings.HasPrefix(entry.source, "(defmacro ") {
			assert.False(t, entry.reusable, "macro definitions capture their defining env")
		}
	}
	assert.Equal(t, 1, reusable)
}

func assertReusableBootstrapFormCompiles(t *testing.T, form core.Value) {
	t.Helper()
	comp := compiler.NewCompiler("<stdlib-bootstrap-test>")
	require.NoError(t, comp.Compile(form))
	comp.Chunk().Emit(vm.OpReturn, 0)
	comp.MarkCaptures()
	require.NoError(t, comp.Chunk().Validate())
}

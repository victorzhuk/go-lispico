package stdlib

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestBootstrapEntriesExpandDeterministically(t *testing.T) {
	ctx := context.Background()
	env1 := setupEnv(t)
	env2 := setupEnv(t)
	macro1 := core.NewEvaluator()
	macro2 := core.NewEvaluator()

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
		}
	}
}

// Every bootstrap entry is a macro definition, and a macro captures its
// defining environment — so no entry can be published as a shared compiled
// artifact, whatever reuse policy the loader grows.
func TestBootstrapEntries_AllMacroDefinitionsNoReusePolicy(t *testing.T) {
	entries := stdlibBootstrapEntries()
	require.Len(t, entries, 5)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.name)
		assert.True(t, strings.HasPrefix(entry.source, "(defmacro "),
			"bootstrap entry %q is not a top-level macro definition", entry.name)
	}

	assert.Equal(t, []string{"->", "->>", "as->", "if-let", "when-let"}, names)
}

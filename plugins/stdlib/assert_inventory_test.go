package stdlib

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// reconcileResult only reports rows the source demands and the table lacks, so a
// row left behind for a deleted branch reconciles clean. This pins the other
// direction for assert: exactly the branches the builtin still returns from.
func TestResultInventory_AssertBranchesMatchTheFixedCode(t *testing.T) {
	var labels []string
	for _, row := range inventory.ResultBranches {
		if row.Fn == "assert" {
			labels = append(labels, row.BranchLabel)
		}
	}
	sort.Strings(labels)

	require.Equal(t, []string{
		"arity error return",
		"bare failure return",
		"message render error return",
		"string failure return",
		"success nil return",
		"value failure return",
	}, labels)
}

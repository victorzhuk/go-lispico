package cl_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
)

// TestDialect_Forms_CLRenamesAndLisp2Intrinsics pins scenario 2: the CL
// dialect enumerates its renames plus the Lisp-2 intrinsics, without the
// hidden kernel names, sorted.
func TestDialect_Forms_CLRenamesAndLisp2Intrinsics(t *testing.T) {
	forms := cl.Dialect().Forms()
	for _, name := range []string{"defun", "setq", "progn", "funcall", "function"} {
		require.Contains(t, forms, name)
	}
	require.NotContains(t, forms, "set!")
	require.NotContains(t, forms, "do")
	require.True(t, slices.IsSorted(forms), "Forms() = %v, want sorted", forms)
}

package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// bindPrebuiltSubject binds an n-element descending integer list under name,
// built in Go rather than by a Lisp builtin.
//
// A subject a test builds in Lisp charges its own construction against the
// reduction ceiling, so a low ceiling can be crossed while the input is still
// being assembled and never reach the operation the test is about. Handing the
// subject over prebuilt leaves nothing but a symbol lookup ahead of that
// operation, so the ceiling governs the operation alone regardless of what any
// builtin charges to construct a list.
func bindPrebuiltSubject(t *testing.T, eng Engine, name string, n int) {
	t.Helper()
	elems := make([]core.Value, n)
	for i := range elems {
		elems[i] = core.Int{V: int64(n - i)}
	}
	require.NoError(t, eng.Bind(name, core.NewList(elems)))
}

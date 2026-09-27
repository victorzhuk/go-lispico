package clojure_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
)

// TestDialect_Forms_CallerMutationDoesNotLeak pins scenario 4: Forms returns a
// caller-owned copy; mutating it never reaches the process-wide singleton.
func TestDialect_Forms_CallerMutationDoesNotLeak(t *testing.T) {
	first := clojure.Dialect().Forms()
	require.NotEmpty(t, first)
	want := slices.Clone(first)
	first[0] = "tampered"
	require.Equal(t, want, clojure.Dialect().Forms())
}

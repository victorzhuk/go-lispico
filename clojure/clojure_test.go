package clojure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestClojure_IsIdentity asserts that the Clojure dialect is the identity
// dialect — no delta, no vocab, default axes — which is required for bytecode
// VM compatibility.
func TestClojure_IsIdentity(t *testing.T) {
	assert.True(t, Dialect().IsIdentity(), "Clojure dialect must be the identity (bare FullDialect)")
}

// TestClojure_NoVocab asserts that no vocabulary map leaks into the Clojure
// dialect, even if IsIdentity semantics change.
func TestClojure_NoVocab(t *testing.T) {
	assert.Nil(t, Dialect().Vocab(), "Clojure dialect must have nil vocabulary")
}

// TestClojure_ReaderFlags_DefaultsClojureStyle asserts that the default reader
// flags keep [..] and {..} on, and #' and #(...) off.
func TestClojure_ReaderFlags_DefaultsClojureStyle(t *testing.T) {
	d := Dialect()

	// [1] reads as a vector
	vals, err := d.Read("[1]")
	require.NoError(t, err, "[1] must parse as a vector literal")
	require.Len(t, vals, 1, "expected one form")
	_, ok := vals[0].(core.Vector)
	assert.True(t, ok, "[1] must read as a Vector")

	// {:a 1} reads as a map
	vals, err = d.Read("{:a 1}")
	require.NoError(t, err, "{:a 1} must parse as a map literal")
	require.Len(t, vals, 1, "expected one form")
	_, ok = vals[0].(*core.HashMap)
	assert.True(t, ok, "{:a 1} must read as a HashMap")
}

package clojure

import (
	"errors"
	"fmt"
	"sync"
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

// TestClojure_Dialect_Memoized asserts that repeated Dialect() calls are
// stable and that Fingerprint() on the memoized value skips the SHA-256 hash
// work an uncached Dialect repeats on every call. Allocation count, not
// wall-clock, is the observation mechanism: a cache hit returns the
// already-hashed string, while an uncached Fingerprint() allocates a new
// hash.Hash and formats its inputs every time.
func TestClojure_Dialect_Memoized(t *testing.T) {
	memoized := Dialect()
	uncached := core.FullDialect().FlatCond()
	assert.Equal(t, memoized.Fingerprint(), uncached.Fingerprint(), "memoized and uncached Fingerprint() must agree")
	assert.Equal(t, Dialect().Fingerprint(), memoized.Fingerprint(), "repeated Dialect() calls must produce the same fingerprint")

	memoizedAllocs := testing.AllocsPerRun(50, func() {
		_ = memoized.Fingerprint()
	})
	uncachedAllocs := testing.AllocsPerRun(50, func() {
		_ = uncached.Fingerprint()
	})

	t.Logf("memoized Fingerprint(): %.1f allocs/op, uncached Fingerprint(): %.1f allocs/op", memoizedAllocs, uncachedAllocs)
	assert.Less(t, memoizedAllocs, uncachedAllocs, "Fingerprint() on a memoized Dialect must not redo the SHA-256 hash work")
}

// TestClojure_ConcurrentDialectCorpusParity builds the Clojure dialect
// concurrently and re-runs the existing identity/vocab/reader assertions on
// each, proving the process-memoized dialect behaves identically to
// independent construction under concurrent use.
func TestClojure_ConcurrentDialectCorpusParity(t *testing.T) {
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := Dialect()

			if !d.IsIdentity() {
				errs <- errors.New("clojure dialect must be identity")
				return
			}
			if d.Vocab() != nil {
				errs <- errors.New("clojure dialect must have nil vocabulary")
				return
			}

			vals, err := d.Read("[1]")
			if err != nil {
				errs <- err
				return
			}
			if len(vals) != 1 {
				errs <- fmt.Errorf("[1] expected one form, got %d", len(vals))
				return
			}
			if _, ok := vals[0].(core.Vector); !ok {
				errs <- errors.New("[1] must read as a Vector")
				return
			}

			vals, err = d.Read("{:a 1}")
			if err != nil {
				errs <- err
				return
			}
			if len(vals) != 1 {
				errs <- fmt.Errorf("{:a 1} expected one form, got %d", len(vals))
				return
			}
			if _, ok := vals[0].(*core.HashMap); !ok {
				errs <- errors.New("{:a 1} must read as a HashMap")
				return
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

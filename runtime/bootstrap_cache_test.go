package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestStdlibBootstrapCache_SecondEngineReusesArtifact(t *testing.T) {
	clearStdlibBootstrapCacheForTest()
	restore := setStdlibBootstrapCacheDisabledForTest(false)
	defer restore()

	first := newBytecodeStdlibEngine(t)
	defer first.Close()
	assertStdlibBootstrapBehavior(t, first)

	// No bootstrap entry is reusable, so startup never reaches the cache.
	stats := stdlibBootstrapCacheStatsForTest()
	assert.Equal(t, stdlibBootstrapCacheStats{}, stats)

	second := newBytecodeStdlibEngine(t)
	defer second.Close()
	assertStdlibBootstrapBehavior(t, second)

	stats = stdlibBootstrapCacheStatsForTest()
	assert.Equal(t, stdlibBootstrapCacheStats{}, stats)
}

func TestStdlibBootstrapCache_DisableHookForcesCompile(t *testing.T) {
	clearStdlibBootstrapCacheForTest()
	restore := setStdlibBootstrapCacheDisabledForTest(true)
	defer restore()

	first := newBytecodeStdlibEngine(t)
	defer first.Close()
	second := newBytecodeStdlibEngine(t)
	defer second.Close()

	// get-in is a Go builtin: touching it on both engines compiles nothing,
	// so the disable hook has no artifact left to force.
	for _, eng := range []Engine{first, second} {
		v, err := eng.Eval(context.Background(), "touch", "(get-in {:a {:b 1}} [:a :b])")
		require.NoError(t, err)
		require.True(t, core.Int{V: 1}.Equals(v))
	}

	stats := stdlibBootstrapCacheStatsForTest()
	assert.Equal(t, stdlibBootstrapCacheStats{}, stats)
}

func TestStdlibBootstrapCache_CacheCeilingByDialectFingerprint(t *testing.T) {
	clearStdlibBootstrapCacheForTest()
	restore := setStdlibBootstrapCacheDisabledForTest(false)
	defer restore()

	const totalDialects = maxStdlibBootstrapArtifacts + 16
	for i := 0; i < totalDialects; i++ {
		eng := newBytecodeStdlibEngineWithOptions(
			t,
			WithDialect(syntheticStdlibDialect(i)),
		)
		_, err := eng.Eval(context.Background(), "warm", "(get-in {:a {:b 1}} [:a :b])")
		require.NoError(t, err)
		require.NoError(t, eng.Close())
	}

	// The per-dialect ceiling is unexercised while nothing produces an
	// artifact: more dialects than it allows still store none.
	stats := stdlibBootstrapCacheStatsForTest()
	assert.Equal(t, stdlibBootstrapCacheStats{}, stats)
}

func syntheticStdlibDialect(i int) core.Dialect {
	return clojure.Dialect().Add(fmt.Sprintf("stdlib-cache-dialect-%d", i), "if")
}

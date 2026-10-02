package runtime

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// cachedChunk returns the chunk cached for src at formIndex under the
// engine's current macro epoch. The stripe map is read under its own mutex;
// the cache is populated only through Engine.Eval.
func cachedChunk(t *testing.T, e Engine, src string, formIndex int) (*vm.Chunk, bool) {
	t.Helper()
	ei, ok := e.(*engineImpl)
	require.True(t, ok)
	require.NotNil(t, ei.bytecodeEvaluator)
	be := ei.bytecodeEvaluator
	key := cacheKey{
		sourceHash: sourceHash(sha256.Sum256([]byte(src))),
		formIndex:  formIndex,
		macroEpoch: be.globals.MacroEpoch(),
	}
	s := be.stripeFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	return entry.chunk, true
}

// TestCache_DirectChunkReuse pins that re-evaluating the same source and form
// index reuses the identical *vm.Chunk pointer, not merely an equal result,
// and that the cache holds a single entry for both evaluations.
func TestCache_DirectChunkReuse(t *testing.T) {
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
	bindBuiltin(t, e, "+")

	const src = "(+ 1 2)"
	r1 := evalCacheSource(t, e, src)
	chunk1, ok := cachedChunk(t, e, src, 0)
	require.True(t, ok, "first eval must admit its chunk")
	require.NotNil(t, chunk1)

	r2 := evalCacheSource(t, e, src)
	chunk2, ok := cachedChunk(t, e, src, 0)
	require.True(t, ok, "the admitted chunk must still be cached")

	assert.Same(t, chunk1, chunk2, "second eval must reuse the identical chunk pointer")
	assert.True(t, r1.Equals(r2), "both evals must produce the same value")
	assert.Equal(t, 1, cacheCount(t, e), "re-evaluation must not add an entry")
}

// TestCache_MultiFormIndexIsolation pins that a source with two top-level
// forms caches each form under its own form index, that the chunks stay
// distinct, and that re-evaluation reuses each form's own chunk.
func TestCache_MultiFormIndexIsolation(t *testing.T) {
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	const src = "(+ 1 2) (* 2 3)"
	v := evalCacheSource(t, e, src)
	assert.True(t, core.Int{V: 6}.Equals(v), "a two-form source returns the last form's result")

	chunk0, ok := cachedChunk(t, e, src, 0)
	require.True(t, ok, "form index 0 must be cached")
	chunk1, ok := cachedChunk(t, e, src, 1)
	require.True(t, ok, "form index 1 must be cached")
	assert.NotSame(t, chunk0, chunk1, "each form index must hold its own chunk")
	assert.Equal(t, 2, cacheCount(t, e), "two forms must cache as two entries")

	evalCacheSource(t, e, src)
	chunk0b, ok := cachedChunk(t, e, src, 0)
	require.True(t, ok)
	chunk1b, ok := cachedChunk(t, e, src, 1)
	require.True(t, ok)
	assert.Same(t, chunk0, chunk0b, "form index 0 must reuse its own chunk")
	assert.Same(t, chunk1, chunk1b, "form index 1 must reuse its own chunk")
}

// TestCache_LazyTemplateFingerprintPreserved pins that the lazy template
// layer keeps sharing the evaluator's dialect fingerprint, and that
// stdlibTemplateKey.cacheKey still distinguishes dialects that differ only in
// fingerprint with identical plugin name and version.
func TestCache_LazyTemplateFingerprintPreserved(t *testing.T) {
	dialect, err := core.NewDialect(core.DialectSpec{Forms: map[string]string{"pick": "when"}})
	require.NoError(t, err)
	e, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	impl := e.(*engineImpl)
	require.NotNil(t, impl.bytecodeEvaluator)
	require.NotNil(t, impl.lazyMaterializer)

	fp := dialect.Fingerprint()
	assert.Equal(t, fp, impl.bytecodeEvaluator.dialectFP,
		"the evaluator must carry the selected dialect's fingerprint")
	assert.Equal(t, fp, impl.lazyMaterializer.dialectFP,
		"the lazy materializer must share the evaluator's fingerprint")

	a := stdlibTemplateKey{dialectFP: fp, pluginName: "plugin", pluginVersion: "1.0.0"}
	b := stdlibTemplateKey{dialectFP: fp + "-other", pluginName: "plugin", pluginVersion: "1.0.0"}
	assert.NotEqual(t, a.cacheKey(), b.cacheKey(),
		"keys differing only in the dialect fingerprint must differ")
	assert.Equal(t, a.cacheKey(), (stdlibTemplateKey{dialectFP: fp, pluginName: "plugin", pluginVersion: "1.0.0"}).cacheKey(),
		"identical keys must render identically")
}

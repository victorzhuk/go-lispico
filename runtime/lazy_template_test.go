package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// putEntryGuardTestSeq gives each TestPutEntry_RefusesWriteOnPublishedLayer
// invocation its own registry key. The registry is process-global and
// layers are never pruned, so a fixed literal key would collide with itself
// across repeated runs in one process (-count>1) once the first run's
// markComplete leaves that key permanently complete.
var putEntryGuardTestSeq atomic.Int64

// TestPutEntry_RefusesWriteOnPublishedLayer is the white-box guard test for
// task 2.2: once a layer is complete, putEntry must refuse the write instead
// of mutating the map every attached engine reads, and it must do so without
// touching the underlying entries map at all.
func TestPutEntry_RefusesWriteOnPublishedLayer(t *testing.T) {
	t.Parallel()

	seq := putEntryGuardTestSeq.Add(1)
	key := stdlibTemplateKey{dialectFP: fmt.Sprintf("put-entry-guard-standalone-%d", seq), pluginName: "", pluginVersion: "1.0.0"}

	require.NoError(t, stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name: "a", kind: stdlibTemplateGoValue,
	}))
	stdlibLazyTemplateRegistry.markComplete(key)

	layer, ok := stdlibLazyTemplateRegistry.layerFor(key)
	require.True(t, ok)
	require.True(t, layer.complete)

	err := stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name: "b", kind: stdlibTemplateGoValue,
	})
	assert.Error(t, err, "putEntry must refuse a write once the layer is published")

	assert.Len(t, layer.entries, 1, "the published layer's entries map must stay untouched")
	_, present := layer.entries["b"]
	assert.False(t, present, "the refused entry must never reach the map")

	published := layer.publishedEntries()
	assert.Len(t, published, 1, "the published read path must reflect only the entry written before completion")
}

// TestUnloadPlugin_PublishedLayerIdentityUnaffected pins task 4.2: a
// sibling's UnloadPlugin+re-Use never republishes the layer or replaces any
// entry — both the map itself and every entry pointer inside it survive
// identically, not merely "evaluation still works".
func TestUnloadPlugin_PublishedLayerIdentityUnaffected(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("published-unload-identity", "if")

	first, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.Use(stdlib.New()))

	second, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.Use(stdlib.New()))

	// The scenario unloads "after materializing some of its names", so force
	// materialization on the engine that will unload before it does.
	_, err = second.Eval(context.Background(), "materialize-before-unload", "(+ 1 2)")
	require.NoError(t, err)
	require.Positive(t, second.(*engineImpl).lazyMaterializer.MaterializeCount())

	impl := first.(*engineImpl)
	keys := impl.lazyMaterializer.activeKeys()
	require.Len(t, keys, 1)
	layer, ok := stdlibLazyTemplateRegistry.layerFor(keys[0])
	require.True(t, ok)

	before := layer.published.Load()
	require.NotNil(t, before)
	plusBefore, ok := (*before)["+"]
	require.True(t, ok)

	require.NoError(t, second.UnloadPlugin(""))
	require.NoError(t, second.Use(stdlib.New()))

	after := layer.published.Load()
	assert.Same(t, before, after, "a sibling's unload+re-Use must not republish the layer")
	plusAfter, ok := (*after)["+"]
	require.True(t, ok)
	assert.Same(t, plusBefore, plusAfter, "entry pointer identity must survive a sibling's unload+re-Use")

	v, err := first.Eval(context.Background(), "still-there", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(v), "the layer-building engine must be unaffected by a sibling's unload")
}

// TestPublishedLayer_ConcurrentFirstBuildRacesAttach covers the window
// TestPublishedLayer_ConcurrentAttachRacesUnload cannot: there the layer is
// already complete before any goroutine starts, so ensureLayer short-circuits
// and markComplete's Store never runs concurrently with anything. Here the key
// starts unbuilt, so exactly one goroutine publishes while the rest reach
// publishedEntries as singleflight releases them — the Store racing the Loads
// that depend on it. Every engine must observe the same published map.
func TestPublishedLayer_ConcurrentFirstBuildRacesAttach(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("published-concurrent-first-build", "if")

	const engines = 16
	var wg sync.WaitGroup
	wg.Add(engines)

	seen := make([]*map[string]*stdlibTemplateEntry, engines)
	built := make([]Engine, engines)

	for i := range engines {
		go func(i int) {
			defer wg.Done()
			eng, err := New(nil, WithBytecode(), WithDialect(dialect))
			if !assert.NoError(t, err) {
				return
			}
			if !assert.NoError(t, eng.Use(stdlib.New())) {
				return
			}
			built[i] = eng

			keys := eng.(*engineImpl).lazyMaterializer.activeKeys()
			if !assert.Len(t, keys, 1) {
				return
			}
			layer, ok := stdlibLazyTemplateRegistry.layerFor(keys[0])
			if !assert.True(t, ok) {
				return
			}
			seen[i] = layer.published.Load()
		}(i)
	}
	wg.Wait()

	var first *map[string]*stdlibTemplateEntry
	for i, eng := range built {
		if eng == nil {
			continue
		}
		t.Cleanup(func() { _ = eng.Close() })

		require.NotNil(t, seen[i], "engine %d attached without observing a published layer", i)
		if first == nil {
			first = seen[i]
			continue
		}
		assert.Same(t, first, seen[i], "engine %d observed a different published map", i)
	}
	require.NotNil(t, first, "no engine completed")

	for i, eng := range built {
		if eng == nil {
			continue
		}
		v, err := eng.Eval(context.Background(), "race-first-build", "(+ 1 2)")
		require.NoError(t, err, "engine %d", i)
		assert.True(t, core.Int{V: 3}.Equals(v), "engine %d", i)
	}
}

// TestPublishedLayer_ConcurrentAttachRacesUnload pins task 4.3: many engines
// attaching one already-complete layer for the first time, concurrently with
// a sibling engine repeatedly unloading and re-Using the same plugin
// identity. Run with -race.
func TestPublishedLayer_ConcurrentAttachRacesUnload(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("published-concurrent-attach-unload", "if")

	builder, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = builder.Close() })
	require.NoError(t, builder.Use(stdlib.New()))

	unloader, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = unloader.Close() })
	require.NoError(t, unloader.Use(stdlib.New()))

	const attachers = 16
	var wg sync.WaitGroup
	wg.Add(attachers + 1)

	go func() {
		defer wg.Done()
		for range 50 {
			assert.NoError(t, unloader.UnloadPlugin(""))
			assert.NoError(t, unloader.Use(stdlib.New()))
		}
	}()

	engines := make([]Engine, attachers)
	for i := range attachers {
		go func(i int) {
			defer wg.Done()
			eng, err := New(nil, WithBytecode(), WithDialect(dialect))
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, eng.Use(stdlib.New()))
			engines[i] = eng
		}(i)
	}
	wg.Wait()

	for i, eng := range engines {
		if eng == nil {
			continue
		}
		t.Cleanup(func() { _ = eng.Close() })
		v, err := eng.Eval(context.Background(), "race-attach", "(+ 1 2)")
		require.NoError(t, err, "engine %d", i)
		assert.True(t, core.Int{V: 3}.Equals(v), "engine %d", i)
	}
}

// TestPublishedEntries_AllocsZeroOnCompleteLayer pins task 5.1 / design
// decision 4: the attach read path allocates exactly zero for a complete
// layer, discriminating at N=1 regardless of layer size (a copy costs at
// least one allocation no matter how small the map is).
func TestPublishedEntries_AllocsZeroOnCompleteLayer(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	dialect := clojure.Dialect().Add("published-entries-zero-alloc", "if")
	eng, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	impl := eng.(*engineImpl)
	keys := impl.lazyMaterializer.activeKeys()
	require.Len(t, keys, 1)
	layer, ok := stdlibLazyTemplateRegistry.layerFor(keys[0])
	require.True(t, ok)

	allocs := testing.AllocsPerRun(200, func() {
		_ = layer.publishedEntries()
	})
	assert.Equal(t, float64(0), allocs, "attach read path must allocate zero for a complete layer")
}

// TestNewStdlibLazyEngineState_NoBookkeepingUntilFirstWrite pins task 3.1: an
// engine that never loads a template-routed plugin must never allocate the
// active/installed/tombstoned maps — only activate, recordInstall, and
// TombstoneForDelete create them, on their own first write.
func TestNewStdlibLazyEngineState_NoBookkeepingUntilFirstWrite(t *testing.T) {
	t.Parallel()

	s := newStdlibLazyEngineState()
	assert.Nil(t, s.active)
	assert.Nil(t, s.installed)
	assert.Nil(t, s.tombstoned)

	// The scenario is about a whole engine, not the constructor in isolation:
	// installLazyLayer runs on every New, so an engine that loads no plugin
	// must still reach Close with none of the three maps allocated.
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	state := eng.(*engineImpl).lazyMaterializer.state
	assert.Nil(t, state.active)
	assert.Nil(t, state.installed)
	assert.Nil(t, state.tombstoned)
	require.NoError(t, eng.Close())
}

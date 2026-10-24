package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// lmPausedLookup reproduces the state of a first-touch lookup that already
// passed LookupAndMaterialize's tombstone/installed selection but has not yet
// acquired the per-name lock inside materializeOne: the selection result
// (active key + registry entry) is returned with everything still deferred.
func lmPausedLookup(t *testing.T, m *stdlibLazyMaterializer, name string) (stdlibTemplateKey, *stdlibTemplateEntry) {
	t.Helper()
	for _, key := range m.activeKeys() {
		if entry, ok := stdlibLazyTemplateRegistry.entryFor(key, name); ok {
			return key, entry
		}
	}
	t.Fatalf("no active template entry for %q", name)
	return stdlibTemplateKey{}, nil
}

func lmAssertNotResurrected(t *testing.T, eng Engine, m *stdlibLazyMaterializer, name, src string) {
	t.Helper()
	state := m.state
	state.mu.Lock()
	_, installed := state.installed[name]
	state.mu.Unlock()
	assert.False(t, installed, "%s must not be recorded installed", name)
	assert.False(t, eng.RootEnv().HasLive(name), "%s must not be live in the value cell", name)
	assert.False(t, eng.RootEnv().HasLiveFunc(name), "%s must not be live in the func cell", name)
	_, err := eng.Eval(context.Background(), "after-unload", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")
}

// A first-touch lookup paused between selection and the per-name lock must not
// install a stale entry when UnloadPlugin completes inside the window.
func TestLazyMaterialize_UnloadDuringPausedLookupDoesNotResurrect(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)
	m := impl.lazyMaterializer

	key, entry := lmPausedLookup(t, m, "reverse")

	// The unload completes while the lookup is conceptually paused; the
	// resume below is exactly the materializeOne call the paused lookup makes.
	require.NoError(t, eng.UnloadPlugin(""))

	v, found, _ := m.materializeOne(eng.RootEnv(), key, entry, false)
	assert.False(t, found, "paused lookup must not report the stale entry")
	assert.Nil(t, v)
	lmAssertNotResurrected(t, eng, m, "reverse", "(reverse [1 2])")
}

// Same paused window, but the intervening event is an explicit deletion of the
// name: the tombstone taken under the name's mutex must win over the install.
func TestLazyMaterialize_DeleteDuringPausedLookupDoesNotResurrect(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	m := eng.(*engineImpl).lazyMaterializer

	key, entry := lmPausedLookup(t, m, "reverse")
	m.BeginDelete(eng.RootEnv(), "reverse")()

	v, found, _ := m.materializeOne(eng.RootEnv(), key, entry, false)
	assert.False(t, found, "paused lookup must not report a tombstoned entry")
	assert.Nil(t, v)
	lmAssertNotResurrected(t, eng, m, "reverse", "(reverse [1 2])")
}

// Control: the identical paused lookup with no unload or deletion in the
// window still materializes, and the binding resolves.
func TestLazyMaterialize_PausedLookupMaterializesWithoutIntervention(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	m := eng.(*engineImpl).lazyMaterializer

	key, entry := lmPausedLookup(t, m, "reverse")

	v, found, _ := m.materializeOne(eng.RootEnv(), key, entry, false)
	assert.True(t, found, "control lookup must materialize")
	assert.NotNil(t, v)
	_, err = eng.Eval(context.Background(), "control", "(reverse [1 2])")
	assert.NoError(t, err)
}

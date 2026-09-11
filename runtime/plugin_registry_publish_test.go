package runtime

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// rpWatchdog only detects a deadlocked barrier; it never orders events.
const rpWatchdog = 2 * time.Second

type rpPlugin struct {
	name    string
	version string
	init    func(env *core.Env) error
}

func (p *rpPlugin) Name() string              { return p.name }
func (p *rpPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: p.version} }
func (p *rpPlugin) Init(env *core.Env) error  { return p.init(env) }

type rpHost struct{ name string }

func (h *rpHost) Name() string              { return h.name }
func (h *rpHost) Metadata() core.PluginMeta { return core.PluginMeta{Version: "host"} }
func (h *rpHost) Init(*core.Env) error      { return nil }

type rpBarrier struct {
	entered chan struct{}
	release chan struct{}
}

func newRPBarrier() *rpBarrier {
	return &rpBarrier{entered: make(chan struct{}), release: make(chan struct{})}
}

// pause runs inside Init: it signals the test and parks until released.
func (b *rpBarrier) pause() error {
	close(b.entered)
	select {
	case <-b.release:
		return nil
	case <-time.After(rpWatchdog):
		return errors.New("barrier release timed out")
	}
}

func rpWait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(rpWatchdog):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func rpResult(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(rpWatchdog):
		t.Fatalf("timed out waiting for %s to return", what)
		return nil
	}
}

func rpGo(fn func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	return done
}

func rpWantCode(t *testing.T, err error, code string) {
	t.Helper()
	var lerr *core.LispicoError
	if assert.ErrorAs(t, err, &lerr) {
		assert.Equal(t, code, lerr.Code)
	}
}

func rpWantInt(t *testing.T, env *core.Env, name string, want int) {
	t.Helper()
	v, ok := env.Get(name)
	if !assert.Truef(t, ok, "%s: want bound", name) {
		return
	}
	assert.Equalf(t, core.Int{V: int64(want)}, v, "%s", name)
}

func rpWantAbsent(t *testing.T, env *core.Env, name string) {
	t.Helper()
	v, ok := env.Get(name)
	assert.Falsef(t, ok, "%s: want absent, got %v", name, v)
}

func TestUsePublishIfRejectsStaleGeneration(t *testing.T) {
	t.Parallel()

	r := core.NewRegistry()
	p1, p2, p3, p4 := &rpHost{name: "p"}, &rpHost{name: "p"}, &rpHost{name: "p"}, &rpHost{name: "p"}

	require.Equal(t, uint64(0), r.Generation("p"), "absent entry: want generation 0")
	require.NoError(t, r.PublishIf(p1, 0))
	g1 := r.Generation("p")
	require.Greater(t, g1, uint64(0), "PublishIf on an absent entry: want a positive generation")

	err := r.PublishIf(p2, 0)
	require.Error(t, err, "PublishIf with a stale generation overwrote the entry")
	rpWantCode(t, err, core.CodeRegistryConflict)
	assert.Contains(t, err.Error(), "p")
	if got, ok := r.Get("p"); assert.True(t, ok, "conflict removed the entry") {
		assert.Same(t, p1, got, "conflict replaced the entry")
	}
	assert.Equal(t, g1, r.Generation("p"), "conflict moved the generation")

	require.NoError(t, r.PublishIf(p2, g1))
	if got, ok := r.Get("p"); assert.True(t, ok, "matching PublishIf left no entry") {
		assert.Same(t, p2, got, "matching PublishIf did not install the plugin")
	}
	g2 := r.Generation("p")
	assert.Greater(t, g2, g1, "matching PublishIf: want a newer generation")

	r.Unregister("p")
	assert.Equal(t, uint64(0), r.Generation("p"), "Unregister: want generation 0")

	require.NoError(t, r.Register(p3))
	g3 := r.Generation("p")
	assert.Greater(t, g3, g2, "Register after Unregister reused a generation")

	r.RegisterNoCheck(p4)
	assert.Greater(t, r.Generation("p"), g3, "RegisterNoCheck: want a newer generation")
}

func TestUseRegistryConflictKeepsHostEntry(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	const name = "rp-conflict"
	root := eng.RootEnv()
	require.NoError(t, root.Set("rp-x", core.Int{V: 1}))
	active0 := eng.Stats().ActivePlugins

	b := newRPBarrier()
	p := &rpPlugin{name: name, version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("rp-x", core.Int{V: 2}); err != nil {
			return err
		}
		if err := env.Set("rp-y", core.Int{V: 3}); err != nil {
			return err
		}
		return b.pause()
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	host := &rpHost{name: name}
	eng.Registry().RegisterNoCheck(host)
	hostGen := eng.Registry().Generation(name)
	close(b.release)
	err = rpResult(t, done, "Use")

	require.Error(t, err, "Use published over a host registry entry written during Init")
	rpWantCode(t, err, core.CodeRegistryConflict)
	assert.Contains(t, err.Error(), "publish plugin "+name)
	if got, ok := eng.Registry().Get(name); assert.True(t, ok, "host registry entry lost") {
		assert.Same(t, host, got, "host registry entry replaced")
	}
	assert.Equal(t, hostGen, eng.Registry().Generation(name), "host entry generation moved")
	rpWantInt(t, root, "rp-x", 1)
	rpWantAbsent(t, root, "rp-y")
	assert.Equal(t, active0, eng.Stats().ActivePlugins, "failed Use changed ActivePlugins")

	eng.Registry().Unregister(name)
	retry := &rpPlugin{name: name, version: "1.0.0", init: func(env *core.Env) error {
		return env.Set("rp-y", core.Int{V: 3})
	}}
	require.NoError(t, eng.Use(retry), "retry after the host freed the name")
	assert.Equal(t, active0+1, eng.Stats().ActivePlugins, "retry: want one more active plugin")
	rpWantInt(t, root, "rp-y", 3)
	if got, ok := eng.Registry().Get(name); assert.True(t, ok, "retry left no registry entry") {
		assert.Same(t, retry, got, "retry did not publish the plugin")
	}
}

func TestReloadPluginRegistryConflictKeepsHostRemoval(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	const name = "rp-reload"
	require.NoError(t, eng.Use(&bindingPlugin{name: name, names: []string{"rp-old"}}))
	require.Equal(t, 1, eng.Stats().ActivePlugins)

	b := newRPBarrier()
	p := &rpPlugin{name: name, version: "2.0.0", init: func(env *core.Env) error {
		if err := env.Set("rp-new", core.Int{V: 2}); err != nil {
			return err
		}
		return b.pause()
	}}
	done := rpGo(func() error { return eng.ReloadPlugin(p) })

	rpWait(t, b.entered, "Init entry")
	eng.Registry().Unregister(name)
	close(b.release)
	err = rpResult(t, done, "ReloadPlugin")

	require.Error(t, err, "ReloadPlugin published over a host removal made during Init")
	rpWantCode(t, err, core.CodeRegistryConflict)
	if got, ok := eng.Registry().Get(name); ok {
		t.Errorf("host removal lost: registry holds %s %s", got.Name(), got.Metadata().Version)
	}
	root := eng.RootEnv()
	_, ok := root.Get("rp-old")
	assert.True(t, ok, "rp-old: old plugin binding not restored")
	rpWantAbsent(t, root, "rp-new")
	assert.Equal(t, 1, eng.Stats().ActivePlugins, "failed reload changed ActivePlugins")
}

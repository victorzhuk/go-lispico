package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// rrWatchdog only detects a deadlocked barrier; it never orders events.
const rrWatchdog = 2 * time.Second

var errRR = errors.New("rr init failed")

type rrPlugin struct {
	name    string
	version string
	init    func(env *core.Env) error
}

func (p *rrPlugin) Name() string              { return p.name }
func (p *rrPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: p.version} }
func (p *rrPlugin) Init(env *core.Env) error  { return p.init(env) }

// rrFailingStdlib reloads the stdlib namespace at a new version and fails
// before registering anything.
type rrFailingStdlib struct{ version string }

func (rrFailingStdlib) Name() string                { return "" }
func (p rrFailingStdlib) Metadata() core.PluginMeta { return core.PluginMeta{Version: p.version} }
func (rrFailingStdlib) Init(*core.Env) error        { return errRR }

func rrConst(name string, v core.Value) core.GoFunc {
	return core.GoFunc{Name: name, Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
		return v, nil
	}}
}

func rrCall(t *testing.T, v core.Value) core.Value {
	t.Helper()
	fn, ok := v.(core.GoFunc)
	require.Truef(t, ok, "want GoFunc, got %T", v)
	got, err := fn.Fn(t.Context(), nil, nil, nil)
	require.NoError(t, err)
	return got
}

func rrWantInt(t *testing.T, env *core.Env, name string, want int) {
	t.Helper()
	v, ok := env.Get(name)
	if !assert.Truef(t, ok, "%s: want bound", name) {
		return
	}
	assert.Equalf(t, core.Int{V: int64(want)}, v, "%s", name)
}

func rrWantBound(t *testing.T, env *core.Env, name string) {
	t.Helper()
	_, ok := env.Get(name)
	assert.Truef(t, ok, "%s: want bound", name)
}

func rrWantAbsent(t *testing.T, env *core.Env, name string) {
	t.Helper()
	v, ok := env.Get(name)
	assert.Falsef(t, ok, "%s: want absent, got %v", name, v)
}

func rrWantEval(t *testing.T, eng Engine, src, want string) {
	t.Helper()
	v, err := eng.Eval(t.Context(), "rr", src)
	if assert.NoErrorf(t, err, "%s: want it to evaluate", src) {
		assert.Equalf(t, want, v.String(), "%s", src)
	}
}

func rrWantUndefined(t *testing.T, eng Engine, src string) {
	t.Helper()
	_, err := eng.Eval(t.Context(), "rr", src)
	if assert.Errorf(t, err, "%s: want undefined", src) {
		assert.Contains(t, err.Error(), "undefined", src)
	}
}

func rrStdlibEngine(t *testing.T) Engine {
	t.Helper()
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	return eng
}

func TestReloadPluginFailedInitRestoresCanonicalBinding(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	const name = "rr-canon-plugin"
	root := eng.RootEnv()
	require.NoError(t, root.SetCanonical("rr-canon", rrConst("rr-canon", core.Int{V: 1})))
	require.NoError(t, eng.Use(&bindingPlugin{name: name, names: []string{"rr-old"}}))

	err = eng.ReloadPlugin(&rrPlugin{name: name, version: "2.0.0", init: func(env *core.Env) error {
		if err := env.Set("rr-canon", rrConst("rr-canon", core.Int{V: 2})); err != nil {
			return err
		}
		if err := env.Set("rr-new", core.Int{V: 3}); err != nil {
			return err
		}
		return errRR
	}})
	require.ErrorIs(t, err, errRR)

	if v, ok, canon := root.GetCanonical("rr-canon"); assert.True(t, ok, "rr-canon: want bound") {
		assert.True(t, canon, "rr-canon: failed reload lost the canonical flag")
		assert.Equal(t, core.Int{V: 1}, rrCall(t, v), "rr-canon: want the original function")
	}
	rrWantBound(t, root, "rr-old")
	rrWantAbsent(t, root, "rr-new")
	if p, ok := eng.Registry().Get(name); assert.True(t, ok, "old plugin lost after failed reload") {
		assert.Equal(t, "1.0.0", p.Metadata().Version)
	}
	assert.Equal(t, 1, eng.Stats().ActivePlugins, "failed reload changed ActivePlugins")
}

func TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion(t *testing.T) {
	t.Parallel()

	eng1 := rrStdlibEngine(t)
	eng2 := rrStdlibEngine(t)

	rrWantEval(t, eng1, "(+ 1 2)", "3")
	rrWantEval(t, eng1, "(str 1)", `"1"`)
	fnStr, err := eng1.Func("str")
	require.NoError(t, err)
	root := eng1.RootEnv()
	require.NoError(t, root.Set("count", rrConst("count", core.Int{V: 99})))
	root.Delete("reverse")
	impl2 := eng2.(*engineImpl)
	m2 := impl2.lazyMaterializer.MaterializeCount()

	err = eng1.ReloadPlugin(rrFailingStdlib{version: "rr-2.0.0"})
	require.ErrorIs(t, err, errRR)

	rrWantEval(t, eng1, "(+ 1 2)", "3")
	if _, ok, canon := root.GetCanonical("+"); assert.True(t, ok, "+: want bound") {
		assert.True(t, canon, "+: failed reload lost the canonical flag")
	}
	got, err := fnStr.Call(t.Context(), core.Int{V: 1})
	if assert.NoError(t, err, "retained str handle after failed reload") {
		assert.Equal(t, `"1"`, got.String())
	}
	rrWantEval(t, eng1, "(count [1])", "99")
	rrWantUndefined(t, eng1, "(reverse [1 2])")
	rrWantEval(t, eng1, "(nth [10 20] 1)", "20")

	_, err = eng2.Eval(t.Context(), "rr", "(reverse [1 2])")
	assert.NoError(t, err, "sibling engine lost reverse")
	assert.Equal(t, m2+1, impl2.lazyMaterializer.MaterializeCount(), "sibling engine: want only the reverse touch to materialize")
}

func TestReloadPluginFailedInitKeepsConcurrentHostWrites(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	const name = "rr-host"
	require.NoError(t, eng.Use(&bindingPlugin{name: name, names: []string{"rr-old"}}))
	root := eng.RootEnv()
	for _, n := range []string{"a", "h-rebind", "h-del", "h-repl"} {
		require.NoError(t, root.Set(n, core.Int{V: 1}))
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	p := &rrPlugin{name: name, version: "2.0.0", init: func(env *core.Env) error {
		if err := env.Set("a", core.Int{V: 2}); err != nil {
			return err
		}
		if err := env.Set("rr-plugin-only", core.Int{V: 1}); err != nil {
			return err
		}
		close(entered)
		select {
		case <-release:
		case <-time.After(rrWatchdog):
			return errors.New("barrier release timed out")
		}
		if err := env.Set("a", core.Int{V: 3}); err != nil {
			return err
		}
		return errRR
	}}
	done := make(chan error, 1)
	go func() { done <- eng.ReloadPlugin(p) }()

	select {
	case <-entered:
	case <-time.After(rrWatchdog):
		t.Fatal("timed out waiting for Init entry")
	}
	hostErr := errors.Join(
		root.Set("h-add", core.Int{V: 1}),
		root.Set("h-rebind", core.Int{V: 9}),
		func() error { root.Delete("h-del"); return nil }(),
		root.ReplaceCell("h-repl", core.Int{V: 5}),
		root.Set("a", core.Int{V: 7}),
	)
	root.Rebuild()
	close(release)

	select {
	case err = <-done:
	case <-time.After(rrWatchdog):
		t.Fatal("timed out waiting for ReloadPlugin to return")
	}

	require.NoError(t, hostErr)
	require.ErrorIs(t, err, errRR)
	rrWantInt(t, root, "h-add", 1)
	rrWantInt(t, root, "h-rebind", 9)
	rrWantAbsent(t, root, "h-del")
	rrWantInt(t, root, "h-repl", 5)
	rrWantInt(t, root, "a", 7)
	rrWantAbsent(t, root, "rr-plugin-only")
	rrWantBound(t, root, "rr-old")
}

func TestUseRetryAfterFailedInitPublishesOnce(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	const name = "rr-retry"
	root := eng.RootEnv()
	require.NoError(t, root.Set("rr-shared", core.Int{V: 1}))

	failing := &rrPlugin{name: name, version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("rr-shared", core.Int{V: 2}); err != nil {
			return err
		}
		if err := env.Set("rr-first", core.Int{V: 1}); err != nil {
			return err
		}
		return errRR
	}}
	require.ErrorIs(t, eng.Use(failing), errRR)

	retry := &rrPlugin{name: name, version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("rr-shared", core.Int{V: 3}); err != nil {
			return err
		}
		return env.Set("rr-second", core.Int{V: 4})
	}}
	require.NoError(t, eng.Use(retry))

	assert.Equal(t, 1, eng.Stats().ActivePlugins, "retry: want exactly one active plugin")
	if got, ok := eng.Registry().Get(name); assert.True(t, ok, "retry left no registry entry") {
		assert.Same(t, retry, got, "registry must hold the succeeding instance")
	}
	rrWantInt(t, root, "rr-shared", 3)
	rrWantInt(t, root, "rr-second", 4)
	rrWantAbsent(t, root, "rr-first")

	require.NoError(t, eng.UnloadPlugin(name))
	rrWantAbsent(t, root, "rr-second")
	rrWantBound(t, root, "rr-shared")
}

func TestUnloadPluginKeepsLastWriterOwnership(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Use(&bindingPlugin{name: "rr-a", names: []string{"rr-shared", "rr-a-only"}}))
	require.NoError(t, eng.Use(&bindingPlugin{name: "rr-b", names: []string{"rr-shared", "rr-b-only"}}))
	root := eng.RootEnv()

	require.NoError(t, eng.UnloadPlugin("rr-a"))
	rrWantAbsent(t, root, "rr-shared")
	rrWantAbsent(t, root, "rr-a-only")
	rrWantBound(t, root, "rr-b-only")

	require.NoError(t, eng.UnloadPlugin("rr-b"))
	rrWantAbsent(t, root, "rr-b-only")
}

package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// lrWatchdog only detects a deadlocked barrier; it never orders events.
const lrWatchdog = 2 * time.Second

var lrErr = errors.New("lr init failed")

// lrFailingStdlib reloads the stdlib namespace at a new version and fails
// before registering anything.
type lrFailingStdlib struct{}

func (lrFailingStdlib) Name() string              { return "" }
func (lrFailingStdlib) Metadata() core.PluginMeta { return core.PluginMeta{Version: "lr-2.0.0"} }
func (lrFailingStdlib) Init(*core.Env) error      { return lrErr }

type lrPlugin struct {
	name string
	init func(env *core.Env) error
}

func (p *lrPlugin) Name() string              { return p.name }
func (p *lrPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: "1.0.0"} }
func (p *lrPlugin) Init(env *core.Env) error  { return p.init(env) }

func lrColdStdlib(t *testing.T) (Engine, *engineImpl) {
	t.Helper()
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)
	require.Equal(t, 0, impl.lazyMaterializer.MaterializeCount(), "stdlib not cold after Use")
	return eng, impl
}

func lrTombstoned(impl *engineImpl, name string) bool {
	state := impl.lazyMaterializer.state
	state.mu.Lock()
	defer state.mu.Unlock()
	_, ok := state.tombstoned[name]
	return ok
}

func lrWantEval(t *testing.T, eng Engine, src, want string) {
	t.Helper()
	v, err := eng.Eval(t.Context(), "lr", src)
	if assert.NoErrorf(t, err, "%s: want it to evaluate", src) {
		assert.Equalf(t, want, v.String(), "%s", src)
	}
}

func lrWantUndefined(t *testing.T, eng Engine, src string) {
	t.Helper()
	_, err := eng.Eval(t.Context(), "lr", src)
	if assert.Errorf(t, err, "%s: want undefined", src) {
		assert.Contains(t, err.Error(), "undefined", src)
	}
}

func lrEval(ctx context.Context, env *core.Env, src string) error {
	form, err := core.ReadOne(src)
	if err != nil {
		return err
	}
	_, err = env.Evaluator().Eval(ctx, form, env)
	return err
}

func TestReloadPluginFailedColdStdlibKeepsDeferredNames(t *testing.T) {
	t.Parallel()

	eng, impl := lrColdStdlib(t)
	active0 := eng.Stats().ActivePlugins

	err := eng.ReloadPlugin(lrFailingStdlib{})
	require.ErrorIs(t, err, lrErr)
	assert.Contains(t, err.Error(), "init plugin")

	if p, ok := eng.Registry().Get(""); assert.True(t, ok, "stdlib lost from the registry after failed reload") {
		assert.Equal(t, "1.0.0", p.Metadata().Version, "registry must keep the original stdlib")
	}
	assert.Equal(t, 0, impl.lazyMaterializer.MaterializeCount(), "failed reload forced materialization")

	lrWantEval(t, eng, "(+ 1 2)", "3")
	lrWantEval(t, eng, "(str 1 2)", `"12"`)
	lrWantEval(t, eng, "(-> 1 (+ 2))", "3")
	assert.Equal(t, active0, eng.Stats().ActivePlugins, "failed reload changed ActivePlugins")
}

func TestLazyViewMaterializationRevertsWithFailedUse(t *testing.T) {
	t.Parallel()

	eng, impl := lrColdStdlib(t)
	before := impl.lazyMaterializer.MaterializeCount()

	ctx := t.Context()
	var getOK bool
	var evalErr error
	p := &lrPlugin{name: "lr-view", init: func(env *core.Env) error {
		_, getOK = env.Get("str")
		evalErr = lrEval(ctx, env, "(reverse [1 2])")
		return lrErr
	}}
	require.ErrorIs(t, eng.Use(p), lrErr)
	require.True(t, getOK, "Get(str) through the supplied env did not resolve")
	require.NoError(t, evalErr, "(reverse [1 2]) through the supplied env")

	installed := installedNames(impl)
	assert.NotContains(t, installed, "str", "view materialization of str survived the failed Use")
	assert.NotContains(t, installed, "reverse", "view materialization of reverse survived the failed Use")
	assert.Equal(t, before, impl.lazyMaterializer.MaterializeCount(), "failed Use kept its materialization count")

	lrWantEval(t, eng, "(str 1)", `"1"`)
	assert.Equal(t, before+1, impl.lazyMaterializer.MaterializeCount(), "str must materialize again on first touch after abort")
}

func TestLazyViewDeleteTombstoneRevertsWithFailedUse(t *testing.T) {
	t.Parallel()

	eng, impl := lrColdStdlib(t)
	lrWantEval(t, eng, "(+ 1 2)", "3")

	p := &lrPlugin{name: "lr-delete", init: func(env *core.Env) error {
		env.Delete("nth")
		env.Delete("+")
		return lrErr
	}}
	require.ErrorIs(t, eng.Use(p), lrErr)

	assert.False(t, lrTombstoned(impl, "nth"), "nth: plugin tombstone survived the failed Use")
	assert.False(t, lrTombstoned(impl, "+"), "+: plugin tombstone survived the failed Use")
	lrWantEval(t, eng, "(nth [10 20] 1)", "20")
	lrWantEval(t, eng, "(+ 1 2)", "3")
}

func TestLazyRegisterValueWithoutOperationBindsImmediately(t *testing.T) {
	t.Parallel()

	eng, _ := lrColdStdlib(t)
	root := eng.RootEnv()
	fn := core.GoFunc{Name: "lr-host", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
		return core.Int{V: 1}, nil
	}}

	require.NoError(t, root.RegisterValue("lr-host", fn, false))
	_, ok := root.Get("lr-host")
	assert.True(t, ok, "host RegisterValue outside a plugin operation must bind immediately")
}

func TestLazyHostTombstoneDuringFailedUseSurvives(t *testing.T) {
	t.Parallel()

	eng, impl := lrColdStdlib(t)
	root := eng.RootEnv()

	entered := make(chan struct{})
	release := make(chan struct{})
	p := &lrPlugin{name: "lr-host-delete", init: func(env *core.Env) error {
		env.Delete("concat")
		close(entered)
		select {
		case <-release:
		case <-time.After(lrWatchdog):
			return errors.New("barrier release timed out")
		}
		env.Delete("sort")
		return lrErr
	}}
	done := make(chan error, 1)
	go func() { done <- eng.Use(p) }()

	select {
	case <-entered:
	case <-time.After(lrWatchdog):
		t.Fatal("timed out waiting for Init entry")
	}
	root.Delete("sort")
	root.Delete("concat")
	close(release)

	var err error
	select {
	case err = <-done:
	case <-time.After(lrWatchdog):
		t.Fatal("timed out waiting for Use to return")
	}
	require.ErrorIs(t, err, lrErr)

	lrWantUndefined(t, eng, "(sort [2 1])")
	lrWantUndefined(t, eng, "(concat [1] [2])")
	assert.True(t, lrTombstoned(impl, "sort"), "sort: host tombstone lost on abort")
	assert.True(t, lrTombstoned(impl, "concat"), "concat: host tombstone lost on abort")
}

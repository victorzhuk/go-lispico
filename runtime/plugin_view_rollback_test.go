package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// vrWatchdog only detects a deadlocked barrier; it never orders events.
const vrWatchdog = 2 * time.Second

var errVRInit = errors.New("vr init failed")

type vrPlugin struct {
	name    string
	version string
	init    func(env *core.Env) error
}

func (p *vrPlugin) Name() string              { return p.name }
func (p *vrPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: p.version} }
func (p *vrPlugin) Init(env *core.Env) error  { return p.init(env) }

type vrBarrier struct {
	entered chan struct{}
	release chan struct{}
}

func newVRBarrier() *vrBarrier {
	return &vrBarrier{entered: make(chan struct{}), release: make(chan struct{})}
}

// pause runs inside Init: it signals the test and parks until released.
func (b *vrBarrier) pause() error {
	close(b.entered)
	select {
	case <-b.release:
		return nil
	case <-time.After(vrWatchdog):
		return errors.New("barrier release timed out")
	}
}

type vrEvaluator struct{ core.Evaluator }

func vrWait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(vrWatchdog):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func vrResult(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(vrWatchdog):
		t.Fatalf("timed out waiting for %s to return", what)
		return nil
	}
}

func vrGo(fn func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	return done
}

func vrConst(name string, v core.Value) core.GoFunc {
	return core.GoFunc{Name: name, Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
		return v, nil
	}}
}

func vrCall(t *testing.T, v core.Value) core.Value {
	t.Helper()
	fn, ok := v.(core.GoFunc)
	require.Truef(t, ok, "want GoFunc, got %T", v)
	got, err := fn.Fn(t.Context(), nil, nil, nil)
	require.NoError(t, err)
	return got
}

func vrEval(ctx context.Context, env, target *core.Env, src string) error {
	form, err := core.ReadOne(src)
	if err != nil {
		return err
	}
	_, err = env.Evaluator().Eval(ctx, form, target)
	return err
}

func vrWantInt(t *testing.T, env *core.Env, name string, want int) {
	t.Helper()
	v, ok := env.Get(name)
	if !assert.Truef(t, ok, "%s: want bound", name) {
		return
	}
	assert.Equalf(t, core.Int{V: int64(want)}, v, "%s", name)
}

func vrWantAbsent(t *testing.T, env *core.Env, name string) {
	t.Helper()
	v, ok := env.Get(name)
	assert.Falsef(t, ok, "%s: want absent, got %v", name, v)
}

func vrWantCode(t *testing.T, err error, code string) {
	t.Helper()
	var lerr *core.LispicoError
	if assert.ErrorAs(t, err, &lerr) {
		assert.Equal(t, code, lerr.Code)
	}
}

func TestUseFailedInitRestoresOverwrittenBindings(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	root0 := eng.RootEnv()
	require.NoError(t, root0.Set("vr-val", core.Int{V: 1}))
	require.NoError(t, root0.SetFunc("vr-fn", vrConst("vr-fn", core.Int{V: 1})))
	require.NoError(t, root0.SetCanonical("vr-canon", vrConst("vr-canon", core.Int{V: 3})))
	fn, err := eng.Func("vr-fn")
	require.NoError(t, err)
	gen0 := root0.NameGen()

	p := &vrPlugin{name: "vr-overwrite", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("vr-val", core.Int{V: 2}); err != nil {
			return err
		}
		if err := env.SetFunc("vr-fn", vrConst("vr-fn", core.Int{V: 2})); err != nil {
			return err
		}
		if err := env.Set("vr-canon", vrConst("vr-canon", core.Int{V: 30})); err != nil {
			return err
		}
		if err := env.Set("vr-new-val", core.Int{V: 4}); err != nil {
			return err
		}
		if err := env.SetFunc("vr-new-fn", vrConst("vr-new-fn", core.Int{V: 5})); err != nil {
			return err
		}
		return errVRInit
	}}

	err = eng.Use(p)
	require.ErrorIs(t, err, errVRInit)
	assert.Contains(t, err.Error(), "init plugin vr-overwrite")

	root := eng.RootEnv()
	assert.Same(t, root0, root, "root identity changed")
	vrWantInt(t, root, "vr-val", 1)
	if v, ok := root.GetFunc("vr-fn"); assert.True(t, ok, "vr-fn: want bound") {
		assert.Equal(t, core.Int{V: 1}, vrCall(t, v), "vr-fn: want the original function")
	}
	if v, ok, canon := root.GetCanonical("vr-canon"); assert.True(t, ok, "vr-canon: want bound") {
		assert.True(t, canon, "vr-canon: want canonical restored")
		assert.Equal(t, core.Int{V: 3}, vrCall(t, v), "vr-canon: want the original function")
	}
	vrWantAbsent(t, root, "vr-new-val")
	if v, ok := root.GetFunc("vr-new-fn"); ok {
		t.Errorf("vr-new-fn: want absent, got %v", v)
	}
	_, registered := eng.Registry().Get("vr-overwrite")
	assert.False(t, registered, "failed plugin stayed registered")
	assert.Equal(t, uint64(0), eng.Registry().Generation("vr-overwrite"))
	assert.Equal(t, 0, eng.Stats().ActivePlugins)
	got, err := fn.Call(t.Context())
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 1}, got, "retained vr-fn handle: want the original function")
	assert.GreaterOrEqual(t, root.NameGen(), gen0, "NameGen moved backwards")
}

func TestUseFailedInitRestoresMacro(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	_, err = eng.Eval(t.Context(), "vr-macro", "(defmacro vr-m [x] x)")
	require.NoError(t, err)
	v, err := eng.Eval(t.Context(), "vr-macro", "(vr-m 1)")
	require.NoError(t, err)
	require.Equal(t, core.Int{V: 1}, v)

	ctx := t.Context()
	p := &vrPlugin{name: "vr-macro", version: "1.0.0", init: func(env *core.Env) error {
		if err := vrEval(ctx, env, env, "(defmacro vr-m [x] 99)"); err != nil {
			return err
		}
		return errVRInit
	}}
	require.ErrorIs(t, eng.Use(p), errVRInit)

	v, err = eng.Eval(t.Context(), "vr-macro", "(vr-m 1)")
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 1}, v, "failed Use left the plugin's macro definition in place")
}

func TestUseRegistryPendingDuringInit(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	b := newVRBarrier()
	p := &vrPlugin{name: "vr-pending", version: "1.0.0", init: func(*core.Env) error {
		if err := b.pause(); err != nil {
			return err
		}
		return errVRInit
	}}
	done := vrGo(func() error { return eng.Use(p) })

	vrWait(t, b.entered, "Init entry")
	_, seen := eng.Registry().Get("vr-pending")
	gen := eng.Registry().Generation("vr-pending")
	close(b.release)
	err = vrResult(t, done, "Use")

	assert.False(t, seen, "Registry().Get returned the plugin while its Init ran")
	assert.Equal(t, uint64(0), gen)
	require.ErrorIs(t, err, errVRInit)
	_, registered := eng.Registry().Get("vr-pending")
	assert.False(t, registered, "failed plugin stayed registered")
	assert.Equal(t, 0, eng.Stats().ActivePlugins)
}

func TestReloadPluginRegistryKeepsOldDuringInit(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(&bindingPlugin{name: "vr-reload", names: []string{"vr-reload/v"}}))

	b := newVRBarrier()
	p := &vrPlugin{name: "vr-reload", version: "2.0.0", init: func(*core.Env) error {
		if err := b.pause(); err != nil {
			return err
		}
		return errVRInit
	}}
	done := vrGo(func() error { return eng.ReloadPlugin(p) })

	vrWait(t, b.entered, "Init entry")
	during, seen := eng.Registry().Get("vr-reload")
	close(b.release)
	err = vrResult(t, done, "ReloadPlugin")

	if assert.True(t, seen, "old plugin missing from the registry while the reload Init ran") {
		assert.Equal(t, "1.0.0", during.Metadata().Version, "registry held the reloading plugin while its Init ran")
	}
	require.ErrorIs(t, err, errVRInit)
	if after, ok := eng.Registry().Get("vr-reload"); assert.True(t, ok, "old plugin lost after failed reload") {
		assert.Equal(t, "1.0.0", after.Metadata().Version)
	}
	assert.Equal(t, 1, eng.Stats().ActivePlugins)
}

func TestReloadPluginSettlementFailureKeepsActiveCount(t *testing.T) {
	t.Parallel()

	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	err = eng.ReloadPlugin(setupPlugin{})
	require.Error(t, err)
	vrWantCode(t, err, core.CodeResourceLimit)
	assert.Equal(t, 0, eng.Stats().ActivePlugins, "failed reload of a fresh plugin changed ActivePlugins")
	_, registered := eng.Registry().Get("setup")
	assert.False(t, registered, "failed plugin stayed registered")
	vrWantAbsent(t, eng.RootEnv(), "setup/value")
}

func TestUseFailedVocabularyRestoresOwnedBindings(t *testing.T) {
	t.Parallel()

	const slotLimit = 16
	dialect := core.FullDialect().Vocabulary(map[string]string{"vr-visible": "vr-canon"})
	eng, err := New(nil, WithDialect(dialect), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: slotLimit}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	root := eng.RootEnv()
	_, bootSlots := root.RetainedUsage()
	require.Less(t, bootSlots, int64(slotLimit), "boot retained slots leave no room for the fixture")
	require.NoError(t, root.Set("vr-seed", core.Int{V: 1}))

	var fillers []string
	p := &vrPlugin{name: "vr-vocab", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("vr-canon", vrConst("vr-canon", core.Int{V: 7})); err != nil {
			return err
		}
		if err := env.Set("vr-seed", core.Int{V: 2}); err != nil {
			return err
		}
		for i := 0; i < slotLimit; i++ {
			if _, slots := env.RetainedUsage(); slots >= slotLimit {
				return nil
			}
			name := fmt.Sprintf("vr-fill-%d", i)
			if err := env.Set(name, core.Int{V: int64(i)}); err != nil {
				return err
			}
			fillers = append(fillers, name)
		}
		return errors.New("retained slots never reached the limit")
	}}

	err = eng.Use(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply vocabulary for plugin vr-vocab")
	vrWantCode(t, err, core.CodeResourceLimit)

	vrWantInt(t, root, "vr-seed", 1)
	vrWantAbsent(t, root, "vr-canon")
	vrWantAbsent(t, root, "vr-visible")
	for _, name := range fillers {
		vrWantAbsent(t, root, name)
	}
	assert.Equal(t, 0, eng.Stats().ActivePlugins)
	_, registered := eng.Registry().Get("vr-vocab")
	assert.False(t, registered, "failed plugin stayed registered")

	assert.NoError(t, eng.Use(&mockPlugin{name: "vr-after", version: "1.0.0"}), "follow-up Use after vocabulary rollback")
}

func TestUseFailedInitKeepsConcurrentHostWrites(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	root := eng.RootEnv()
	for _, name := range []string{"a", "h-rebind", "h-del", "h-repl"} {
		require.NoError(t, root.Set(name, core.Int{V: 1}))
	}

	b := newVRBarrier()
	p := &vrPlugin{name: "vr-host", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("a", core.Int{V: 2}); err != nil {
			return err
		}
		if err := env.Set("vr-plugin-only", core.Int{V: 1}); err != nil {
			return err
		}
		if err := b.pause(); err != nil {
			return err
		}
		if err := env.Set("a", core.Int{V: 3}); err != nil {
			return err
		}
		return errVRInit
	}}
	done := vrGo(func() error { return eng.Use(p) })

	vrWait(t, b.entered, "Init entry")
	hostErr := errors.Join(
		root.Set("h-add", core.Int{V: 1}),
		root.Set("h-rebind", core.Int{V: 9}),
		func() error { root.Delete("h-del"); return nil }(),
		root.ReplaceCell("h-repl", core.Int{V: 5}),
		root.Set("a", core.Int{V: 7}),
	)
	root.Rebuild()
	close(b.release)
	err = vrResult(t, done, "Use")

	require.NoError(t, hostErr)
	require.ErrorIs(t, err, errVRInit)
	vrWantInt(t, root, "h-add", 1)
	vrWantInt(t, root, "h-rebind", 9)
	vrWantAbsent(t, root, "h-del")
	vrWantInt(t, root, "h-repl", 5)
	vrWantInt(t, root, "a", 7)
	vrWantAbsent(t, root, "vr-plugin-only")
}

func TestUseFailedInitKeepsHostLazyMaterialization(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)
	require.NotNil(t, impl.lazyMaterializer)

	b := newVRBarrier()
	p := &vrPlugin{name: "vr-lazy", version: "1.0.0", init: func(*core.Env) error {
		if err := b.pause(); err != nil {
			return err
		}
		return errVRInit
	}}
	done := vrGo(func() error { return eng.Use(p) })

	vrWait(t, b.entered, "Init entry")
	before := impl.lazyMaterializer.MaterializeCount()
	_, found := eng.RootEnv().Get("str")
	close(b.release)
	err = vrResult(t, done, "Use")

	require.True(t, found, "host Get(str) did not materialize")
	require.ErrorIs(t, err, errVRInit)
	v, err := eng.Eval(t.Context(), "vr-lazy", "(str 1)")
	if assert.NoError(t, err, "host-materialized str lost after failed Use") {
		assert.Equal(t, `"1"`, v.String())
	}
	assert.Contains(t, installedNames(impl), "str")
	assert.Equal(t, before+1, impl.lazyMaterializer.MaterializeCount(), "str materialized more than once")
}

func TestUseFailedInitRevertsAliasWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(ctx context.Context, env *core.Env) error
	}{
		{name: "find", write: func(_ context.Context, env *core.Env) error {
			owner, ok := env.Find("x")
			if !ok {
				return errors.New("x not found")
			}
			return owner.Set("x", core.Int{V: 2})
		}},
		{name: "child", write: func(ctx context.Context, env *core.Env) error {
			return vrEval(ctx, env, env.Child(), "(set! x 2)")
		}},
		{name: "evaluator", write: func(ctx context.Context, env *core.Env) error {
			return vrEval(ctx, env, env, "(def x 2)")
		}},
		{name: "merge", write: func(_ context.Context, env *core.Env) error {
			src := core.NewEnv(nil)
			if err := src.Set("x", core.Int{V: 2}); err != nil {
				return err
			}
			return src.MergeInto(env)
		}},
		{name: "closure", write: func(ctx context.Context, env *core.Env) error {
			if err := vrEval(ctx, env, env, "(defn vr-bump [] (set! x 2))"); err != nil {
				return err
			}
			return vrEval(ctx, env, env, "(vr-bump)")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eng, err := New(nil, WithDialect(clojure.Dialect()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })
			require.NoError(t, eng.RootEnv().Set("x", core.Int{V: 1}))

			ctx := t.Context()
			var writeErr error
			p := &vrPlugin{name: "vr-alias", version: "1.0.0", init: func(env *core.Env) error {
				writeErr = tt.write(ctx, env)
				return errVRInit
			}}
			require.ErrorIs(t, eng.Use(p), errVRInit)
			require.NoError(t, writeErr, "alias write inside Init")

			vrWantInt(t, eng.RootEnv(), "x", 1)
			vrWantAbsent(t, eng.RootEnv(), "vr-bump")
		})
	}
}

func TestUseEvaluatorReadDuringFailedInitIsRaceFree(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	root := eng.RootEnv()
	orig := root.Evaluator()
	evalSet := make(chan struct{})
	p := &vrPlugin{name: "vr-eval", version: "1.0.0", init: func(env *core.Env) error {
		env.SetEvaluator(&vrEvaluator{Evaluator: env.Evaluator()})
		close(evalSet)
		return errVRInit
	}}

	returned := make(chan struct{})
	reader := make(chan struct{})
	go func() {
		defer close(reader)
		select {
		case <-evalSet:
		case <-time.After(vrWatchdog):
			return
		}
		for range 10000 {
			select {
			case <-returned:
				return
			default:
			}
			_ = root.Evaluator()
		}
	}()

	err = eng.Use(p)
	close(returned)
	vrWait(t, reader, "evaluator reader")

	require.ErrorIs(t, err, errVRInit)
	if got := root.Evaluator(); got != orig {
		t.Errorf("RootEnv().Evaluator() = %T after failed Use, want the original evaluator restored", got)
	}
}

func TestUseSuppliedEnvStaysLiveAfterSuccess(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	var retained *core.Env
	p := &vrPlugin{name: "vr-live", version: "1.0.0", init: func(env *core.Env) error {
		retained = env
		return vrEval(ctx, env, env, "(defn vr-get [] vr-x)")
	}}
	require.NoError(t, eng.Use(p))
	require.NotNil(t, retained)

	root := eng.RootEnv()
	require.NoError(t, root.Set("vr-x", core.Int{V: 5}))
	vrWantInt(t, retained, "vr-x", 5)
	v, err := eng.Eval(t.Context(), "vr-live", "(vr-get)")
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 5}, v)

	require.NoError(t, retained.Set("vr-late", core.Int{V: 1}))
	vrWantInt(t, root, "vr-late", 1)
	require.Error(t, eng.Use(&mockPlugin{name: "vr-other", version: "1.0.0", initErr: errVRInit}))
	vrWantInt(t, root, "vr-late", 1)

	require.NoError(t, eng.UnloadPlugin("vr-live"))
	vrWantAbsent(t, root, "vr-get")
}

package runtime

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// lfGuard only detects a deadlocked barrier; it never orders events.
const lfGuard = 2 * time.Second

var lfErr = errors.New("lf init failed")

// lfProbe wraps the engine's evaluator and, once armed, parks the first
// DefineBootstrap of -> until release closes, so a view materialization is
// held in flight across the end of the plugin's Init.
type lfProbe struct {
	inner       core.Evaluator
	armed       atomic.Bool
	fire        sync.Once
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func (p *lfProbe) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Eval(ctx, form, env)
}

func (p *lfProbe) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Apply(ctx, fn, args, env)
}

func (p *lfProbe) DefineBootstrap(ctx context.Context, source string, env *core.Env) (core.Value, error) {
	if p.armed.Load() && strings.HasPrefix(source, "(defmacro -> ") {
		p.fire.Do(func() {
			close(p.entered)
			<-p.release
		})
	}
	return p.inner.(core.BootstrapDefiner).DefineBootstrap(ctx, source, env)
}

func (p *lfProbe) unblock() {
	p.releaseOnce.Do(func() { close(p.release) })
}

type lfPlugin struct {
	init func(env *core.Env) error
}

func (lfPlugin) Name() string               { return "lf" }
func (lfPlugin) Metadata() core.PluginMeta  { return core.PluginMeta{Version: "1.0.0"} }
func (p lfPlugin) Init(env *core.Env) error { return p.init(env) }

// lfArmed builds a cold bytecode Clojure engine with stdlib loaded and the
// probe armed on its root evaluator.
func lfArmed(t *testing.T) (Engine, *engineImpl, *lfProbe) {
	t.Helper()
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	root := eng.RootEnv()
	p := &lfProbe{inner: root.Evaluator(), entered: make(chan struct{}), release: make(chan struct{})}
	root.SetEvaluator(p)
	require.NoError(t, eng.Use(stdlib.New()))
	p.armed.Store(true)
	return eng, eng.(*engineImpl), p
}

// lfInit spawns a view lookup of -> that parks in the probe, waits until it
// is in flight, then returns result.
func lfInit(p *lfProbe, gDone chan struct{}, result error) func(env *core.Env) error {
	return func(env *core.Env) error {
		go func() {
			defer close(gDone)
			env.Get("->")
		}()
		select {
		case <-p.entered:
			return result
		case <-time.After(lfGuard):
			return errors.New("view materialization of -> never reached the probe")
		}
	}
}

// lfUse runs Use(pl) in its own goroutine and releases every barrier on
// cleanup, so a red run leaks no parked goroutine.
func lfUse(t *testing.T, eng Engine, p *lfProbe, pl lfPlugin, gDone chan struct{}) (<-chan struct{}, *error) {
	t.Helper()
	uDone := make(chan struct{})
	var useErr error
	go func() {
		defer close(uDone)
		useErr = eng.Use(pl)
	}()
	t.Cleanup(func() {
		p.unblock()
		lfDrain(uDone)
		lfDrain(gDone)
	})
	return uDone, &useErr
}

func lfDrain(ch <-chan struct{}) {
	select {
	case <-ch:
	case <-time.After(lfGuard):
	}
}

// lfWindow waits for the fence window: the operation closed while still
// open on the engine, before Use returns.
func lfWindow(t *testing.T, impl *engineImpl, p *lfProbe, uDone <-chan struct{}) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(lfGuard):
		require.FailNow(t, "view materialization of -> never reached the probe")
	}
	state := impl.lazyMaterializer.state
	deadline := time.Now().Add(lfGuard)
	for {
		state.mu.Lock()
		closed := state.op != nil && state.op.closed
		state.mu.Unlock()
		if closed {
			return
		}
		select {
		case <-uDone:
			require.FailNow(t, "Use returned before in-flight view materialization settled")
		default:
		}
		if time.Now().After(deadline) {
			require.FailNow(t, "fence window did not open within the guard")
		}
		goruntime.Gosched()
	}
}

func lfWait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(lfGuard):
		require.FailNowf(t, "barrier deadlocked", "%s did not finish after release", what)
	}
}

func TestLazyFenceWaitsForInFlightViewMaterialization(t *testing.T) {
	t.Parallel()

	eng, impl, p := lfArmed(t)
	before := impl.lazyMaterializer.MaterializeCount()

	gDone := make(chan struct{})
	uDone, useErr := lfUse(t, eng, p, lfPlugin{init: lfInit(p, gDone, lfErr)}, gDone)
	lfWindow(t, impl, p, uDone)

	hostDone := make(chan struct{})
	var strOK bool
	go func() {
		defer close(hostDone)
		_, strOK = eng.RootEnv().Get("str")
	}()
	select {
	case <-hostDone:
	case <-time.After(lfGuard):
		require.FailNow(t, "host lookup of str waited on the fence")
	}
	assert.True(t, strOK, "host lookup of str inside the fence window did not resolve")

	p.unblock()
	lfWait(t, uDone, "Use")
	lfWait(t, gDone, "view lookup of ->")

	require.ErrorIs(t, *useErr, lfErr)
	assert.Contains(t, (*useErr).Error(), "init plugin")
	assert.NotContains(t, installedNames(impl), "->", "in-flight view materialization of -> survived the failed Use")
	assert.Equal(t, before+1, impl.lazyMaterializer.MaterializeCount(), "only the host materialization of str may count")

	v, err := eng.Eval(t.Context(), "lf", "(-> 1 (+ 2))")
	require.NoError(t, err)
	assert.Equal(t, "3", v.String())
}

func TestLazyFenceSettlesInFlightMaterializationOnSuccess(t *testing.T) {
	t.Parallel()

	eng, impl, p := lfArmed(t)
	before := impl.lazyMaterializer.MaterializeCount()
	active := eng.Stats().ActivePlugins

	gDone := make(chan struct{})
	uDone, useErr := lfUse(t, eng, p, lfPlugin{init: lfInit(p, gDone, nil)}, gDone)
	lfWindow(t, impl, p, uDone)

	p.unblock()
	lfWait(t, uDone, "Use")
	lfWait(t, gDone, "view lookup of ->")

	require.NoError(t, *useErr)
	assert.Contains(t, installedNames(impl), "->", "in-flight view materialization of -> lost on a successful Use")
	assert.Equal(t, before+1, impl.lazyMaterializer.MaterializeCount(), "the settled materialization of -> must count once")
	assert.Equal(t, active+1, eng.Stats().ActivePlugins, "successful Use must add one active plugin")
}

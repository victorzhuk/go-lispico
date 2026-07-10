package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

type mockPlugin struct {
	name       string
	version    string
	initErr    error
	initCalled bool
	initCount  int
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Init(env *core.Env) error {
	m.initCalled = true
	m.initCount++
	return m.initErr
}

func (m *mockPlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version: m.version,
	}
}

func TestUse_Success(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p := &mockPlugin{name: "test", version: "1.0.0"}

	err = eng.Use(p)

	assert.NoError(t, err)
	assert.True(t, p.initCalled)

	stats := eng.Stats()
	assert.Equal(t, 1, stats.ActivePlugins)

	_, exists := eng.Registry().Get("test")
	assert.True(t, exists)
}

func TestUse_InitFailure(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	initErr := errors.New("init failed")
	p := &mockPlugin{name: "test", version: "1.0.0", initErr: initErr}

	err = eng.Use(p)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "init plugin test")
	assert.True(t, p.initCalled)

	stats := eng.Stats()
	assert.Equal(t, 0, stats.ActivePlugins)

	_, exists := eng.Registry().Get("test")
	assert.False(t, exists)
}

func TestUse_AlreadyRegistered(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p1 := &mockPlugin{name: "test", version: "1.0.0"}
	p2 := &mockPlugin{name: "test", version: "2.0.0"}

	err = eng.Use(p1)
	require.NoError(t, err)

	err = eng.Use(p2)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	stats := eng.Stats()
	assert.Equal(t, 1, stats.ActivePlugins)
}

func TestUnloadPlugin_Success(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, eng.Use(p))

	err = eng.UnloadPlugin("test")

	assert.NoError(t, err)

	stats := eng.Stats()
	assert.Equal(t, 0, stats.ActivePlugins)

	_, exists := eng.Registry().Get("test")
	assert.False(t, exists)
}

func TestUnloadPlugin_NotFound(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	err = eng.UnloadPlugin("nonexistent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestReloadPlugin_NewPlugin(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p := &mockPlugin{name: "test", version: "1.0.0"}

	err = eng.ReloadPlugin(p)

	assert.NoError(t, err)
	assert.True(t, p.initCalled)

	stats := eng.Stats()
	assert.Equal(t, 1, stats.ActivePlugins)
}

func TestReloadPlugin_ReplaceExisting(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p1 := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, eng.Use(p1))
	assert.Equal(t, 1, p1.initCount)

	p2 := &mockPlugin{name: "test", version: "2.0.0"}

	err = eng.ReloadPlugin(p2)

	assert.NoError(t, err)
	assert.True(t, p2.initCalled)

	stats := eng.Stats()
	assert.Equal(t, 1, stats.ActivePlugins)

	loaded, exists := eng.Registry().Get("test")
	require.True(t, exists)
	assert.Equal(t, "2.0.0", loaded.Metadata().Version)
}

func TestReloadPlugin_InitFailure_KeepsOld(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p1 := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, eng.Use(p1))

	initErr := errors.New("init failed")
	p2 := &mockPlugin{name: "test", version: "2.0.0", initErr: initErr}

	err = eng.ReloadPlugin(p2)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "init plugin test")

	stats := eng.Stats()
	assert.Equal(t, 1, stats.ActivePlugins)

	loaded, exists := eng.Registry().Get("test")
	require.True(t, exists)
	assert.Equal(t, "1.0.0", loaded.Metadata().Version)
}

func TestListPlugins_Empty(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	statuses := eng.ListPlugins()

	assert.Empty(t, statuses)
}

func TestListPlugins_Sorted(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	pZ := &mockPlugin{name: "zebra", version: "1.0.0"}
	pA := &mockPlugin{name: "alpha", version: "2.0.0"}
	pM := &mockPlugin{name: "middle", version: "3.0.0"}

	require.NoError(t, eng.Use(pZ))
	require.NoError(t, eng.Use(pA))
	require.NoError(t, eng.Use(pM))

	statuses := eng.ListPlugins()

	require.Len(t, statuses, 3)
	assert.Equal(t, "alpha", statuses[0].Name)
	assert.Equal(t, "middle", statuses[1].Name)
	assert.Equal(t, "zebra", statuses[2].Name)

	assert.Equal(t, "2.0.0", statuses[0].Version)
	assert.Equal(t, "3.0.0", statuses[1].Version)
	assert.Equal(t, "1.0.0", statuses[2].Version)

	for _, s := range statuses {
		assert.Equal(t, "active", s.Status)
	}
}

func TestListPlugins_AfterUnload(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	p1 := &mockPlugin{name: "alpha", version: "1.0.0"}
	p2 := &mockPlugin{name: "beta", version: "1.0.0"}

	require.NoError(t, eng.Use(p1))
	require.NoError(t, eng.Use(p2))

	require.NoError(t, eng.UnloadPlugin("alpha"))

	statuses := eng.ListPlugins()

	require.Len(t, statuses, 1)
	assert.Equal(t, "beta", statuses[0].Name)
}

// bindingPlugin registers named GoFuncs in Init, optionally in both the value
// cell (env.Set) and function cell (env.SetFunc).
type bindingPlugin struct {
	name  string
	names []string // Set these in the value cell
	funcs []string // SetFunc these in the function cell
}

func (p *bindingPlugin) Name() string { return p.name }

func (p *bindingPlugin) Init(env *core.Env) error {
	for _, n := range p.names {
		env.Set(n, core.GoFunc{Name: n, Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		}})
	}
	for _, n := range p.funcs {
		env.SetFunc(n, core.GoFunc{Name: n, Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		}})
	}
	return nil
}

func (p *bindingPlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{Version: "1.0.0"}
}

func TestUnloadPlugin_RemovesRegisteredFuncs(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	p := &bindingPlugin{name: t.Name(), names: []string{"json/encode"}, funcs: []string{"cl-func"}}
	require.NoError(t, eng.Use(p))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "test", `(json/encode "hi")`)
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "test", `(cl-func)`)
	require.NoError(t, err)

	require.NoError(t, eng.UnloadPlugin(t.Name()))

	var le *core.LispicoError
	_, err = eng.Eval(ctx, "test", `(json/encode "hi")`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)

	_, err = eng.Eval(ctx, "test", `(cl-func)`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)
}

func TestReloadPlugin_NoStaleBindings(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	p1 := &bindingPlugin{name: t.Name(), names: []string{"old-func"}, funcs: []string{"old-cl-func"}}
	require.NoError(t, eng.Use(p1))

	p2 := &bindingPlugin{name: t.Name(), names: []string{"new-func"}}
	require.NoError(t, eng.ReloadPlugin(p2))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "test", `(new-func)`)
	require.NoError(t, err)

	var le *core.LispicoError
	_, err = eng.Eval(ctx, "test", `(old-func)`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)

	_, err = eng.Eval(ctx, "test", `(old-cl-func)`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)
}

func TestUnloadPlugin_BothCellsCleared(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	p := &bindingPlugin{
		name:  t.Name(),
		names: []string{"only-value"},
		funcs: []string{"only-func"},
	}
	require.NoError(t, eng.Use(p))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "test", `(only-value)`)
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "test", `(only-func)`)
	require.NoError(t, err)

	require.NoError(t, eng.UnloadPlugin(t.Name()))

	var le *core.LispicoError
	_, err = eng.Eval(ctx, "test", `(only-value)`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)

	_, err = eng.Eval(ctx, "test", `(only-func)`)
	require.Error(t, err)
	require.True(t, errors.As(err, &le), "expected LispicoError")
	assert.Equal(t, "UndefinedError", le.Code)
}

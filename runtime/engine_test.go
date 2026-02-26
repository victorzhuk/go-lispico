package runtime

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew_DefaultOptions(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.NotNil(t, eng.rootEnv)
	assert.NotNil(t, eng.registry)
	assert.NotNil(t, eng.evaluator)
	assert.NotNil(t, eng.stats)
	assert.NoError(t, eng.Close())
}

func TestNew_CustomOptions(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(log,
		WithMaxEvalDepth(500),
		WithTimeout(10*time.Second),
		WithHotReloadDir("/tmp/scripts"),
	)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.Equal(t, 500, eng.config.maxEvalDepth)
	assert.Equal(t, 10*time.Second, eng.config.timeout)
	assert.Equal(t, "/tmp/scripts", eng.config.hotReloadDir)
	assert.NoError(t, eng.Close())
}

func TestNew_NilLogger(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.NotNil(t, eng.logger)
	assert.NoError(t, eng.Close())
}

func TestClose_NoWatcher(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)

	err = eng.Close()

	assert.NoError(t, err)
	assert.Nil(t, eng.watcher)
	assert.Nil(t, eng.watchCancel)
}

func TestStats_Initial(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	stats := eng.Stats()

	assert.Equal(t, int64(0), stats.TotalEvals)
	assert.Equal(t, int64(0), stats.TotalErrors)
	assert.Empty(t, stats.PluginCallCounts)
}

func TestStats_RecordEval(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	eng.stats.recordEval(time.Millisecond, nil)
	eng.stats.recordEval(time.Millisecond, assert.AnError)
	eng.stats.recordEval(time.Millisecond, nil)

	stats := eng.Stats()
	assert.Equal(t, int64(3), stats.TotalEvals)
	assert.Equal(t, int64(1), stats.TotalErrors)
}

func TestStats_RecordPluginCall(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	eng.stats.recordPluginCall("foo", time.Millisecond)
	eng.stats.recordPluginCall("foo", time.Millisecond)
	eng.stats.recordPluginCall("bar", time.Millisecond)

	stats := eng.Stats()
	assert.Equal(t, int64(2), stats.PluginCallCounts["foo"])
	assert.Equal(t, int64(1), stats.PluginCallCounts["bar"])
}

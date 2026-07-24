package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestFileWatcher_DetectsNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	assert.Empty(t, w.mtimes)

	file := filepath.Join(dir, "test.lisp")
	err = os.WriteFile(file, []byte("(def x 42)"), 0o644)
	require.NoError(t, err)

	w.scan()
	assert.Contains(t, w.mtimes, file)
}

func TestFileWatcher_DetectsModifiedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	file := filepath.Join(dir, "test.lisp")
	err = os.WriteFile(file, []byte("(def x 1)"), 0o644)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	originalMtime := w.mtimes[file]

	time.Sleep(10 * time.Millisecond)
	err = os.WriteFile(file, []byte("(def x 2)"), 0o644)
	require.NoError(t, err)

	w.scan()
	assert.NotEqual(t, originalMtime, w.mtimes[file])
}

func TestFileWatcher_IgnoresNonLispFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	txtFile := filepath.Join(dir, "test.txt")
	err = os.WriteFile(txtFile, []byte("hello"), 0o644)
	require.NoError(t, err)

	goFile := filepath.Join(dir, "test.go")
	err = os.WriteFile(goFile, []byte("package main"), 0o644)
	require.NoError(t, err)

	w.scan()

	assert.NotContains(t, w.mtimes, txtFile)
	assert.NotContains(t, w.mtimes, goFile)
}

func TestFileWatcher_IgnoresDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	subdir := filepath.Join(dir, "subdir")
	err = os.Mkdir(subdir, 0o755)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()

	assert.NotContains(t, w.mtimes, subdir)
}

func TestReloadFile_SyntaxErrorKeepsOldDefinitions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.RootEnv().Set("existing", core.Int{V: 42}))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	file := filepath.Join(dir, "bad.lisp")
	err = os.WriteFile(file, []byte("(def x "), 0o644)
	require.NoError(t, err)

	w.reloadFile(file)

	val, ok := eng.RootEnv().Get("existing")
	assert.True(t, ok)
	assert.Equal(t, int64(42), val.(core.Int).V)

	_, hasX := eng.RootEnv().Get("x")
	assert.False(t, hasX)
}

func TestReloadFile_EvalErrorKeepsOldDefinitions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.RootEnv().Set("existing", core.Int{V: 42}))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	file := filepath.Join(dir, "error.lisp")
	err = os.WriteFile(file, []byte("(def x 1) (undefined-symbol)"), 0o644)
	require.NoError(t, err)

	w.reloadFile(file)

	val, ok := eng.RootEnv().Get("existing")
	assert.True(t, ok)
	assert.Equal(t, int64(42), val.(core.Int).V)

	_, hasX := eng.RootEnv().Get("x")
	assert.False(t, hasX)
}

func TestReloadFile_PanicSurfacesErrorAndKeepsOldDefinitions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	eng, err := New(log)
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.RootEnv().Set("existing", core.Int{V: 42}))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	require.NoError(t, eng.Bind("boom", core.GoFunc{
		Name: "boom",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			panic("watcher boom")
		},
	}))

	file := filepath.Join(dir, "bad.lisp")
	err = os.WriteFile(file, []byte("(boom)"), 0o644)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		w.reloadFile(file)
	})

	val, ok := eng.RootEnv().Get("existing")
	assert.True(t, ok)
	assert.Equal(t, int64(42), val.(core.Int).V)

	assert.NotContains(t, buf.String(), "reloaded file")
	assert.Contains(t, buf.String(), "recovered panic")
	assert.Contains(t, buf.String(), "watcher boom")
}

func TestReloadFile_Success(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	file := filepath.Join(dir, "good.lisp")
	err = os.WriteFile(file, []byte("(def x 123) (def y 456)"), 0o644)
	require.NoError(t, err)

	w.reloadFile(file)

	xVal, xOk := eng.RootEnv().Get("x")
	assert.True(t, xOk)
	assert.Equal(t, int64(123), xVal.(core.Int).V)

	yVal, yOk := eng.RootEnv().Get("y")
	assert.True(t, yOk)
	assert.Equal(t, int64(456), yVal.(core.Int).V)
}

func TestWatch_StartsWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	assert.NoError(t, err)

	impl := eng.(*engineImpl)
	assert.NotNil(t, impl.watcher)

	eng.Stop()
}

func TestWatch_CtxCancelStopsWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	err = eng.Watch(ctx, dir)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	w := impl.watcher
	require.NotNil(t, w)

	cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}

func TestWatch_DoubleWatchReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Stop()

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestWatch_InvalidDirectory(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	err = eng.Watch(context.Background(), "/nonexistent/path")
	assert.Error(t, err)
}

func TestWatch_FileIsNotDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	err := os.WriteFile(file, []byte("test"), 0o644)
	require.NoError(t, err)

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	err = eng.Watch(context.Background(), file)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestWatchStop_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = eng.Stop()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()

	assert.Equal(t, before, after, "goroutine leak detected")
}

func TestWatchStop_CleanShutdown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	assert.NotNil(t, impl.watcher)
	assert.NotNil(t, impl.watchCancel)

	err = eng.Stop()
	assert.NoError(t, err)
	assert.Nil(t, impl.watcher)
	assert.Nil(t, impl.watchCancel)
}

func TestWatch_DetectsFileChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Stop()

	file := filepath.Join(dir, "test.lisp")
	err = os.WriteFile(file, []byte("(def value 1)"), 0o644)
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	time.Sleep(600 * time.Millisecond)

	val, ok := eng.RootEnv().Get("value")
	require.True(t, ok)
	assert.Equal(t, int64(1), val.(core.Int).V)

	err = os.WriteFile(file, []byte("(def value 2)"), 0o644)
	require.NoError(t, err)

	time.Sleep(600 * time.Millisecond)

	val, ok = eng.RootEnv().Get("value")
	require.True(t, ok)
	assert.Equal(t, int64(2), val.(core.Int).V)
}

func TestWatch_ReloadPanicKeepsLoopRunning(t *testing.T) {
	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.Bind("boom", core.GoFunc{
		Name: "boom",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			panic("watcher boom")
		},
	}))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.Start(context.Background())
	t.Cleanup(w.Stop)

	badFile := filepath.Join(dir, "bad.lisp")
	require.NoError(t, os.WriteFile(badFile, []byte("(boom)"), 0o644))

	stopped := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(stopped)
	}()

	time.Sleep(60 * time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("watch loop stopped after panic during reload")
	default:
	}

	goodFile := filepath.Join(dir, "good.lisp")
	require.NoError(t, os.WriteFile(goodFile, []byte("(def recovered 1)"), 0o644))

	time.Sleep(60 * time.Millisecond)
	w.Stop()

	val, ok := eng.RootEnv().Get("recovered")
	assert.True(t, ok)
	assert.Equal(t, int64(1), val.(core.Int).V)
}

func TestWatch_MultipleFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Stop()

	file1 := filepath.Join(dir, "a.lisp")
	file2 := filepath.Join(dir, "b.lisp")

	err = os.WriteFile(file1, []byte("(def from-a 10)"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("(def from-b 20)"), 0o644)
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	time.Sleep(600 * time.Millisecond)

	valA, okA := eng.RootEnv().Get("from-a")
	require.True(t, okA)
	assert.Equal(t, int64(10), valA.(core.Int).V)

	valB, okB := eng.RootEnv().Get("from-b")
	require.True(t, okB)
	assert.Equal(t, int64(20), valB.(core.Int).V)
}

func TestClose_StopsWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)

	err = eng.Watch(context.Background(), dir)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	assert.NotNil(t, impl.watcher)

	err = eng.Close()
	assert.NoError(t, err)
	assert.Nil(t, impl.watcher)
}

func TestReloadFile_ReadError(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, "/nonexistent", 10*time.Millisecond)
	w.ctx = context.Background()

	w.reloadFile("/nonexistent/file.lisp")

	assert.Empty(t, w.mtimes)
}

func TestFileWatcher_ScanNonexistentDirectory(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, "/nonexistent/dir", 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
}

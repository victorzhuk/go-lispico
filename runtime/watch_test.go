package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// TestWatch_ReentrantShutdown drives the shipped Watch path end to end:
// watchLoop -> scan -> reloadFile -> Eval -> host GoFunc that shuts the
// engine down from inside evaluation. Each case runs in a subprocess so a
// self-joining Stop surfaces as a bounded child failure instead of stalling
// the suite.
func TestWatch_ReentrantShutdown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		id   string
	}{
		{"Stop/bytecode", "stop-bytecode"},
		{"Stop/tree-walker", "stop-tree-walker"},
		{"Close/bytecode", "close-bytecode"},
		{"Close/tree-walker", "close-tree-walker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exe, err := os.Executable()
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// The child re-executes this binary; its TempDir must live in a
			// directory this test created, not a host-specific scratch path.
			tmp := t.TempDir()

			cmd := exec.CommandContext(ctx, exe,
				"-test.run=^TestWatch_ReentrantShutdown_Child$",
				"-test.timeout=4s",
			)
			cmd.Env = append(os.Environ(),
				"LISPICO_WATCH_CASE="+tc.id,
				"TMPDIR="+tmp,
			)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("watcher shutdown hung: parent deadline exceeded; child output:\n%s", out)
			}
			require.NoError(t, err, "child output:\n%s", out)
		})
	}
}

func TestWatch_ReentrantShutdown_Child(t *testing.T) {
	id := os.Getenv("LISPICO_WATCH_CASE")
	if id == "" {
		t.Skip("harness child; driven by TestWatch_ReentrantShutdown")
	}

	bytecode := strings.HasSuffix(id, "bytecode")
	action := "stop"
	if strings.HasPrefix(id, "close") {
		action = "close"
	}

	opts := []EngineOption{WithBytecode()}
	if !bytecode {
		opts = []EngineOption{WithTreeWalker()}
	}
	eng, err := New(nil, opts...)
	require.NoError(t, err)

	shutdown := func() error {
		if action == "close" {
			return eng.Close()
		}
		return eng.Stop()
	}

	done := make(chan struct{})
	var shutErr error
	require.NoError(t, eng.Bind("shutdown", core.GoFunc{
		Name: "shutdown",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			shutErr = shutdown()
			close(done)
			return core.Nil{}, nil
		},
	}))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case.lisp"), []byte("(shutdown)\n"), 0o644))

	require.NoError(t, eng.Watch(context.Background(), dir))

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("watcher shutdown hung")
	}
	require.NoError(t, shutErr)
	require.NoError(t, eng.Close())
}

// TestWatch_ReentrantShutdown_Control is the positive control: the identical
// watch scenario without a shutdown callback must reload and Stop must return
// nil, so a hung case above cannot be blamed on the harness itself.
func TestWatch_ReentrantShutdown_Control(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "probe.lisp"), []byte("(def control-marker 1)\n"), 0o644))

	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.Watch(context.Background(), dir))

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := eng.RootEnv().Get("control-marker"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher never reloaded probe file")
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NoError(t, eng.Stop())
}

func TestFileWatcher_PrunesRemovedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	file := filepath.Join(dir, "gone.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(def x 1)"), 0o644))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	require.Contains(t, w.mtimes, file)

	require.NoError(t, os.Remove(file))
	w.scan()
	assert.NotContains(t, w.mtimes, file)
}

func TestFileWatcher_RecreatedFileReloaded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	file := filepath.Join(dir, "churn.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(def x 1)"), 0o644))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	require.NoError(t, os.Remove(file))
	w.scan()

	require.NoError(t, os.WriteFile(file, []byte("(def x 2)"), 0o644))
	w.scan()

	val, ok := eng.RootEnv().Get("x")
	require.True(t, ok)
	assert.Equal(t, int64(2), val.(core.Int).V)
	assert.Contains(t, w.mtimes, file)
}

func TestFileWatcher_IncompleteScanKeepsMtimes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	file := filepath.Join(dir, "kept.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(def x 1)"), 0o644))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	require.Contains(t, w.mtimes, file)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.ctx = ctx
	w.scan()

	assert.Contains(t, w.mtimes, file)
}

func TestFileWatcher_RetriesUnchangedFailedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	file := filepath.Join(dir, "flaky.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(def x (undefined-symbol))"), 0o644))
	failedInfo, err := os.Stat(file)
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	w.scan()
	_, hasX := eng.RootEnv().Get("x")
	assert.False(t, hasX)
	assert.NotContains(t, w.mtimes, file)

	require.NoError(t, os.WriteFile(file, []byte("(def x 7)"), 0o644))
	// Pin the mtime back to the failed version's, proving the retry comes
	// from the missing stored mtime, not from a changed timestamp.
	require.NoError(t, os.Chtimes(file, failedInfo.ModTime(), failedInfo.ModTime()))

	w.scan()

	val, ok := eng.RootEnv().Get("x")
	require.True(t, ok)
	assert.Equal(t, int64(7), val.(core.Int).V)
	assert.Contains(t, w.mtimes, file)
}

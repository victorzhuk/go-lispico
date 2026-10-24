package runtime

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func smallLimitEngine(t *testing.T, ceiling int) Engine {
	t.Helper()
	e, err := New(nil, WithResourceLimits(ResourceLimits{
		MaxAllocationBytes:     ceiling,
		MaxCacheEntries:        1 << 20,
		MaxCacheBytes:          1 << 20,
		MaxCacheNodes:          1 << 20,
		MaxCollectionLen:       1 << 20,
		MaxRetainedBytesPerEnv: 1 << 20,
		MaxRetainedSlotsPerEnv: 1 << 20,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func writeSource(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

func TestReadSourceFile_OversizeRejectedBeforeFullRead(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	dir := t.TempDir()

	path := writeSource(t, dir, "big.lisp", bytes.Repeat([]byte("x"), ceiling*16))

	_, err := e.EvalFile(path)
	require.Error(t, err)
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "MaxAllocationBytes")
}

func TestReadSourceFile_AtLimitAccepted(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	dir := t.TempDir()

	content := bytes.Repeat([]byte(" "), ceiling)
	content[len(content)-1] = ')'
	content[0] = '('
	path := writeSource(t, dir, "exact.lisp", content)

	_, err := e.EvalFile(path)
	require.NoError(t, err)
}

func TestReadSourceFile_LimitPlusOneRejected(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	dir := t.TempDir()

	content := bytes.Repeat([]byte(" "), ceiling+1)
	content[0] = '('
	content[len(content)-1] = ')'
	path := writeSource(t, dir, "over.lisp", content)

	_, err := e.EvalFile(path)
	require.Error(t, err)
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
}

func TestReadSourceFile_MaxIntCeilingOnEmptyAndSmallFile(t *testing.T) {
	e := smallLimitEngine(t, math.MaxInt)
	dir := t.TempDir()

	empty := writeSource(t, dir, "empty.lisp", nil)
	got, err := e.(*engineImpl).readSourceFile(context.Background(), empty)
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = e.EvalFile(empty)
	require.NoError(t, err)

	small := writeSource(t, dir, "small.lisp", []byte("(defun f () 1)"))
	_, err = e.EvalFile(small)
	require.NoError(t, err)
}

func TestLoadDir_OversizeFileIsPerFile(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	dir := t.TempDir()

	writeSource(t, dir, "a-small.lisp", []byte("(defun f () 1)"))
	writeSource(t, dir, "z-big.lisp", bytes.Repeat([]byte("x"), ceiling*2))

	err := e.LoadDir(dir)
	require.Error(t, err)
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	assert.Contains(t, err.Error(), "z-big.lisp")
}

func TestReadSourceFile_CancelledContextStopsRead(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	dir := t.TempDir()

	writeSource(t, dir, "big.lisp", bytes.Repeat([]byte("x"), ceiling*16))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.(*engineImpl).readSourceFile(ctx, filepath.Join(dir, "big.lisp"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got %v", err)
}

func TestWatcherReload_OversizeFileStaysUnrecorded(t *testing.T) {
	const ceiling = 4096
	e := smallLimitEngine(t, ceiling)
	impl := e.(*engineImpl)
	dir := t.TempDir()
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	file := writeSource(t, dir, "big.lisp", bytes.Repeat([]byte("x"), ceiling*2))

	w.scan()
	assert.NotContains(t, w.mtimes, file, "an oversize file must not be recorded")

	require.NoError(t, os.WriteFile(file, []byte("(defun f () 1)"), 0o644))
	w.scan()
	assert.Contains(t, w.mtimes, file, "the failed file must be retried once small")
}

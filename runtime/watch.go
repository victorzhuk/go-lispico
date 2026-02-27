package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type fileWatcher struct {
	engine   *engineImpl
	dir      string
	interval time.Duration
	mtimes   map[string]time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func newFileWatcher(engine *engineImpl, dir string, interval time.Duration) *fileWatcher {
	return &fileWatcher{
		engine:   engine,
		dir:      dir,
		interval: interval,
		mtimes:   make(map[string]time.Time),
	}
}

func (w *fileWatcher) Start(ctx context.Context) {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(1)
	go w.watchLoop()
}

func (w *fileWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

func (w *fileWatcher) watchLoop() {
	defer w.wg.Done()

	w.scan()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

func (w *fileWatcher) scan() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		w.engine.logger.Error("scan directory", "dir", w.dir, "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".lisp") {
			continue
		}

		path := filepath.Join(w.dir, name)
		info, err := entry.Info()
		if err != nil {
			w.engine.logger.Error("get file info", "path", path, "error", err)
			continue
		}

		mtime := info.ModTime()
		lastMtime, seen := w.mtimes[path]

		if !seen || !mtime.Equal(lastMtime) {
			w.mtimes[path] = mtime
			w.reloadFile(path)
		}
	}
}

func (w *fileWatcher) reloadFile(path string) {
	start := time.Now()

	content, err := os.ReadFile(path)
	if err != nil {
		w.engine.logger.Error("read file", "path", path, "error", err)
		return
	}

	forms, err := core.Read(string(content))
	if err != nil {
		w.engine.logger.Error("parse file", "path", path, "error", err)
		return
	}

	childEnv := w.engine.rootEnv.Child()

	for _, form := range forms {
		_, err := w.engine.evaluator.Eval(w.ctx, form, childEnv)
		if err != nil {
			w.engine.logger.Error("eval form in file", "path", path, "error", err)
			return
		}
	}

	childEnv.MergeInto(w.engine.rootEnv)

	w.engine.logger.Info("reloaded file",
		"path", path,
		"forms", len(forms),
		"duration", time.Since(start),
	)
}

func (e *engineImpl) Watch(ctx context.Context, dir string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.watcher != nil {
		return fmt.Errorf("watcher already running")
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat watch directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("watch path is not a directory: %s", dir)
	}

	w := newFileWatcher(e, dir, 500*time.Millisecond)
	e.watcher = w
	e.watchCtx, e.watchCancel = context.WithCancel(context.Background())

	w.Start(e.watchCtx)

	e.logger.Info("started watching", "dir", dir)
	return nil
}

func (e *engineImpl) Stop() error {
	e.stopWatcher()
	return nil
}

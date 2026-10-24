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

// Stop requests cancellation but never joins the watchLoop goroutine: that
// goroutine may be inside Eval running a host GoFunc that itself calls
// Stop/Close, so waiting here would join our own goroutine and deadlock.
// The goroutine owns its own exit and wg.Done accounting.
func (w *fileWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *fileWatcher) watchLoop() {
	defer w.wg.Done()

	if w.ctx.Err() != nil {
		return
	}

	w.scan()

	if w.ctx.Err() != nil {
		return
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if w.ctx.Err() != nil {
				return
			}
			w.scan()
		}
	}
}

func (w *fileWatcher) scan() {
	if w.ctx.Err() != nil {
		return
	}

	entries, err := os.ReadDir(w.dir)
	if err != nil {
		w.engine.logger.Error("scan directory", "dir", w.dir, "error", err)
		return
	}

	// present tracks every .lisp path seen with a readable info in this
	// scan. Pruning runs only after a complete uncanceled pass, so a
	// canceled scan or a directory-read error never drops stored mtimes.
	present := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if w.ctx.Err() != nil {
			return
		}
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
		present[path] = struct{}{}

		mtime := info.ModTime()
		lastMtime, seen := w.mtimes[path]

		if !seen || !mtime.Equal(lastMtime) {
			if w.reloadFile(path) {
				w.mtimes[path] = mtime
			}
		}
	}

	if w.ctx.Err() != nil {
		return
	}
	for path := range w.mtimes {
		if _, ok := present[path]; !ok {
			delete(w.mtimes, path)
		}
	}
}

// startReloadEval draws the reload's enclosing evaluation lease with the
// same panic containment the settlement gets: a host meter may panic from
// the initial lease draw, and the panic must surface as an error, not kill
// the watch loop.
func startReloadEval(ctx context.Context) (top bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			top, err = false, core.NewPanicError("start eval", recovered)
		}
	}()
	return core.StartEval(ctx)
}

// reloadFile returns true only after a complete successful application:
// all forms evaluated, eval state flushed, and the child env merged into
// the root. Any earlier exit leaves the stored mtime untouched so the
// unchanged file retries on the next scan.
func (w *fileWatcher) reloadFile(path string) (ok bool) {
	start := time.Now()

	ctx := w.engine.evalResourceContext(core.DetachEvalState(w.ctx))
	if d := w.engine.evalDeadline(ctx, start); !d.IsZero() {
		ctx = core.WithEvalDeadline(ctx, d)
	}
	top, err := startReloadEval(ctx)
	if err != nil {
		w.engine.logger.Error("start eval in file", "path", path, "error", err)
		return false
	}
	settled := false
	// settle is panic-contained on its own: FinishEval recovers meter panics
	// from the flush and the retained charge, but a host meter panicking from
	// ReturnEval escapes FinishEval's defer, and the watch loop must survive
	// that too.
	settle := func() (ok bool) {
		settled = true
		defer func() {
			if r := recover(); r != nil {
				w.engine.logger.Error("settle eval in file", "path", path, "error", core.NewPanicError("settlement", r))
				ok = false
			}
		}()
		if ferr := core.FinishEval(ctx, top); ferr != nil {
			w.engine.logger.Error("settle eval in file", "path", path, "error", ferr)
			return false
		}
		return true
	}
	defer func() {
		recovered := recover()
		if !settled {
			settle()
		}
		if recovered != nil {
			w.engine.logger.Error(
				"eval form in file",
				"path", path,
				"error", core.NewPanicError(path, recovered),
			)
		}
	}()

	content, err := w.engine.readSourceFile(ctx, path)
	if err != nil {
		w.engine.logger.Error("read file", "path", path, "error", err)
		return false
	}
	forms, err := w.engine.readForms(ctx, string(content))
	if err != nil {
		w.engine.logger.Error("parse file", "path", path, "error", err)
		return false
	}

	childEnv := w.engine.rootEnv.Child()

	for _, form := range forms {
		if ctx.Err() != nil {
			return false
		}
		_, err := w.engine.evaluator.Eval(ctx, form, childEnv)
		if err != nil {
			w.engine.logger.Error("eval form in file", "path", path, "error", err)
			return false
		}
	}
	if err := core.FlushEvalState(ctx); err != nil {
		w.engine.logger.Error("eval form in file", "path", path, "error", err)
		return false
	}

	if ctx.Err() != nil {
		return false
	}
	// A failed settlement must leave the stored mtime and the root env
	// untouched, so the unchanged file retries on the next scan.
	if !settle() {
		return false
	}
	if err := childEnv.MergeInto(w.engine.rootEnv); err != nil {
		w.engine.logger.Error("merge file env", "path", path, "error", err)
		return false
	}
	// MergeInto copies cells but not scope counters: the child's epoch bump
	// from a reloaded defmacro stays on the child. Chunk-cache keys carry the
	// root's epoch, so without an explicit bump here, cached expansions of
	// the reloaded macro keep returning the stale chunk.
	if definesMacro(childEnv) {
		w.engine.rootEnv.BumpMacroEpoch()
	}

	w.engine.logger.Info(
		"reloaded file",
		"path", path,
		"forms", len(forms),
		"duration", time.Since(start),
	)
	return true
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
	e.watchCtx, e.watchCancel = context.WithCancel(ctx)

	w.Start(e.watchCtx)

	e.logger.Info("started watching", "dir", dir)
	return nil
}

func (e *engineImpl) Stop() error {
	e.stopWatcher()
	return nil
}

// definesMacro reports whether env's local bindings include a macro, under
// either cell a dialect's defmacro may own (Lisp-2 binds the function cell,
// Lisp-1 the value cell).
func definesMacro(env *core.Env) bool {
	for _, name := range env.LocalNames() {
		if cell, ok := env.CellLocal(name); ok {
			if v, _, _ := env.ReadCell(cell); v != nil {
				if _, isMacro := v.(core.Macro); isMacro {
					return true
				}
			}
		}
	}
	for _, name := range env.LocalFuncNames() {
		if cell, ok := env.FuncCellLocal(name); ok {
			if v, _, _ := env.ReadCell(cell); v != nil {
				if _, isMacro := v.(core.Macro); isMacro {
					return true
				}
			}
		}
	}
	return false
}

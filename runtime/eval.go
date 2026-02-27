package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

func (e *engineImpl) Eval(ctx context.Context, source, input string) (core.Value, error) {
	start := time.Now()

	forms, err := core.Read(input)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, env)
		if err != nil {
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	e.logger.Debug("eval", "source", source, "duration", dur)
	return result, nil
}

func (e *engineImpl) EvalFile(path string) (core.Value, error) {
	e.logger.Info("loading file", "path", path)

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	result, err := e.Eval(context.Background(), path, string(content))
	if err != nil {
		return nil, err
	}

	e.logger.Info("loaded file", "path", path)
	return result, nil
}

func (e *engineImpl) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".lisp" {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)

	for _, name := range files {
		path := filepath.Join(dir, name)
		if _, err := e.EvalFile(path); err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
	}

	return nil
}

func (e *engineImpl) Call(ctx context.Context, name string, args ...core.Value) (core.Value, error) {
	start := time.Now()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var cancel context.CancelFunc
	if e.config.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.config.timeout)
		defer cancel()
	}

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	fn, ok := env.Get(name)
	if !ok {
		dur := time.Since(start)
		e.stats.recordPluginCall(name, dur)
		e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: dur})
		return nil, fmt.Errorf("undefined function: %s", name)
	}

	result, err := e.evaluator.Apply(ctx, fn, args, env)
	dur := time.Since(start)
	e.stats.recordPluginCall(name, dur)
	e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: dur})
	return result, err
}

func (e *engineImpl) Bind(name string, v core.Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.registry.HasPrefix(name) {
		return fmt.Errorf("bind: name %q conflicts with registered plugin namespace", name)
	}

	e.rootEnv.Set(name, v)
	e.logger.Debug("bind", "name", name)
	return nil
}

func (e *engineImpl) EvalWithBindings(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, error) {
	start := time.Now()

	forms, err := core.Read(source)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	childEnv := e.rootEnv.Child()
	e.mu.RUnlock()

	for name, val := range bindings {
		childEnv.Set(name, val)
	}

	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, childEnv)
		if err != nil {
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	return result, nil
}

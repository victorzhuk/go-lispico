package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

type macroExpander interface {
	MacroExpand(ctx context.Context, form core.Value, env *core.Env) (core.Value, error)
}

// bytecodeEvaluator runs Lisp forms through the bytecode VM with per-evaluation
// isolation: each Eval gets a fresh compiler and a fresh VM state.
type bytecodeEvaluator struct {
	globals  *core.Env
	maxDepth int
	macro    macroExpander
	tree     core.Evaluator
}

func newBytecodeEvaluator(globals *core.Env, maxDepth int, treeWalker core.Evaluator) *bytecodeEvaluator {
	return &bytecodeEvaluator{
		globals:  globals,
		maxDepth: maxDepth,
		macro:    treeWalker.(macroExpander),
		tree:     treeWalker,
	}
}

func (be *bytecodeEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	expanded, err := be.macro.MacroExpand(ctx, form, env)
	if err != nil {
		return nil, fmt.Errorf("macro expand: %w", err)
	}
	comp := compiler.NewCompiler("<eval>")
	if err := comp.Compile(expanded); err != nil {
		if isUnsupportedInBytecode(err) {
			return be.tree.Eval(ctx, expanded, env)
		}
		return nil, fmt.Errorf("compile: %w", err)
	}
	comp.Chunk().Emit(vm.OpReturn, 0)
	fresh := vm.New(env, vm.WithMaxDepth(be.maxDepth), vm.WithEvaluator(be))
	return fresh.Run(ctx, comp.Chunk())
}

func (be *bytecodeEvaluator) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	fresh := vm.New(env, vm.WithMaxDepth(be.maxDepth), vm.WithEvaluator(be))
	return fresh.Apply(ctx, fn, args, env)
}

// isUnsupportedInBytecode reports whether err is the compiler's typed
// "unsupported in bytecode" error (defmacro nested in a body, unquote-splicing),
// so the caller can fall back to the tree-walker instead of failing the eval.
func isUnsupportedInBytecode(err error) bool {
	var lispErr *core.LispicoError
	return errors.As(err, &lispErr) && lispErr.Code == compiler.CodeUnsupported
}

func (e *engineImpl) Eval(ctx context.Context, source, input string) (core.Value, error) {
	start := time.Now()

	var cancel context.CancelFunc
	if e.config.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.config.timeout)
		defer cancel()
	}

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

	var cancel context.CancelFunc
	if e.config.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.config.timeout)
		defer cancel()
	}

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

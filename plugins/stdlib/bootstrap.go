package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

type bootstrapEntry struct {
	source   string
	reusable bool
}

type stdlibBootstrapEvaluator interface {
	EvalStdlibBootstrap(ctx context.Context, source string, env *core.Env) (core.Value, error)
}

func stdlibBootstrapEntries() []bootstrapEntry {
	return []bootstrapEntry{
		{source: `(defmacro -> [x & forms]
  (reduce (fn [acc form] (if (list? form) (cons (first form) (cons acc (rest form))) (list form acc))) x forms))`},

		{source: `(defmacro ->> [x & forms]
  (reduce (fn [acc form] (if (list? form) (concat form (list acc)) (list form acc))) x forms))`},

		{source: `(defmacro as-> [expr name & forms]
  (let* [bindings (reduce (fn [acc form] (conj acc name form)) [name expr] forms)]
    (list (quote let*) bindings name)))`},

		{source: `(defmacro if-let [bindings then else]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))`},

		{source: `(defmacro when-let [bindings & body]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))`},

		{source: `(defn get-in [m ks]
  (reduce (fn [acc k] (get acc k)) m ks))`, reusable: true},
	}
}

func (p *Plugin) mirrorBootstrapBindings(env *core.Env, before map[string]struct{}) {
	for _, name := range env.VarNames() {
		if _, existed := before[name]; existed {
			continue
		}
		if _, inFuncs := env.GetFunc(name); inFuncs {
			continue
		}
		if v, ok := env.Get(name); ok {
			env.SetFunc(name, v)
		}
	}
}

func (p *Plugin) loadBootstrap(env *core.Env) error {
	bootstrapCode := stdlibBootstrapEntries()

	// The bootstrap macros are defined with the full-kernel evaluator so they
	// work even when the engine's dialect (e.g. EmptyDialect) drops defmacro.
	// After definition, newly-added names are mirrored to the function cell so
	// they resolve in head position under Lisp-2. Under Lisp-1 the function cell
	// is unused, so the copy is harmless.
	evaluator := core.NewEvaluator()
	ctx := context.Background()
	cacheEval, _ := env.Evaluator().(stdlibBootstrapEvaluator)

	before := make(map[string]struct{})
	for _, name := range env.VarNames() {
		before[name] = struct{}{}
	}
	for _, name := range env.FuncNames() {
		before[name] = struct{}{}
	}

	for _, entry := range bootstrapCode {
		if entry.reusable && cacheEval != nil {
			_, err := cacheEval.EvalStdlibBootstrap(ctx, entry.source, env)
			if err != nil {
				return fmt.Errorf("bootstrap eval: %w", err)
			}
			continue
		}

		forms, err := core.Read(entry.source)
		if err != nil {
			return fmt.Errorf("bootstrap read: %w", err)
		}

		for _, form := range forms {
			_, err = evaluator.Eval(ctx, form, env)
			if err != nil {
				return fmt.Errorf("bootstrap eval: %w", err)
			}
		}
	}

	p.mirrorBootstrapBindings(env, before)

	return nil
}

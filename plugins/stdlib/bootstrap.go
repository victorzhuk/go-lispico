package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

type bootstrapEntry struct {
	name     string
	source   string
	reusable bool
}

type stdlibBootstrapEvaluator interface {
	EvalStdlibBootstrap(ctx context.Context, source string, env *core.Env) (core.Value, error)
}

func stdlibBootstrapEntries() []bootstrapEntry {
	return []bootstrapEntry{
		{name: "->", source: `(defmacro -> [x & forms]
  (reduce (fn [acc form] (if (list? form) (cons (first form) (cons acc (rest form))) (list form acc))) x forms))`},

		{name: "->>", source: `(defmacro ->> [x & forms]
  (reduce (fn [acc form] (if (list? form) (concat form (list acc)) (list form acc))) x forms))`},

		{name: "as->", source: `(defmacro as-> [expr name & forms]
  (let* [bindings (reduce (fn [acc form] (conj acc name form)) [name expr] forms)]
    (list (quote let*) bindings name)))`},

		{name: "if-let", source: `(defmacro if-let [bindings then else]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))`},

		{name: "when-let", source: `(defmacro when-let [bindings & body]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))`},
	}
}

func (p *Plugin) loadBootstrap(env *core.Env) error {
	if env.Evaluator() == nil {
		env.SetEvaluator(core.NewEvaluator())
	}
	owner := env.Evaluator()
	definer, ok := owner.(core.BootstrapDefiner)
	if !ok {
		return fmt.Errorf("stdlib bootstrap: installed evaluator %T does not implement core.BootstrapDefiner", owner)
	}

	lisp2 := false
	if axis, ok := owner.(interface{ IsLisp2() bool }); ok {
		lisp2 = axis.IsLisp2()
	}

	ctx := context.Background()
	cacheEval, _ := owner.(stdlibBootstrapEvaluator)
	for _, entry := range stdlibBootstrapEntries() {
		// A lazy layer defers the definition behind first touch; the
		// materializer executes this same source then (see
		// stdlibLazyMaterializer.materializeBootstrap).
		if env.RegisterSource(entry.name, entry.source, entry.reusable) {
			continue
		}
		if entry.reusable && cacheEval != nil {
			if _, err := cacheEval.EvalStdlibBootstrap(ctx, entry.source, env); err != nil {
				return fmt.Errorf("bootstrap eval: %w", err)
			}
		} else if _, err := definer.DefineBootstrap(ctx, entry.source, env); err != nil {
			return fmt.Errorf("bootstrap eval: %w", err)
		}
		// The definition lands in the cell the owner's dialect owns. Under
		// a Lisp-2 owner, fill an empty function cell so head-position
		// resolution never misses; under Lisp-1 the value cell is the single
		// namespace and must not gain a func-cell mirror. Only an empty cell
		// is filled, so a user binding always wins.
		if lisp2 && !env.HasLiveFunc(entry.name) {
			if v, ok := env.Get(entry.name); ok {
				if err := env.SetFunc(entry.name, v); err != nil {
					return fmt.Errorf("bootstrap eval: %w", err)
				}
			}
		}
	}

	return nil
}

package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) loadBootstrap(env *core.Env) error {
	bootstrapCode := []string{
		`(defmacro -> [x & forms]
  (reduce (fn [acc form] (if (list? form) (cons (first form) (cons acc (rest form))) (list form acc))) x forms))`,

		`(defmacro ->> [x & forms]
  (reduce (fn [acc form] (if (list? form) (concat form (list acc)) (list form acc))) x forms))`,

		`(defmacro as-> [expr name & forms]
  (let* [bindings (reduce (fn [acc form] (conj acc name form)) [name expr] forms)]
    (list (quote let*) bindings name)))`,

		`(defmacro if-let [bindings then else]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))`,

		`(defmacro when-let [bindings & body]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))`,

		`(defn get-in [m ks]
  (reduce (fn [acc k] (get acc k)) m ks))`,
	}

	// The bootstrap macros are defined with the full-kernel evaluator so they
	// work even when the engine's dialect (e.g. EmptyDialect) drops defmacro.
	// After definition, newly-added names are mirrored to the function cell so
	// they resolve in head position under Lisp-2. Under Lisp-1 the function cell
	// is unused, so the copy is harmless.
	evaluator := core.NewEvaluator()
	ctx := context.Background()

	before := make(map[string]struct{})
	for _, name := range env.VarNames() {
		before[name] = struct{}{}
	}
	for _, name := range env.FuncNames() {
		before[name] = struct{}{}
	}

	for _, code := range bootstrapCode {
		forms, err := core.Read(code)
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

	return nil
}

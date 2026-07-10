package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) loadBootstrap(env *core.Env) error {
	bootstrapCode := []string{
		`(defmacro -> [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let* [form (first fs)
             threaded (if (list? form)
                        (cons (first form) (cons acc (rest form)))
                        (list form acc))]
        (recur threaded (rest fs))))))`,

		`(defmacro ->> [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let* [form (first fs)
             threaded (if (list? form)
                        (concat form (list acc))
                        (list form acc))]
        (recur threaded (rest fs))))))`,

		`(defmacro as-> [expr name & forms]
  (let* [pairs (loop [acc []
                      fs forms]
                 (if (empty? fs)
                   acc
                   (recur (conj acc name (first fs)) (rest fs))))]
    (list (quote let) (apply vector (conj pairs name expr)) name)))`,

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

	// Prefer the engine's dialect-aware evaluator so bootstrap macros bind into the correct cell.
	evaluator := env.Evaluator()
	if evaluator == nil {
		evaluator = core.NewEvaluator()
	}
	ctx := context.Background()

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
	return nil
}

package llm

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) loadBootstrap(env *core.Env) error {
	bootstrapCode := []string{
		`(defmacro prompt [& parts]
  (cons (quote str) parts))`,

		`(defmacro defprompt [name params & body]
  (list (quote defn) name params (cons (quote str) body)))`,

		`(defmacro with-model [model & body]
  (list (quote let) (vector (quote *model*) model)
    (cons (quote do) body)))`,

		`(defmacro with-temp [temp & body]
  (list (quote let) (vector (quote *temperature*) temp)
    (cons (quote do) body)))`,
	}

	evaluator := core.NewEvaluator()
	ctx := context.Background()

	for _, code := range bootstrapCode {
		forms, err := core.Read(code)
		if err != nil {
			return fmt.Errorf("llm bootstrap read: %w", err)
		}

		for _, form := range forms {
			_, err = evaluator.Eval(ctx, form, env)
			if err != nil {
				return fmt.Errorf("llm bootstrap eval: %w", err)
			}
		}
	}
	return nil
}

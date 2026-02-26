package agent

import (
	"context"
	"embed"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

//go:embed bootstrap.lisp
var bootstrapFS embed.FS

func (p *Plugin) loadBootstrap(env *core.Env) error {
	content, err := bootstrapFS.ReadFile("bootstrap.lisp")
	if err != nil {
		return fmt.Errorf("agent bootstrap read file: %w", err)
	}

	forms, err := core.Read(string(content))
	if err != nil {
		return fmt.Errorf("agent bootstrap read: %w", err)
	}

	evaluator := core.NewEvaluator()
	ctx := context.Background()

	for _, form := range forms {
		_, err = evaluator.Eval(ctx, form, env)
		if err != nil {
			return fmt.Errorf("agent bootstrap eval: %w", err)
		}
	}
	return nil
}

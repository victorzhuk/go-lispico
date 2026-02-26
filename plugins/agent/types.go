package agent

import "context"

type LLMCaller interface {
	Complete(ctx context.Context, model, system, prompt string) (string, error)
}

type Agent struct {
	ID          string
	Model       string
	Temperature float64
	MaxTokens   int
	System      string
	Tools       []string
	CanDelegate []string
}

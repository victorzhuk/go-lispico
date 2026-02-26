package llm

import (
	"context"
	"encoding/json"
)

type LLMRequest struct {
	Model       string
	System      string
	User        string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	Tools       []ToolSpec
	StopSeqs    []string
	Stream      bool
}

type Message struct {
	Role    string
	Content string
}

type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type LLMResponse struct {
	Content    string
	StopReason string
	ToolCalls  []ToolCall
	Usage      TokenUsage
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type LLMChunk struct {
	Content string
	Done    bool
	Err     error
}

type LLMClient interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
	Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
	Embed(ctx context.Context, text string, model string) ([]float64, error)
}

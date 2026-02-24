# Design Document: LLM Plugin

**Change ID:** 004-llm-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package llm

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/victorzhuk/go-lispico/core"
)

// LLMClient is the interface for LLM backends
type LLMClient interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
    Embed(ctx context.Context, text string, model string) ([]float64, error)
}

type Plugin struct {
    client LLMClient
}

func New(client LLMClient) *Plugin {
    return &Plugin{client: client}
}

func (p *Plugin) Name() string {
    return "llm"
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "LLM API bindings for go-lispico",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    // Register functions
    env.Set("llm/complete", core.GoFunc{
        Name: "llm/complete",
        Fn:   p.complete,
    })
    
    env.Set("llm/complete*", core.GoFunc{
        Name: "llm/complete*",
        Fn:   p.completeStar,
    })
    
    env.Set("llm/stream", core.GoFunc{
        Name: "llm/stream",
        Fn:   p.stream,
    })
    
    env.Set("llm/chat", core.GoFunc{
        Name: "llm/chat",
        Fn:   p.chat,
    })
    
    env.Set("llm/embed", core.GoFunc{
        Name: "llm/embed",
        Fn:   p.embed,
    })
    
    env.Set("llm/tool-call", core.GoFunc{
        Name: "llm/tool-call",
        Fn:   p.toolCall,
    })
    
    return nil
}
```

---

## 2. Request/Response Types

```go
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
    Role    string // "system", "user", "assistant"
    Content string
}

type ToolSpec struct {
    Name        string
    Description string
    Parameters  map[string]any // JSON Schema
}

type LLMResponse struct {
    Content    string
    StopReason string
    ToolCalls  []ToolCall
    Usage      TokenUsage
}

type ToolCall struct {
    Name      string
    Arguments map[string]any
}

type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
}

type LLMChunk struct {
    Content string
    Done    bool
}
```

---

## 3. Function Implementations

### llm/complete

```go
func (p *Plugin) complete(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 3 {
        return nil, fmt.Errorf("llm/complete: requires 3 arguments (model, system, user)")
    }
    
    model, ok1 := args[0].(core.String)
    system, ok2 := args[1].(core.String)
    user, ok3 := args[2].(core.String)
    
    if !ok1 || !ok2 || !ok3 {
        return nil, fmt.Errorf("llm/complete: all arguments must be strings")
    }
    
    req := LLMRequest{
        Model:  model.V,
        System: system.V,
        User:   user.V,
    }
    
    resp, err := p.client.Complete(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("llm/complete: %w", err)
    }
    
    return core.String{V: resp.Content}, nil
}
```

### llm/complete*

```go
func (p *Plugin) completeStar(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("llm/complete*: requires 1 argument (opts map)")
    }
    
    opts, ok := args[0].(*core.HashMap)
    if !ok {
        return nil, fmt.Errorf("llm/complete*: argument must be a map")
    }
    
    req := LLMRequest{}
    
    // Extract options from map
    if v, ok := opts.Get(core.Keyword{V: "model"}); ok {
        if s, ok := v.(core.String); ok {
            req.Model = s.V
        }
    }
    
    if v, ok := opts.Get(core.Keyword{V: "system"}); ok {
        if s, ok := v.(core.String); ok {
            req.System = s.V
        }
    }
    
    if v, ok := opts.Get(core.Keyword{V: "user"}); ok {
        if s, ok := v.(core.String); ok {
            req.User = s.V
        }
    }
    
    if v, ok := opts.Get(core.Keyword{V: "max-tokens"}); ok {
        if i, ok := v.(core.Int); ok {
            req.MaxTokens = int(i.V)
        }
    }
    
    if v, ok := opts.Get(core.Keyword{V: "temperature"}); ok {
        if f, ok := v.(core.Float); ok {
            req.Temperature = f.V
        } else if i, ok := v.(core.Int); ok {
            req.Temperature = float64(i.V)
        }
    }
    
    resp, err := p.client.Complete(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("llm/complete*: %w", err)
    }
    
    return core.String{V: resp.Content}, nil
}
```

### llm/stream

```go
func (p *Plugin) stream(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("llm/stream: requires 2 arguments (opts, handler)")
    }
    
    opts, ok1 := args[0].(*core.HashMap)
    handler, ok2 := args[1].(core.Lambda)
    
    if !ok1 {
        return nil, fmt.Errorf("llm/stream: first argument must be a map")
    }
    
    if !ok2 {
        return nil, fmt.Errorf("llm/stream: second argument must be a function")
    }
    
    // Build request from opts (similar to complete*)
    req := LLMRequest{}
    // ... extract options
    req.Stream = true
    
    chunks, err := p.client.Stream(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("llm/stream: %w", err)
    }
    
    // Call handler for each chunk
    for chunk := range chunks {
        chunkMap := core.NewHashMap()
        chunkMap.M[core.Keyword{V: "content"}] = core.String{V: chunk.Content}
        chunkMap.M[core.Keyword{V: "done"}] = core.Bool{V: chunk.Done}
        
        // Apply handler - note: this requires evaluator access
        // In real implementation, would need to pass evaluator
        _, _ = handler, chunkMap
    }
    
    return core.Nil{}, nil
}
```

### llm/chat

```go
func (p *Plugin) chat(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("llm/chat: requires 2 arguments (model, messages)")
    }
    
    model, ok1 := args[0].(core.String)
    messagesList, ok2 := args[1].(core.List)
    
    if !ok1 {
        return nil, fmt.Errorf("llm/chat: first argument must be a string")
    }
    
    if !ok2 {
        return nil, fmt.Errorf("llm/chat: second argument must be a list")
    }
    
    var messages []Message
    for _, item := range messagesList.Items {
        msgMap, ok := item.(*core.HashMap)
        if !ok {
            return nil, fmt.Errorf("llm/chat: messages must be maps")
        }
        
        msg := Message{}
        
        if v, ok := msgMap.Get(core.Keyword{V: "role"}); ok {
            if s, ok := v.(core.Keyword); ok {
                msg.Role = s.V
            } else if s, ok := v.(core.String); ok {
                msg.Role = s.V
            }
        }
        
        if v, ok := msgMap.Get(core.Keyword{V: "content"}); ok {
            if s, ok := v.(core.String); ok {
                msg.Content = s.V
            }
        }
        
        messages = append(messages, msg)
    }
    
    req := LLMRequest{
        Model:    model.V,
        Messages: messages,
    }
    
    resp, err := p.client.Complete(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("llm/chat: %w", err)
    }
    
    return core.String{V: resp.Content}, nil
}
```

### llm/embed

```go
func (p *Plugin) embed(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 1 || len(args) > 2 {
        return nil, fmt.Errorf("llm/embed: requires 1-2 arguments (text [model])")
    }

    text, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("llm/embed: argument must be a string")
    }

    // Use default model; caller can pass model as second optional arg
    model := ""
    if len(args) == 2 {
        if m, ok := args[1].(core.String); ok {
            model = m.V
        }
    }
    embedding, err := p.client.Embed(ctx, text.V, model)
    if err != nil {
        return nil, fmt.Errorf("llm/embed: %w", err)
    }
    
    // Convert to vector
    items := make([]core.Value, len(embedding))
    for i, f := range embedding {
        items[i] = core.Float{V: f}
    }
    
    return core.Vector{Items: items}, nil
}
```

### llm/tool-call

```go
func (p *Plugin) toolCall(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("llm/tool-call: requires 2 arguments (opts, tools)")
    }
    
    // Parse options and tools
    // Build request with tools
    // Execute
    // Return tool call results
    
    return core.List{}, nil
}
```

---

## 4. HTTP Client Implementation

```go
// HTTPClient implements LLMClient using net/http
type HTTPClient struct {
    baseURL    string
    apiKey     string
    httpClient *http.Client
}

func NewHTTPClient(baseURL, apiKey string) *HTTPClient {
    return &HTTPClient{
        baseURL: baseURL,
        apiKey:  apiKey,
        httpClient: &http.Client{
            Timeout: 60 * time.Second,
        },
    }
}

func (c *HTTPClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
    // Build request body
    body := map[string]any{
        "model": req.Model,
        "messages": buildMessages(req),
    }
    
    if req.MaxTokens > 0 {
        body["max_tokens"] = req.MaxTokens
    }
    
    if req.Temperature > 0 {
        body["temperature"] = req.Temperature
    }
    
    // Marshal
    jsonBody, err := json.Marshal(body)
    if err != nil {
        return LLMResponse{}, err
    }
    
    // Create request
    httpReq, err := http.NewRequestWithContext(ctx, "POST", 
        c.baseURL+"/v1/chat/completions",
        bytes.NewReader(jsonBody))
    if err != nil {
        return LLMResponse{}, err
    }
    
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
    
    // Execute
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return LLMResponse{}, err
    }
    defer resp.Body.Close()
    
    // Read body
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return LLMResponse{}, err
    }
    
    if resp.StatusCode != http.StatusOK {
        return LLMResponse{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
    }
    
    // Parse response
    var result struct {
        Choices []struct {
            Message struct {
                Content   string `json:"content"`
                ToolCalls []struct {
                    Function struct {
                        Name      string          `json:"name"`
                        Arguments json.RawMessage `json:"arguments"`
                    } `json:"function"`
                } `json:"tool_calls"`
            } `json:"message"`
            FinishReason string `json:"finish_reason"`
        } `json:"choices"`
        Usage struct {
            PromptTokens     int `json:"prompt_tokens"`
            CompletionTokens int `json:"completion_tokens"`
            TotalTokens      int `json:"total_tokens"`
        } `json:"usage"`
    }
    
    if err := json.Unmarshal(respBody, &result); err != nil {
        return LLMResponse{}, err
    }
    
    if len(result.Choices) == 0 {
        return LLMResponse{}, fmt.Errorf("no choices in response")
    }
    
    choice := result.Choices[0]
    
    // Parse tool calls
    var toolCalls []ToolCall
    for _, tc := range choice.Message.ToolCalls {
        var args map[string]any
        json.Unmarshal(tc.Function.Arguments, &args)
        toolCalls = append(toolCalls, ToolCall{
            Name:      tc.Function.Name,
            Arguments: args,
        })
    }
    
    return LLMResponse{
        Content:    choice.Message.Content,
        StopReason: choice.FinishReason,
        ToolCalls:  toolCalls,
        Usage: TokenUsage{
            PromptTokens:     result.Usage.PromptTokens,
            CompletionTokens: result.Usage.CompletionTokens,
            TotalTokens:      result.Usage.TotalTokens,
        },
    }, nil
}

func (c *HTTPClient) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
    ch := make(chan LLMChunk, 10)
    
    // Similar to Complete but with streaming
    // Parse SSE events
    // Send chunks to channel
    
    go func() {
        defer close(ch)
        // Implementation...
    }()
    
    return ch, nil
}

func (c *HTTPClient) Embed(ctx context.Context, text string, model string) ([]float64, error) {
    if model == "" {
        model = "text-embedding-3-small"
    }
    body := map[string]any{
        "model": model,
        "input": text,
    }
    
    jsonBody, _ := json.Marshal(body)
    
    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        c.baseURL+"/v1/embeddings",
        bytes.NewReader(jsonBody))
    
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
    
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    respBody, _ := io.ReadAll(resp.Body)
    
    var result struct {
        Data []struct {
            Embedding []float64 `json:"embedding"`
        } `json:"data"`
    }
    
    if err := json.Unmarshal(respBody, &result); err != nil {
        return nil, err
    }
    
    if len(result.Data) == 0 {
        return nil, fmt.Errorf("no embedding in response")
    }
    
    return result.Data[0].Embedding, nil
}

func buildMessages(req LLMRequest) []map[string]string {
    var messages []map[string]string
    
    if req.System != "" {
        messages = append(messages, map[string]string{
            "role":    "system",
            "content": req.System,
        })
    }
    
    if req.User != "" {
        messages = append(messages, map[string]string{
            "role":    "user",
            "content": req.User,
        })
    }
    
    for _, m := range req.Messages {
        messages = append(messages, map[string]string{
            "role":    m.Role,
            "content": m.Content,
        })
    }
    
    return messages
}
```

---

## 5. Bootstrap Lisp (Prompt DSL)

```go
func (p *Plugin) loadBootstrap(env *core.Env) error {
    // prompt and defprompt use only str and defn which are in stdlib
    bootstrapCode := []string{
        // prompt: concatenates non-nil parts
        `(defmacro prompt [& parts]
  (cons (quote str) parts))`,

        // defprompt: defines a named prompt template function
        `(defmacro defprompt [name params & body]
  (list (quote defn) name params (cons (quote str) body)))`,

        // with-model: sets current model via let binding
        `(defmacro with-model [model & body]
  (list (quote let) (vector (quote *model*) model)
    (cons (quote do) body)))`,

        // with-temp: sets current temperature via let binding
        `(defmacro with-temp [temp & body]
  (list (quote let) (vector (quote *temperature*) temp)
    (cons (quote do) body)))`,
    }

    evaluator := core.NewEvaluator()
    for _, code := range bootstrapCode {
        reader := core.NewReader(code)
        tokens, err := reader.Tokenize()
        if err != nil { return fmt.Errorf("llm bootstrap tokenize: %w", err) }

        parser := core.NewParser(tokens)
        form, err := parser.Parse()
        if err != nil { return fmt.Errorf("llm bootstrap parse: %w", err) }
        if form == nil { continue }

        ctx := context.Background()
        if _, err = evaluator.Eval(ctx, form, env); err != nil {
            return fmt.Errorf("llm bootstrap eval: %w", err)
        }
    }
    return nil
}
```

---

## 6. File Organization

```
plugins/llm/
├── plugin.go         # Main plugin
├── types.go          # Request/response types
├── client.go         # HTTPClient implementation
├── streaming.go      # SSE streaming parser
├── bootstrap.lisp    # Prompt DSL macros
└── llm_test.go       # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).

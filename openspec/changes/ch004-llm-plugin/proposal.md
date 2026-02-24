# Change Proposal: LLM Plugin

**Change ID:** 004-llm-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team, AI/Orchestration Team

---

## 1. Summary

Implement the LLM (Large Language Model) plugin for go-lispico, enabling hot-reloadable, macro-powered prompt engineering in production Go services. This plugin bridges the interpreter with any LLM backend via a clean interface abstraction.

**Key Characteristics:**
- Interface-based abstraction (`LLMClient`) — no SDK lock-in
- Support for completion, streaming, embeddings, and tool calling
- Prompt DSL macros for composable templates
- Context propagation for cancellation and timeouts
- Capability-gated: requires `net:http`, `io:env-read` (capability enforcement deferred to future change; document intent only)

---

## 2. Motivation

### Problem
Current approaches to LLM integration in Go services:
- Prompt templates are string-based, hard to compose
- No hot-reload — changing prompts requires restart
- Business logic mixed with LLM calls
- No standardized interface for different providers (OpenAI, Anthropic, local)

### Solution
A Lisp-native LLM integration where:
- Prompts are composable Lisp expressions
- Templates are hot-reloadable `.lisp` files
- Business logic is pure Lisp with clear LLM boundary
- Multiple LLM providers supported via interface

### Success Metrics
- Prompt templates modifiable without service restart
- LLM calls respect context cancellation (< 100ms response)
- Streaming delivers tokens with < 50ms latency
- Tool calling returns structured results
- Support for Anthropic, OpenAI, and OpenAI-compatible APIs

---

## 3. Scope

### In Scope

**LLMClient Interface**
- `Complete(ctx, req)` - synchronous completion
- `Stream(ctx, req)` - streaming tokens
- `Embed(ctx, text)` - text embeddings

**Lisp API**
- `llm/complete` - simple completion (positional args)
- `llm/complete*` - full options (map)
- `llm/stream` - streaming with handler
- `llm/chat` - multi-turn conversation
- `llm/embed` - embeddings
- `llm/tool-call` - function calling

**Prompt DSL Macros**
- `defprompt` - define reusable prompt template
- `with-model` - scoped model selection
- `with-temp` - scoped temperature

**Request/Response Types**
- `LLMRequest` - model, system, messages, max-tokens, temperature, tools
- `LLMResponse` - content, stop-reason, tool-calls, usage
- `LLMChunk` - streaming token chunk

### Out of Scope (Other Plugins)

- Agent orchestration → Change 5 (agent-plugin)
- File I/O for prompt loading → Change 6a (io-plugin)
- JSON handling → Change 6c (data-plugin)
- Specific provider implementations → Host application responsibility

---

## 4. Functional Requirements

### LLMClient Interface

| ID | Requirement | Priority |
|----|-------------|----------|
| L4.1 | Interface has no external dependencies | P0 |
| L4.2 | Complete() returns full response synchronously | P0 |
| L4.3 | Stream() returns channel of chunks | P0 |
| L4.4 | Embed() returns vector of floats | P0 |
| L4.5 | All methods accept context for cancellation | P0 |
| L4.6 | Request/response types are pure Go structs | P0 |

### Lisp API

| ID | Requirement | Priority |
|----|-------------|----------|
| L4.7 | llm/complete accepts model, system, user as strings | P0 |
| L4.8 | llm/complete* accepts options map | P0 |
| L4.9 | llm/stream calls handler for each chunk | P0 |
| L4.10 | llm/chat accepts messages list | P0 |
| L4.11 | llm/embed returns vector | P0 |
| L4.12 | llm/tool-call returns tool results | P0 |
| L4.13 | All calls propagate context | P0 |

### Prompt DSL

| ID | Requirement | Priority |
|----|-------------|----------|
| L4.14 | defprompt creates reusable template function | P0 |
| L4.15 | with-model scopes model for body | P0 |
| L4.16 | with-temp scopes temperature for body | P0 |
| L4.17 | Macros compose correctly (nesting works) | P0 |

### Error Handling

| ID | Requirement | Priority |
|----|-------------|----------|
| L4.18 | Network errors return LispicoError | P0 |
| L4.19 | HTTP status in error for debugging | P0 |
| L4.20 | Timeout errors distinguishable | P0 |

---

## 5. Design Philosophy

### Interface Abstraction

The plugin depends only on `LLMClient` interface, not specific SDKs:

```go
type LLMClient interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
    // Caller must drain the channel or the streaming goroutine will leak.
    Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
    Embed(ctx context.Context, text string) ([]float64, error)
}
```

**Benefits**:
- Swap providers without changing Lisp code
- Mock for testing
- Support custom implementations (local models, proxies)

### Context Propagation

All LLM calls respect Go context:
- Cancellation stops in-flight requests
- Timeouts apply to entire operation
- Deadlines propagated to HTTP client

### Prompt as Code

Prompts are Lisp expressions, not strings:

```lisp
(defprompt review-code [code focus-areas]
  "You are an expert Go developer reviewing code."
  ""
  (str "Review this Go code:\n\ngo\n" code "\n")
  (when focus-areas
    (str "Focus on: " (string/join focus-areas ", "))))

; Usage
(with-model "claude-sonnet-4-6"
  (llm/complete model (review-code my-code ["performance" "security"])))
```

---

## 6. Type Specifications

### Request Types

```go
type LLMRequest struct {
    Model       string
    System      string
    Messages    []Message   // For chat
    MaxTokens   int
    Temperature float64
    Tools       []ToolSpec
    StopSeqs    []string
}

type Message struct {
    Role    string // "system", "user", "assistant"
    Content string
}

// Parameters is a JSON Schema object stored as raw JSON for flexibility.
type ToolSpec struct {
    Name        string
    Description string
    Parameters  json.RawMessage
}
```

### Response Types

```go
type LLMResponse struct {
    Content    string
    StopReason string // "stop", "length", "tool_calls"
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

## 7. Lisp API Reference

**Context propagation**: Go context flows from `engine.Call(ctx, ...)` through the evaluator into GoFunc implementations automatically via the GoFunc `ctx` parameter. Lisp code does not need to pass context explicitly — all `llm/*` functions inherit the caller's context.

### llm/complete

```lisp
(llm/complete model system user) → string

; Example
(llm/complete "claude-sonnet-4-6"
              "You are a helpful assistant."
              "What is the capital of France?")
; => "The capital of France is Paris."
```

### llm/complete*

```lisp
(llm/complete* opts) → string

; Example
(llm/complete* {:model "claude-sonnet-4-6"
                :system "You are a helpful assistant."
                :user "What is 2+2?"
                :max-tokens 100
                :temperature 0.7})
```

### llm/stream

```lisp
(llm/stream opts handler-fn) → nil

; Example
(llm/stream {:model "claude-sonnet-4-6"
             :user "Write a poem about Go"}
            (fn [chunk]
              (print chunk)
              (flush)))
```

### llm/chat

```lisp
(llm/chat model messages) → string

; Example
(llm/chat "claude-sonnet-4-6"
          [{:role "system" :content "You are helpful."}
           {:role "user" :content "Hello!"}
           {:role "assistant" :content "Hi there!"}
           {:role "user" :content "What's 2+2?"}])
```

### llm/embed

```lisp
(llm/embed text) → [float]

; Example
(llm/embed "The quick brown fox")
; => [0.023, -0.456, 0.789, ...]
```

### llm/tool-call

Returns a list of pending tool calls: `[{:name string :args map}]`. The host application is responsible for executing the tools and calling `(llm/tool-result call-id result)` to continue the conversation. tool-call does NOT execute the tools itself.

```lisp
(llm/tool-call opts tools) → [{:name string :args map}]

; Example
(llm/tool-call {:model "claude-sonnet-4-6"
                :user "What's the weather in Paris?"}
               [{:name "get_weather"
                 :description "Get weather for a city"
                 :parameters {:city :string}}])
; => [{:name "get_weather" :args {:city "Paris"}}]
```

---

## 8. Prompt DSL Macros

### defprompt

```lisp
(defmacro defprompt [name params & body]
  `(defn ~name ~params
     (prompt ~@body)))

; Implementation in bootstrap.lisp
defmacro prompt [& parts]
  `(str ~@(filter identity parts)))
```

### with-model

**Rationale**: The original `set!`-based implementation would corrupt model state under concurrent evaluation. Lexical binding via `let` is thread-safe — each call frame has its own `*model*` binding.

```lisp
; with-model scopes the model for the body using lexical binding
; The model is passed as an argument, not via global mutation
(defmacro with-model [model & body]
  `(let [*model* ~model]
     ~@body))

; llm/complete* reads *model* from lexical scope when :model key is absent
; Direct usage:
(with-model "claude-sonnet-4-6"
  (llm/complete* {:system "You are helpful." :user "Hello"}))
```

### with-temp

```lisp
; with-temp scopes temperature for the body using lexical binding
(defmacro with-temp [temp & body]
  `(let [*temperature* ~temp]
     ~@body))
```

---

## 9. Implementation Notes

### HTTP Client

Use Go's `net/http` directly (no external SDK):
- Minimal dependencies
- Full control over request/response
- Easy to customize (retries, logging, etc.)

### Streaming

```go
func (c *client) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
    ch := make(chan LLMChunk, 10)

    go func() {
        defer close(ch)
        // Make request with stream=true
        // Read SSE events
        // Parse each chunk
        // Send to channel; on error: ch <- LLMChunk{Err: fmt.Errorf("llm stream: %w", err), Done: true}
    }()

    return ch, nil
}
```

### Tool Calling Flow

1. LLM receives request with tool specs
2. LLM decides to use tool, returns tool call
3. Plugin parses tool call from response
4. Returns to Lisp code for execution
5. (Host responsibility) Execute tool, send result back

---

## 10. Performance Requirements

| Metric | Target | Notes |
|--------|--------|-------|
| Complete call overhead | < 1ms | Excluding network latency |
| Streaming latency | < 50ms | Time to first token |
| Context cancellation | < 100ms | From cancel to stop |
| Embed 1k tokens | < 500ms | Including network |

---

## 11. Dependencies

### External Dependencies

- `net/http` - HTTP client (stdlib)
- `encoding/json` - JSON encoding (stdlib)
- `context` - Cancellation (stdlib)

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required (str, format)
- **Change 3** (runtime-api): Required (context propagation)

### Dependent Changes

- **Change 5** (agent-plugin): Uses LLM for agent responses

---

## 12. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| API changes by providers | High | Medium | Interface abstraction, version pinning |
| Streaming complexity | Medium | Medium | Thorough testing, clear error handling |
| Context cancellation races | Medium | High | Careful goroutine management |
| Tool call format differences | Medium | Medium | Adapter pattern per provider |

---

## 13. Acceptance Criteria

- [ ] LLMClient interface defined
- [ ] All Lisp API functions working
- [ ] Streaming delivers tokens correctly
- [ ] Tool calling parses responses
- [ ] Prompt DSL macros functional
- [ ] Context cancellation respected
- [ ] Mock client for testing
- [ ] Example implementations (Anthropic, OpenAI)
- [ ] Documentation with examples
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with LLMClient implementation, HTTP handling, and prompt macro expansion.

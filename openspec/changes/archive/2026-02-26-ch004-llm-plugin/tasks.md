# Tasks: LLM Plugin

**Change ID:** 004-llm-plugin  
**Status:** Complete  
**Created:** 2026-02-23  
**Completed:** 2026-02-26  
**Estimated Effort:** 1 week  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Core Types and Interface (Days 1-2)

### Task 1.1: Request/Response Types
- [x] Define LLMRequest struct
- [x] Define LLMResponse struct
- [x] Define Message struct
- [x] Define ToolSpec and ToolCall
- [x] Define TokenUsage
- [x] Write tests
- **Acceptance**: Types defined correctly

### Task 1.2: LLMClient Interface
- [x] Define LLMClient interface
- [x] Complete() method
- [x] Stream() method
- [x] Embed() method
- [x] Write tests
- **Acceptance**: Interface defined

---

## Phase 2: HTTP Client Implementation (Days 3-4)

### Task 2.1: HTTPClient Structure
- [x] Define HTTPClient struct
- [x] Constructor with baseURL/apiKey
- [x] Configure http.Client
- [x] Write tests
- **Acceptance**: Client structure ready

### Task 2.2: Complete Implementation
- [x] Implement Complete()
- [x] Build request body
- [x] Handle response parsing
- [x] Error handling
- [x] Write tests
- **Acceptance**: Complete works

### Task 2.3: Streaming Implementation
- [x] Implement Stream()
- [x] Parse SSE events
- [x] Send chunks to channel
- [x] Handle errors
- [x] Write tests
- **Acceptance**: Streaming works

### Task 2.4: Embed Implementation
- [x] Implement Embed()
- [x] Parse embedding response
- [x] Return float slice
- [x] Write tests
- **Acceptance**: Embeddings work

---

## Phase 3: Lisp API (Days 5-6)

### Task 3.1: Plugin Structure
- [x] Define Plugin struct
- [x] Implement Name() and Metadata()
- [x] Implement Init()
- [x] Write tests
- **Acceptance**: Plugin skeleton ready

### Task 3.2: llm/complete
- [x] Implement complete function
- [x] Handle string arguments
- [x] Error handling
- [x] Write tests
- **Acceptance**: llm/complete works

### Task 3.3: llm/complete*
- [x] Implement complete* function
- [x] Parse options map
- [x] Handle all options
- [x] Write tests
- **Acceptance**: llm/complete* works

### Task 3.4: llm/stream
- [x] Implement stream function
- [x] Handle callback
- [x] Process chunks
- [x] Write tests
- **Acceptance**: llm/stream works

### Task 3.5: llm/chat
- [x] Implement chat function
- [x] Parse messages list
- [x] Build request
- [x] Write tests
- **Acceptance**: llm/chat works

### Task 3.6: llm/embed
- [x] Implement embed function
- [x] Convert to vector
- [x] Write tests
- **Acceptance**: llm/embed works

---

## Phase 4: Prompt DSL (Day 7)

### Task 4.1: Bootstrap Macros
- [x] Create bootstrap.lisp
- [x] Implement prompt macro
- [x] Implement defprompt macro
- [x] Write tests
- **Acceptance**: Macros work

### Task 4.2: Scoped Macros
- [x] Implement with-model
- [x] Implement with-temp
- [x] Write tests
- **Acceptance**: Scoped macros work

---

## Acceptance Criteria

- [x] LLMClient interface defined
- [x] All Lisp API functions working
- [x] Streaming delivers tokens correctly
- [x] Tool calling parses responses
- [x] Prompt DSL macros functional
- [x] Context cancellation respected
- [x] Mock client for testing
- [ ] Example implementations (Anthropic, OpenAI) - deferred to host app
- [ ] Documentation with examples - deferred
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

## Implementation Notes

### Files Created

```
plugins/llm/
├── types.go         # LLMRequest, LLMResponse, Message, ToolSpec, ToolCall, TokenUsage, LLMChunk
├── http_client.go   # HTTPClient struct with Complete() and Embed()
├── streaming.go     # Stream() with SSE parsing
├── plugin.go        # Plugin struct, Init(), and all Lisp API functions
├── bootstrap.go     # Prompt DSL macros (prompt, defprompt, with-model, with-temp)
└── llm_test.go      # Comprehensive test suite
```

### Key Implementation Details

1. **LLMClient Interface**: Clean abstraction allowing mock testing and provider swapping
2. **HTTPClient**: Uses stdlib net/http, 60s timeout, Bearer token auth
3. **Streaming**: SSE parsing with context cancellation support
4. **Lisp API**: All functions accept both List and Vector where sequences are expected
5. **Bootstrap Macros**: Defined in Go using core.Read() and evaluator.Eval()
6. **Tool Calling**: Parses both JSON object and JSON string arguments formats

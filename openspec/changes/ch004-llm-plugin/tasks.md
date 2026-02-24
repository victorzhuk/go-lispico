# Tasks: LLM Plugin

**Change ID:** 004-llm-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1 week  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Core Types and Interface (Days 1-2)

### Task 1.1: Request/Response Types
- [ ] Define LLMRequest struct
- [ ] Define LLMResponse struct
- [ ] Define Message struct
- [ ] Define ToolSpec and ToolCall
- [ ] Define TokenUsage
- [ ] Write tests
- **Acceptance**: Types defined correctly

### Task 1.2: LLMClient Interface
- [ ] Define LLMClient interface
- [ ] Complete() method
- [ ] Stream() method
- [ ] Embed() method
- [ ] Write tests
- **Acceptance**: Interface defined

---

## Phase 2: HTTP Client Implementation (Days 3-4)

### Task 2.1: HTTPClient Structure
- [ ] Define HTTPClient struct
- [ ] Constructor with baseURL/apiKey
- [ ] Configure http.Client
- [ ] Write tests
- **Acceptance**: Client structure ready

### Task 2.2: Complete Implementation
- [ ] Implement Complete()
- [ ] Build request body
- [ ] Handle response parsing
- [ ] Error handling
- [ ] Write tests
- **Acceptance**: Complete works

### Task 2.3: Streaming Implementation
- [ ] Implement Stream()
- [ ] Parse SSE events
- [ ] Send chunks to channel
- [ ] Handle errors
- [ ] Write tests
- **Acceptance**: Streaming works

### Task 2.4: Embed Implementation
- [ ] Implement Embed()
- [ ] Parse embedding response
- [ ] Return float slice
- [ ] Write tests
- **Acceptance**: Embeddings work

---

## Phase 3: Lisp API (Days 5-6)

### Task 3.1: Plugin Structure
- [ ] Define Plugin struct
- [ ] Implement Name() and Metadata()
- [ ] Implement Init()
- [ ] Write tests
- **Acceptance**: Plugin skeleton ready

### Task 3.2: llm/complete
- [ ] Implement complete function
- [ ] Handle string arguments
- [ ] Error handling
- [ ] Write tests
- **Acceptance**: llm/complete works

### Task 3.3: llm/complete*
- [ ] Implement complete* function
- [ ] Parse options map
- [ ] Handle all options
- [ ] Write tests
- **Acceptance**: llm/complete* works

### Task 3.4: llm/stream
- [ ] Implement stream function
- [ ] Handle callback
- [ ] Process chunks
- [ ] Write tests
- **Acceptance**: llm/stream works

### Task 3.5: llm/chat
- [ ] Implement chat function
- [ ] Parse messages list
- [ ] Build request
- [ ] Write tests
- **Acceptance**: llm/chat works

### Task 3.6: llm/embed
- [ ] Implement embed function
- [ ] Convert to vector
- [ ] Write tests
- **Acceptance**: llm/embed works

---

## Phase 4: Prompt DSL (Day 7)

### Task 4.1: Bootstrap Macros
- [ ] Create bootstrap.lisp
- [ ] Implement prompt macro
- [ ] Implement defprompt macro
- [ ] Write tests
- **Acceptance**: Macros work

### Task 4.2: Scoped Macros
- [ ] Implement with-model
- [ ] Implement with-temp
- [ ] Write tests
- **Acceptance**: Scoped macros work

---

## Acceptance Criteria

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

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**

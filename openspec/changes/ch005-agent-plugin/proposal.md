# Change Proposal: Agent Plugin

**Change ID:** 005-agent-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team, AI/Orchestration Team

---

## 1. Summary

Implement the Agent plugin for multi-agent orchestration: registration, routing, parallel execution, and workflow management. Agents are defined declaratively in Lisp and executed concurrently via Go's errgroup pattern.

**Key Characteristics:**
- Declarative agent definitions with `defagent` macro
- Concurrent execution with configurable parallelism
- Delegation chains between agents
- Hot-reloadable agent configurations
- Context propagation for cancellation

---

## 2. Motivation

### Problem
Building multi-agent AI systems requires:
- Agent definitions mixed in Go code
- No hot-reload for agent behavior
- Manual concurrency management
- Hard-coded routing logic

### Solution
A Lisp-native agent system where:
- Agents defined in `.lisp` files with clear configuration
- Workflows composed from agent calls
- Parallel execution handled automatically
- Routing logic is pure Lisp, modifiable at runtime

### Success Metrics
- 10+ agents executing in parallel without errors
- Hot-reload of agent definitions in < 100ms
- Delegation chains work across 3+ agents
- Context cancellation stops all agent execution

---

## 3. Scope

### In Scope

**Agent Definition**
- `defagent` macro - register agent with config
- Configuration: model, temperature, system prompt, tools, delegation

**Agent Execution**
- `agent/run` - execute single agent
- `agent/run-parallel` - execute multiple agents concurrently
- `agent/run-with-ctx` - execute with additional context

**Agent Management**
- `agent/list` - list all registered agents
- `agent/info` - get agent configuration

**Routing & Delegation**
- `agent/route` - named handler registration
- `agent/dispatch` - route-based invocation
- `agent/delegate` - one agent delegates to another

**Concurrency**
- Configurable parallelism limit (default: 5)
- errgroup for goroutine management
- Context cancellation propagation

### Out of Scope

- LLM calls → Change 4 (llm-plugin)
- Persistence → Host application responsibility
- UI/visualization → Future

---

## 4. Functional Requirements

### Agent Definition

| ID | Requirement | Priority |
|----|-------------|----------|
| A5.1 | defagent registers agent with keyword ID | P0 |
| A5.2 | Configuration includes: model, temp, system, tools | P0 |
| A5.3 | can-delegate field specifies allowed targets | P0 |
| A5.4 | Multiple defagent calls merge/override | P1 |

### Execution

| ID | Requirement | Priority |
|----|-------------|----------|
| A5.5 | agent/run calls LLM with agent config | P0 |
| A5.6 | agent/run-parallel runs agents concurrently | P0 |
| A5.7 | Parallelism limited using `golang.org/x/sync/semaphore.Weighted` | P0 |
| A5.8 | agent/run-with-ctx merges context map | P0 |
| A5.9 | All executions respect context cancellation | P0 |
| A5.10 | Results returned as vector (parallel) or string (single) | P0 |

### Routing & Delegation

| ID | Requirement | Priority |
|----|-------------|----------|
| A5.11 | agent/route invokes routing function | P0 |
| A5.12 | Routing returns agent keyword | P0 |
| A5.13 | agent/delegate checks can-delegate whitelist | P0 |
| A5.14 | Delegation chains work (A → B → C) | P0 |
| A5.15 | Delegation depth limited (max 10) | P0 |

### Management

| ID | Requirement | Priority |
|----|-------------|----------|
| A5.16 | agent/list returns vector of keywords | P0 |
| A5.17 | agent/info returns configuration map | P0 |
| A5.18 | Agent registry accessible from Lisp | P1 |

---

## 5. Design Philosophy

### Declarative Configuration

Agents defined as data, not code:

```lisp
(defagent :architect
  :model "claude-opus-4-6"
  :temperature 0.7
  :max-tokens 8192
  :system "You are a senior Go architect..."
  :tools ["web-search" "read-file"]
  :can-delegate [:developer :reviewer])
```

### Pure Routing

Routing logic is pure Lisp:

```lisp
(defn route-task [task]
  (cond
    (= (:type task) :design) :architect
    (= (:type task) :implement) :developer
    (> (:complexity task) 8) :architect
    :else :developer))
```

### Safe Delegation

Delegation only to explicitly allowed agents:

```lisp
; architect can delegate to developer or reviewer
(agent/delegate :architect :developer "Review this code")

; architect CANNOT delegate to expert (not in :can-delegate)
(agent/delegate :architect :expert "Help!")  ; Error!
```

### Inter-Plugin Communication via Evaluator

The agent plugin calls LLM functionality by evaluating `(llm/complete ...)` Lisp expressions
through the Evaluator interface, not by importing the LLM plugin's Go types directly.
This maintains plugin isolation.

At Init() time, the agent plugin receives the Evaluator:
```go
func (p *Plugin) Init(env *core.Env) error {
    p.eval = env.Evaluator() // Evaluator stored for later use
    env.Set("agent/run", core.GoFunc{
        Name: "agent/run",
        Fn: func(ctx context.Context, eval core.Evaluator, args []Value, env *core.Env) (Value, error) {
            // Call LLM via evaluator - maintains isolation
            result, err := eval.Eval(ctx, buildLLMCall(agent, prompt), env)
            ...
        },
    })
    return nil
}
```

**Rationale**: Direct Go imports between plugins create tight coupling and circular dependency risks.
All inter-plugin calls flow through the Lisp evaluator.

---

## 6. Lisp API Reference

### defagent

```lisp
(defagent :id
  :model string
  :temperature float
  :max-tokens int
  :system string
  :tools [string]
  :can-delegate [:keyword])

; Example
(defagent :developer
  :model "claude-sonnet-4-6"
  :temperature 0.3
  :system "You write production Go code..."
  :tools ["bash" "write-file"]
  :can-delegate [:reviewer])
```

### agent/run

```lisp
(agent/run :id prompt) → string

; Example
(agent/run :developer "Implement a Fibonacci function in Go")
; => "Here is a Fibonacci function..."
```

### agent/run-parallel

```lisp
(agent/run-parallel [:id1 :id2 ...] prompt) → [string]

; Example
(agent/run-parallel [:reviewer-a :reviewer-b] my-code)
; => ["Review from A..." "Review from B..."]
```

### agent/run-with-ctx

```lisp
(agent/run-with-ctx :id prompt ctx-map) → string

; Example
(agent/run-with-ctx :developer
                    "Implement the design"
                    {:design doc :constraints ["fast" "simple"]})
```

### agent/list

```lisp
(agent/list) → [:keyword]

; Example
(agent/list)
; => [:architect :developer :reviewer]
```

### agent/info

```lisp
(agent/info :id) → map

; Example
(agent/info :developer)
; => {:id :developer
;     :model "claude-sonnet-4-6"
;     :temperature 0.3
;     ...}
```

### agent/route

```lisp
(agent/route name handler-fn) → nil

; Register a named route handler for agent requests
(agent/route "analyze-code"
  (fn [req]
    (llm/complete "claude-sonnet-4-6"
                  "You are a code analysis expert."
                  (:input req))))

; Routes are dispatched by agent/dispatch or agent/run
```

### agent/dispatch

```lisp
(agent/dispatch name input) → Value

; Dispatch to a registered route handler
(agent/dispatch "analyze-code" {:input "func foo() {}"})
```

### agent/delegate

```lisp
(agent/delegate :from :to prompt) → string

; Example
(agent/delegate :architect :developer "Implement this design...")
; Delegates from architect to developer
```

---

## 7. Concurrency Design

### errgroup Pattern

```go
func (p *Plugin) runParallel(ctx context.Context, ids []Keyword, prompt string) ([]string, error) {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(p.maxParallel) // Configurable limit
    
    results := make([]string, len(ids))
    
    for i, id := range ids {
        i, id := i, id // capture
        g.Go(func() error {
            result, err := p.runAgent(ctx, id, prompt)
            if err != nil {
                return fmt.Errorf("run agent %v: %w", id, err)
            }
            results[i] = result
            return nil
        })
    }
    
    if err := g.Wait(); err != nil {
        return nil, err
    }
    return results, nil
}
```

### Semaphore for Single Execution

```go
import "golang.org/x/sync/semaphore"

type Plugin struct {
    sem *semaphore.Weighted
}

func New(maxParallel int64) *Plugin {
    return &Plugin{sem: semaphore.NewWeighted(maxParallel)}
}

func (p *Plugin) runAgent(ctx context.Context, id Keyword, prompt string) (string, error) {
    if err := p.sem.Acquire(ctx, 1); err != nil {
        return "", fmt.Errorf("acquire semaphore: %w", err)
    }
    defer p.sem.Release(1)
    // ...
}
```

---

## 8. Agent Registry

### Data Structure

```go
type Agent struct {
    ID           Keyword
    Model        string
    Temperature  float64
    MaxTokens    int
    System       string
    Tools        []string
    CanDelegate  []Keyword
}

type Registry struct {
    mu     sync.RWMutex
    agents map[Keyword]*Agent
}
```

### Thread Safety

- Read lock: `agent/list`, `agent/info`
- Write lock: `defagent` (registration)
- Lock-free during execution (immutable agent configs)

---

## 9. Delegation Chain

### Depth Limiting

```go
const maxDelegationDepth = 10

func (p *Plugin) delegate(ctx context.Context, from, to Keyword, prompt string, depth int) (string, error) {
    if depth > maxDelegationDepth {
        return "", fmt.Errorf("delegate %v→%v: max depth %d exceeded", from, to, maxDelegationDepth)
    }
    
    // Check whitelist...
    // Execute...
    // If delegate again, increment depth
}
```

### Whitelist Enforcement

```go
func (a *Agent) CanDelegateTo(target Keyword) bool {
    for _, allowed := range a.CanDelegate {
        if allowed == target {
            return true
        }
    }
    return false
}
```

---

## 10. Example Workflow

```lisp
;; Define agents
(defagent :architect
  :model "claude-opus-4-6"
  :system "You are a senior Go architect..."
  :can-delegate [:developer :reviewer])

(defagent :developer
  :model "claude-sonnet-4-6"
  :system "You write production Go code..."
  :can-delegate [:reviewer])

(defagent :reviewer
  :model "claude-sonnet-4-6"
  :system "You review Go code for quality...")

;; Routing function
(defn route-task [task]
  (cond
    (= (:type task) :design) :architect
    (= (:type task) :implement) :developer
    :else :developer))

;; Workflow
(defn feature-workflow [spec]
  (let [agent-id (route-task {:type :design :spec spec})
        design (agent/run agent-id
                 (str "Design implementation for: " spec))
        code (agent/run :developer
               (str "Implement this design:\n" design))
        reviews (agent/run-parallel [:reviewer] code)
        approved? (string/contains? (first reviews) "APPROVED")]
    {:design design
     :code code
     :review (first reviews)
     :approved approved?}))

;; Run workflow
(feature-workflow "Add user authentication with JWT")
```

---

## 11. Performance Requirements

| Metric | Target | Notes |
|--------|--------|-------|
| Agent registration | < 1ms | defagent call |
| Single agent execution | Network bound | LLM latency dominates |
| Parallel execution (5 agents) | < 1.2x single | Minimal overhead |
| Context cancellation | < 100ms | Stop all agents |
| Delegation overhead | < 1ms | Per hop |

---

## 12. Dependencies

### External Dependencies

- `golang.org/x/sync/errgroup` - Concurrent execution
- `golang.org/x/sync/semaphore` - Parallelism limiting
- `context` - Cancellation propagation

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required
- **Change 3** (runtime-api): Required
- **Change 4** (llm-plugin): Required (for LLM calls)

### Dependent Changes

None (terminal plugin in AI stack).

---

## 13. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Concurrent execution bugs | Medium | High | Thorough testing, race detector |
| Delegation infinite loops | Low | High | Depth limit, cycle detection |
| LLM rate limiting | High | Medium | Exponential backoff, queue |
| Context cancellation races | Medium | Medium | Careful errgroup usage |

---

## 14. Acceptance Criteria

- [ ] defagent macro registers agents
- [ ] agent/run executes single agent
- [ ] agent/run-parallel runs concurrently
- [ ] Parallelism limit enforced
- [ ] Context cancellation works
- [ ] agent/route invokes routing
- [ ] agent/delegate respects whitelist
- [ ] Delegation depth limit enforced
- [ ] Agent registry introspection works
- [ ] Example workflows documented
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with Registry implementation, concurrency patterns, and delegation flow.

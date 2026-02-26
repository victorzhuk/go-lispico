# Design Document: Agent Plugin

**Change ID:** 005-agent-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package agent

import (
    "context"
    "fmt"
    "sync"
    
    "golang.org/x/sync/errgroup"
    
    "github.com/victorzhuk/go-lispico/core"
)

// LLMCaller is the interface for LLM calls
type LLMCaller interface {
    Complete(ctx context.Context, model, system, prompt string) (string, error)
}

type Plugin struct {
    llm       LLMCaller
    registry  *Registry
    maxParallel int
}

type Registry struct {
    mu     sync.RWMutex
    agents map[string]*Agent
}

type Agent struct {
    ID           string
    Model        string
    Temperature  float64
    MaxTokens    int
    System       string
    Tools        []string
    CanDelegate  []string
}

func New(llm LLMCaller) *Plugin {
    return &Plugin{
        llm:         llm,
        registry:    newRegistry(),
        maxParallel: 5,
    }
}

func (p *Plugin) Name() string {
    return "agent"
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "Agent orchestration for go-lispico",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    env.Set("defagent", core.GoFunc{
        Name: "defagent",
        Fn:   p.defagent,
    })
    
    env.Set("agent/run", core.GoFunc{
        Name: "agent/run",
        Fn:   p.run,
    })
    
    env.Set("agent/run-parallel", core.GoFunc{
        Name: "agent/run-parallel",
        Fn:   p.runParallel,
    })
    
    env.Set("agent/run-with-ctx", core.GoFunc{
        Name: "agent/run-with-ctx",
        Fn:   p.runWithCtx,
    })
    
    env.Set("agent/list", core.GoFunc{
        Name: "agent/list",
        Fn:   p.list,
    })
    
    env.Set("agent/info", core.GoFunc{
        Name: "agent/info",
        Fn:   p.info,
    })
    
    env.Set("agent/route", core.GoFunc{
        Name: "agent/route",
        Fn:   p.route,
    })
    
    env.Set("agent/delegate", core.GoFunc{
        Name: "agent/delegate",
        Fn:   p.delegate,
    })
    
    return nil
}

// defagent registers a new agent definition.
// Usage: (defagent :id :model "model-name" :system "..." :can-delegate [:other])
// Called at Lisp load time as a regular function with keyword-value pairs.
func (p *Plugin) defagent(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 1 {
        return nil, fmt.Errorf("defagent: requires at least an id keyword")
    }

    id, ok := args[0].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("defagent: first argument must be keyword id")
    }

    agent := &Agent{ID: id.V}

    // Parse keyword-value pairs
    for i := 1; i < len(args)-1; i += 2 {
        key, ok := args[i].(core.Keyword)
        if !ok {
            return nil, fmt.Errorf("defagent: expected keyword at position %d", i)
        }

        switch key.V {
        case "model":
            if s, ok := args[i+1].(core.String); ok {
                agent.Model = s.V
            }
        case "system":
            if s, ok := args[i+1].(core.String); ok {
                agent.System = s.V
            }
        case "temperature":
            switch v := args[i+1].(type) {
            case core.Float:
                agent.Temperature = v.V
            case core.Int:
                agent.Temperature = float64(v.V)
            }
        case "max-tokens":
            if v, ok := args[i+1].(core.Int); ok {
                agent.MaxTokens = int(v.V)
            }
        case "tools":
            if list, ok := args[i+1].(core.List); ok {
                for _, item := range list.Items {
                    if s, ok := item.(core.String); ok {
                        agent.Tools = append(agent.Tools, s.V)
                    }
                }
            } else if vec, ok := args[i+1].(core.Vector); ok {
                for _, item := range vec.Items {
                    if s, ok := item.(core.String); ok {
                        agent.Tools = append(agent.Tools, s.V)
                    }
                }
            }
        case "can-delegate":
            if list, ok := args[i+1].(core.List); ok {
                for _, item := range list.Items {
                    if kw, ok := item.(core.Keyword); ok {
                        agent.CanDelegate = append(agent.CanDelegate, kw.V)
                    }
                }
            } else if vec, ok := args[i+1].(core.Vector); ok {
                for _, item := range vec.Items {
                    if kw, ok := item.(core.Keyword); ok {
                        agent.CanDelegate = append(agent.CanDelegate, kw.V)
                    }
                }
            }
        }
    }

    p.registry.Register(agent)
    return id, nil
}

func newRegistry() *Registry {
    return &Registry{
        agents: make(map[string]*Agent),
    }
}

func (r *Registry) Register(a *Agent) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.agents[a.ID] = a
}

func (r *Registry) Get(id string) (*Agent, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    a, ok := r.agents[id]
    return a, ok
}

func (r *Registry) List() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    var ids []string
    for id := range r.agents {
        ids = append(ids, id)
    }
    return ids
}
```

---

## 2. Function Implementations

### agent/run

```go
func (p *Plugin) run(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("agent/run: requires 2 arguments (id, prompt)")
    }
    
    id, ok1 := args[0].(core.Keyword)
    prompt, ok2 := args[1].(core.String)
    
    if !ok1 {
        return nil, fmt.Errorf("agent/run: first argument must be keyword")
    }
    
    if !ok2 {
        return nil, fmt.Errorf("agent/run: second argument must be string")
    }
    
    agent, ok := p.registry.Get(id.V)
    if !ok {
        return nil, fmt.Errorf("agent/run: unknown agent %s", id.V)
    }
    
    // Call LLM
    response, err := p.llm.Complete(ctx,
        agent.Model, agent.System, prompt.V)
    if err != nil {
        return nil, fmt.Errorf("agent/run: %w", err)
    }
    
    return core.String{V: response}, nil
}
```

### agent/run-parallel

```go
func (p *Plugin) runParallel(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("agent/run-parallel: requires 2 arguments (ids, prompt)")
    }
    
    idsList, ok1 := args[0].(core.List)
    prompt, ok2 := args[1].(core.String)
    
    if !ok1 {
        return nil, fmt.Errorf("agent/run-parallel: first argument must be list")
    }
    
    if !ok2 {
        return nil, fmt.Errorf("agent/run-parallel: second argument must be string")
    }
    
    // Extract agent IDs
    var agentIDs []string
    for _, item := range idsList.Items {
        if kw, ok := item.(core.Keyword); ok {
            agentIDs = append(agentIDs, kw.V)
        } else {
            return nil, fmt.Errorf("agent/run-parallel: agent IDs must be keywords")
        }
    }
    
    // Run in parallel with errgroup
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(p.maxParallel)
    
    results := make([]core.Value, len(agentIDs))
    
    for i, id := range agentIDs {
        i, id := i, id // capture
        g.Go(func() error {
            agent, ok := p.registry.Get(id)
            if !ok {
                return fmt.Errorf("unknown agent: %s", id)
            }
            
            response, err := p.llm.Complete(ctx, 
                agent.Model, agent.System, prompt.V)
            if err != nil {
                return err
            }
            
            results[i] = core.String{V: response}
            return nil
        })
    }
    
    if err := g.Wait(); err != nil {
        return nil, fmt.Errorf("agent/run-parallel: %w", err)
    }
    
    return core.Vector{Items: results}, nil
}
```

### agent/run-with-ctx

```go
func (p *Plugin) runWithCtx(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 3 {
        return nil, fmt.Errorf("agent/run-with-ctx: requires 3 arguments (id, prompt, ctx)")
    }
    
    id, ok1 := args[0].(core.Keyword)
    prompt, ok2 := args[1].(core.String)
    ctxMap, ok3 := args[2].(*core.HashMap)
    
    if !ok1 || !ok2 || !ok3 {
        return nil, fmt.Errorf("agent/run-with-ctx: invalid argument types")
    }
    
    agent, ok := p.registry.Get(id.V)
    if !ok {
        return nil, fmt.Errorf("agent/run-with-ctx: unknown agent %s", id.V)
    }
    
    // Build enhanced prompt with context
    enhancedPrompt := buildPromptWithContext(prompt.V, ctxMap)
    
    response, err := p.llm.Complete(ctx,
        agent.Model, agent.System, enhancedPrompt)
    if err != nil {
        return nil, fmt.Errorf("agent/run-with-ctx: %w", err)
    }
    
    return core.String{V: response}, nil
}

func buildPromptWithContext(prompt string, ctx *core.HashMap) string {
    // Format context as string and append to prompt
    var ctxStr string
    for k, v := range ctx.M {
        ctxStr += fmt.Sprintf("%s: %s\n", k.String(), v.String())
    }
    
    return fmt.Sprintf("Context:\n%s\n\nTask:\n%s", ctxStr, prompt)
}
```

### agent/list

```go
func (p *Plugin) list(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 0 {
        return nil, fmt.Errorf("agent/list: takes no arguments")
    }
    
    ids := p.registry.List()
    items := make([]core.Value, len(ids))
    for i, id := range ids {
        items[i] = core.Keyword{V: id}
    }
    
    return core.List{Items: items}, nil
}
```

### agent/info

```go
func (p *Plugin) info(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("agent/info: requires 1 argument (id)")
    }
    
    id, ok := args[0].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("agent/info: argument must be keyword")
    }
    
    agent, ok := p.registry.Get(id.V)
    if !ok {
        return nil, fmt.Errorf("agent/info: unknown agent %s", id.V)
    }
    
    // Build info map
    m := core.NewHashMap()
    m.M[core.Keyword{V: "id"}] = core.Keyword{V: agent.ID}
    m.M[core.Keyword{V: "model"}] = core.String{V: agent.Model}
    m.M[core.Keyword{V: "temperature"}] = core.Float{V: agent.Temperature}
    m.M[core.Keyword{V: "max-tokens"}] = core.Int{V: int64(agent.MaxTokens)}
    m.M[core.Keyword{V: "system"}] = core.String{V: agent.System}
    
    // Tools
    toolsVec := make([]core.Value, len(agent.Tools))
    for i, t := range agent.Tools {
        toolsVec[i] = core.String{V: t}
    }
    m.M[core.Keyword{V: "tools"}] = core.Vector{Items: toolsVec}
    
    // Can-delegate
    delegateVec := make([]core.Value, len(agent.CanDelegate))
    for i, d := range agent.CanDelegate {
        delegateVec[i] = core.Keyword{V: d}
    }
    m.M[core.Keyword{V: "can-delegate"}] = core.Vector{Items: delegateVec}
    
    return m, nil
}
```

### agent/route

```go
func (p *Plugin) route(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("agent/route: requires 1 argument (task)")
    }
    
    // task is passed to routing function
    // In real implementation, would look up and call routing function
    // For now, return a default agent
    
    return core.Keyword{V: "default"}, nil
}
```

### agent/delegate

```go
const maxDelegationDepth = 10

type delegationDepthKey struct{}

func delegationDepth(ctx context.Context) int {
    if d, ok := ctx.Value(delegationDepthKey{}).(int); ok {
        return d
    }
    return 0
}

func withDelegationDepth(ctx context.Context, depth int) context.Context {
    return context.WithValue(ctx, delegationDepthKey{}, depth)
}

func (p *Plugin) delegate(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 3 {
        return nil, fmt.Errorf("agent/delegate: requires 3 arguments (from, to, prompt)")
    }
    
    fromID, ok1 := args[0].(core.Keyword)
    toID, ok2 := args[1].(core.Keyword)
    prompt, ok3 := args[2].(core.String)
    
    if !ok1 || !ok2 || !ok3 {
        return nil, fmt.Errorf("agent/delegate: invalid argument types")
    }
    
    depth := delegationDepth(ctx)
    if depth >= maxDelegationDepth {
        return nil, fmt.Errorf("agent/delegate: max delegation depth %d exceeded", maxDelegationDepth)
    }
    ctx = withDelegationDepth(ctx, depth+1)

    from, ok := p.registry.Get(fromID.V)
    if !ok {
        return nil, fmt.Errorf("agent/delegate: unknown source agent %s", fromID.V)
    }
    
    to, ok := p.registry.Get(toID.V)
    if !ok {
        return nil, fmt.Errorf("agent/delegate: unknown target agent %s", toID.V)
    }
    
    // Check delegation whitelist
    canDelegate := false
    for _, allowed := range from.CanDelegate {
        if allowed == toID.V {
            canDelegate = true
            break
        }
    }
    
    if !canDelegate {
        return nil, fmt.Errorf("agent/delegate: %s cannot delegate to %s", fromID.V, toID.V)
    }
    
    // Execute
    response, err := p.llm.Complete(ctx,
        to.Model, to.System, prompt.V)
    if err != nil {
        return nil, fmt.Errorf("agent/delegate: %w", err)
    }
    
    return core.String{V: response}, nil
}
```

---

## 3. Defagent Macro

```go
// defagent is implemented as a special form that registers agents

func (p *Plugin) registerDefagentHandler(evaluator *core.Evaluator) {
    // Register special form in evaluator
    // This runs during Init
}

// Example expansion:
// (defagent :developer
//   :model "claude-sonnet-4-6"
//   :system "You are a developer..."
//   :can-delegate [:reviewer])
//
// Registers agent with plugin registry
```

---

## 4. File Organization

```
plugins/agent/
├── plugin.go         # Main plugin
├── registry.go       # Agent registry
├── parallel.go       # Concurrent execution helpers
├── bootstrap.lisp    # defagent macro
└── agent_test.go     # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).

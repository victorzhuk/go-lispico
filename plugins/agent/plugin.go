package agent

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	llm         LLMCaller
	registry    *Registry
	maxParallel int
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
		Description: "agent orchestration for go-lispico",
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

	return p.loadBootstrap(env)
}

func (p *Plugin) defagent(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("defagent: requires at least an id keyword")
	}

	id, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("defagent: first argument must be keyword id")
	}

	agent := &Agent{ID: id.V}

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
			agent.Tools = extractStrings(args[i+1])
		case "can-delegate":
			agent.CanDelegate = extractKeywords(args[i+1])
		}
	}

	p.registry.Register(agent)
	return id, nil
}

func extractStrings(v core.Value) []string {
	var result []string
	switch val := v.(type) {
	case core.List:
		for _, item := range val.Items {
			if s, ok := item.(core.String); ok {
				result = append(result, s.V)
			}
		}
	case core.Vector:
		for _, item := range val.Items {
			if s, ok := item.(core.String); ok {
				result = append(result, s.V)
			}
		}
	}
	return result
}

func extractKeywords(v core.Value) []string {
	var result []string
	switch val := v.(type) {
	case core.List:
		for _, item := range val.Items {
			if kw, ok := item.(core.Keyword); ok {
				result = append(result, kw.V)
			}
		}
	case core.Vector:
		for _, item := range val.Items {
			if kw, ok := item.(core.Keyword); ok {
				result = append(result, kw.V)
			}
		}
	}
	return result
}

func (p *Plugin) run(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("agent/run: requires 2 arguments (id, prompt)")
	}

	id, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("agent/run: first argument must be keyword")
	}

	prompt, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("agent/run: second argument must be string")
	}

	agent, ok := p.registry.Get(id.V)
	if !ok {
		return nil, fmt.Errorf("agent/run: unknown agent %s", id.V)
	}

	response, err := p.llm.Complete(ctx, agent.Model, agent.System, prompt.V)
	if err != nil {
		return nil, fmt.Errorf("agent/run: %w", err)
	}

	return core.String{V: response}, nil
}

func (p *Plugin) runParallel(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("agent/run-parallel: requires 2 arguments (ids, prompt)")
	}

	var agentIDs []string
	switch v := args[0].(type) {
	case core.List:
		for _, item := range v.Items {
			if kw, ok := item.(core.Keyword); ok {
				agentIDs = append(agentIDs, kw.V)
			} else {
				return nil, fmt.Errorf("agent/run-parallel: agent IDs must be keywords")
			}
		}
	case core.Vector:
		for _, item := range v.Items {
			if kw, ok := item.(core.Keyword); ok {
				agentIDs = append(agentIDs, kw.V)
			} else {
				return nil, fmt.Errorf("agent/run-parallel: agent IDs must be keywords")
			}
		}
	default:
		return nil, fmt.Errorf("agent/run-parallel: first argument must be list or vector")
	}

	prompt, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("agent/run-parallel: second argument must be string")
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(p.maxParallel)

	results := make([]core.Value, len(agentIDs))

	for i, id := range agentIDs {
		i, id := i, id
		g.Go(func() error {
			agent, ok := p.registry.Get(id)
			if !ok {
				return fmt.Errorf("unknown agent: %s", id)
			}

			response, err := p.llm.Complete(ctx, agent.Model, agent.System, prompt.V)
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

func (p *Plugin) runWithCtx(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("agent/run-with-ctx: requires 3 arguments (id, prompt, ctx)")
	}

	id, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("agent/run-with-ctx: first argument must be keyword")
	}

	prompt, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("agent/run-with-ctx: second argument must be string")
	}

	ctxMap, ok := args[2].(*core.HashMap)
	if !ok {
		return nil, fmt.Errorf("agent/run-with-ctx: third argument must be hashmap")
	}

	agent, ok := p.registry.Get(id.V)
	if !ok {
		return nil, fmt.Errorf("agent/run-with-ctx: unknown agent %s", id.V)
	}

	enhancedPrompt := buildPromptWithContext(prompt.V, ctxMap)

	response, err := p.llm.Complete(ctx, agent.Model, agent.System, enhancedPrompt)
	if err != nil {
		return nil, fmt.Errorf("agent/run-with-ctx: %w", err)
	}

	return core.String{V: response}, nil
}

func buildPromptWithContext(prompt string, ctxMap *core.HashMap) string {
	var ctxStr string
	ctxMap.Each(func(k, v core.Value) {
		ctxStr += fmt.Sprintf("%s: %s\n", k.String(), v.String())
	})

	return fmt.Sprintf("Context:\n%s\n\nTask:\n%s", ctxStr, prompt)
}

func (p *Plugin) list(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

func (p *Plugin) info(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

	m := core.NewHashMap()
	m.Assoc(core.Keyword{V: "id"}, core.Keyword{V: agent.ID})
	m.Assoc(core.Keyword{V: "model"}, core.String{V: agent.Model})
	m.Assoc(core.Keyword{V: "temperature"}, core.Float{V: agent.Temperature})
	m.Assoc(core.Keyword{V: "max-tokens"}, core.Int{V: int64(agent.MaxTokens)})
	m.Assoc(core.Keyword{V: "system"}, core.String{V: agent.System})

	toolsVec := make([]core.Value, len(agent.Tools))
	for i, t := range agent.Tools {
		toolsVec[i] = core.String{V: t}
	}
	m.Assoc(core.Keyword{V: "tools"}, core.Vector{Items: toolsVec})

	delegateVec := make([]core.Value, len(agent.CanDelegate))
	for i, d := range agent.CanDelegate {
		delegateVec[i] = core.Keyword{V: d}
	}
	m.Assoc(core.Keyword{V: "can-delegate"}, core.Vector{Items: delegateVec})

	return m, nil
}

func (p *Plugin) route(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("agent/route: requires 1 argument (task)")
	}

	return core.Keyword{V: "default"}, nil
}

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

func (p *Plugin) delegate(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("agent/delegate: requires 3 arguments (from, to, prompt)")
	}

	fromID, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("agent/delegate: first argument must be keyword")
	}

	toID, ok := args[1].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("agent/delegate: second argument must be keyword")
	}

	prompt, ok := args[2].(core.String)
	if !ok {
		return nil, fmt.Errorf("agent/delegate: third argument must be string")
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

	response, err := p.llm.Complete(ctx, to.Model, to.System, prompt.V)
	if err != nil {
		return nil, fmt.Errorf("agent/delegate: %w", err)
	}

	return core.String{V: response}, nil
}

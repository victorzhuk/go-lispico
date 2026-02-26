package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

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

	return p.loadBootstrap(env)
}

func (p *Plugin) complete(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("llm/complete: requires 3 arguments (model, system, user)")
	}

	model, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("llm/complete: model must be string")
	}
	system, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("llm/complete: system must be string")
	}
	user, ok := args[2].(core.String)
	if !ok {
		return nil, fmt.Errorf("llm/complete: user must be string")
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

func (p *Plugin) completeStar(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("llm/complete*: requires 1 argument (opts map)")
	}

	opts, ok := args[0].(*core.HashMap)
	if !ok {
		return nil, fmt.Errorf("llm/complete*: argument must be a map")
	}

	req := p.parseRequestOptions(opts, env)

	resp, err := p.client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm/complete*: %w", err)
	}

	return core.String{V: resp.Content}, nil
}

func (p *Plugin) stream(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("llm/stream: requires 2 arguments (opts, handler)")
	}

	opts, ok := args[0].(*core.HashMap)
	if !ok {
		return nil, fmt.Errorf("llm/stream: first argument must be a map")
	}

	handler := args[1]
	switch handler.(type) {
	case core.Lambda, core.GoFunc:
	default:
		return nil, fmt.Errorf("llm/stream: second argument must be a function")
	}

	req := p.parseRequestOptions(opts, env)
	req.Stream = true

	chunks, err := p.client.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm/stream: %w", err)
	}

	for chunk := range chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("llm/stream: %w", chunk.Err)
		}

		chunkMap := core.NewHashMap()
		chunkMap, _ = chunkMap.Assoc(core.Keyword{V: "content"}, core.String{V: chunk.Content})
		chunkMap, _ = chunkMap.Assoc(core.Keyword{V: "done"}, core.Bool{V: chunk.Done})

		_, err := eval.Apply(ctx, handler, []core.Value{chunkMap}, env)
		if err != nil {
			return nil, fmt.Errorf("llm/stream handler: %w", err)
		}
	}

	return core.Nil{}, nil
}

func (p *Plugin) chat(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("llm/chat: requires 2 arguments (model, messages)")
	}

	model, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("llm/chat: model must be string")
	}

	var items []core.Value
	switch v := args[1].(type) {
	case core.List:
		items = v.Items
	case core.Vector:
		items = v.Items
	default:
		return nil, fmt.Errorf("llm/chat: messages must be a list or vector")
	}

	var messages []Message
	for _, item := range items {
		msgMap, ok := item.(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("llm/chat: messages must be maps")
		}

		msg := Message{}

		if v, ok := msgMap.Get(core.Keyword{V: "role"}); ok {
			if kw, ok := v.(core.Keyword); ok {
				msg.Role = kw.V
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

func (p *Plugin) embed(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("llm/embed: requires 1-2 arguments (text [model])")
	}

	text, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("llm/embed: text must be string")
	}

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

	items := make([]core.Value, len(embedding))
	for i, f := range embedding {
		items[i] = core.Float{V: f}
	}

	return core.Vector{Items: items}, nil
}

func (p *Plugin) toolCall(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("llm/tool-call: requires 2 arguments (opts, tools)")
	}

	opts, ok := args[0].(*core.HashMap)
	if !ok {
		return nil, fmt.Errorf("llm/tool-call: opts must be a map")
	}

	var toolsItems []core.Value
	switch v := args[1].(type) {
	case core.List:
		toolsItems = v.Items
	case core.Vector:
		toolsItems = v.Items
	default:
		return nil, fmt.Errorf("llm/tool-call: tools must be a list or vector")
	}

	req := p.parseRequestOptions(opts, env)

	for _, item := range toolsItems {
		toolMap, ok := item.(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("llm/tool-call: tools must be maps")
		}

		tool := ToolSpec{}

		if v, ok := toolMap.Get(core.Keyword{V: "name"}); ok {
			if s, ok := v.(core.String); ok {
				tool.Name = s.V
			}
		}

		if v, ok := toolMap.Get(core.Keyword{V: "description"}); ok {
			if s, ok := v.(core.String); ok {
				tool.Description = s.V
			}
		}

		if v, ok := toolMap.Get(core.Keyword{V: "parameters"}); ok {
			params, err := core.ToGoValue(v)
			if err != nil {
				return nil, fmt.Errorf("llm/tool-call: invalid parameters: %w", err)
			}
			tool.Parameters, _ = json.Marshal(params)
		}

		req.Tools = append(req.Tools, tool)
	}

	resp, err := p.client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm/tool-call: %w", err)
	}

	var results []core.Value
	for _, tc := range resp.ToolCalls {
		m := core.NewHashMap()
		m, _ = m.Assoc(core.Keyword{V: "id"}, core.String{V: tc.ID})
		m, _ = m.Assoc(core.Keyword{V: "name"}, core.String{V: tc.Name})

		argsMap := core.NewHashMap()
		for k, v := range tc.Args {
			val, err := core.FromGoValue(v)
			if err != nil {
				val = core.String{V: fmt.Sprintf("%v", v)}
			}
			argsMap, _ = argsMap.Assoc(core.Keyword{V: k}, val)
		}
		m, _ = m.Assoc(core.Keyword{V: "args"}, argsMap)

		results = append(results, m)
	}

	return core.List{Items: results}, nil
}

func (p *Plugin) parseRequestOptions(opts *core.HashMap, env *core.Env) LLMRequest {
	req := LLMRequest{}

	if v, ok := opts.Get(core.Keyword{V: "model"}); ok {
		if s, ok := v.(core.String); ok {
			req.Model = s.V
		}
	} else {
		if v, ok := env.Get("*model*"); ok {
			if s, ok := v.(core.String); ok {
				req.Model = s.V
			}
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
	} else {
		if v, ok := env.Get("*temperature*"); ok {
			if f, ok := v.(core.Float); ok {
				req.Temperature = f.V
			} else if i, ok := v.(core.Int); ok {
				req.Temperature = float64(i.V)
			}
		}
	}

	if v, ok := opts.Get(core.Keyword{V: "messages"}); ok {
		var items []core.Value
		switch vv := v.(type) {
		case core.List:
			items = vv.Items
		case core.Vector:
			items = vv.Items
		}
		for _, item := range items {
			if msgMap, ok := item.(*core.HashMap); ok {
				msg := Message{}
				if rv, ok := msgMap.Get(core.Keyword{V: "role"}); ok {
					if kw, ok := rv.(core.Keyword); ok {
						msg.Role = kw.V
					} else if s, ok := rv.(core.String); ok {
						msg.Role = s.V
					}
				}
				if cv, ok := msgMap.Get(core.Keyword{V: "content"}); ok {
					if s, ok := cv.(core.String); ok {
						msg.Content = s.V
					}
				}
				req.Messages = append(req.Messages, msg)
			}
		}
	}

	return req
}

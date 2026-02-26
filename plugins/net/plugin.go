package net

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	client *http.Client
}

func New() *Plugin {
	return &Plugin{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (p *Plugin) Name() string {
	return "net"
}

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "HTTP client plugin for go-lispico",
		Author:      "go-lispico team",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	env.Set("http/get", core.GoFunc{
		Name: "http/get",
		Fn:   p.get,
	})

	env.Set("http/post", core.GoFunc{
		Name: "http/post",
		Fn:   p.post,
	})

	env.Set("http/fetch", core.GoFunc{
		Name: "http/fetch",
		Fn:   p.fetch,
	})

	return nil
}

func (p *Plugin) get(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("http/get: requires 1-2 arguments (url [opts])")
	}

	urlStr, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("http/get: url must be string")
	}

	var opts *core.HashMap
	if len(args) == 2 {
		opts, ok = args[1].(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("http/get: opts must be map")
		}
	}

	req, err := p.buildRequest("GET", urlStr.V, opts)
	if err != nil {
		return nil, err
	}

	return p.doRequest(ctx, req, opts)
}

func (p *Plugin) post(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("http/post: requires 1-2 arguments (url [opts])")
	}

	urlStr, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("http/post: url must be string")
	}

	var opts *core.HashMap
	if len(args) == 2 {
		opts, ok = args[1].(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("http/post: opts must be map")
		}
	}

	req, err := p.buildRequest("POST", urlStr.V, opts)
	if err != nil {
		return nil, err
	}

	return p.doRequest(ctx, req, opts)
}

func (p *Plugin) fetch(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("http/fetch: requires 1-2 arguments (url [opts])")
	}

	urlStr, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("http/fetch: url must be string")
	}

	var opts *core.HashMap
	if len(args) == 2 {
		opts, ok = args[1].(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("http/fetch: opts must be map")
		}
	}

	method := "GET"
	if opts != nil {
		if m, ok := opts.Get(core.Keyword{V: "method"}); ok {
			switch mv := m.(type) {
			case core.String:
				method = strings.ToUpper(mv.V)
			case core.Keyword:
				method = strings.ToUpper(mv.V)
			}
		}
	}

	req, err := p.buildRequest(method, urlStr.V, opts)
	if err != nil {
		return nil, err
	}

	return p.doRequest(ctx, req, opts)
}

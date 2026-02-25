package stdlib

import (
	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct{}

func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string {
	return ""
}

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "standard library for go-lispico",
		Author:      "go-lispico team",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	p.registerArithmetic(env)
	p.registerStrings(env)
	p.registerCollections(env)
	p.registerHigherOrder(env)
	p.registerControl(env)
	p.registerTypes(env)

	return p.loadBootstrap(env)
}

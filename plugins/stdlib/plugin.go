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
	for _, register := range []func(*core.Env) error{
		p.registerArithmetic,
		p.registerComparison,
		p.registerStrings,
		p.registerCollections,
		p.registerHigherOrder,
		p.registerControl,
		p.registerTypes,
	} {
		if err := register(env); err != nil {
			return err
		}
	}

	return p.loadBootstrap(env)
}

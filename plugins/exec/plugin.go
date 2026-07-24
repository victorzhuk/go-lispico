package exec

import (
	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	defaultTimeout int64
}

func New() *Plugin {
	return &Plugin{defaultTimeout: 30000}
}

func (p *Plugin) Name() string {
	return "exec"
}

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "Process execution and crypto utilities for go-lispico",
		Author:      "go-lispico team",
		Lifecycle:   "frozen",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	for _, fn := range []core.GoFunc{
		{Name: "exec/run", Fn: p.run},
		{Name: "exec/pipe", Fn: p.pipe},
		{Name: "exec/which", Fn: p.which},
		{Name: "crypto/sha256", Fn: p.sha256},
		{Name: "crypto/uuid", Fn: p.uuid},
	} {
		if err := env.Set(fn.Name, fn); err != nil {
			return err
		}
	}
	return nil
}

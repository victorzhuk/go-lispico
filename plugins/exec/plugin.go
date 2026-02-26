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
	}
}

func (p *Plugin) Init(env *core.Env) error {
	env.Set("exec/run", core.GoFunc{Name: "exec/run", Fn: p.run})
	env.Set("exec/pipe", core.GoFunc{Name: "exec/pipe", Fn: p.pipe})
	env.Set("exec/which", core.GoFunc{Name: "exec/which", Fn: p.which})
	env.Set("crypto/sha256", core.GoFunc{Name: "crypto/sha256", Fn: p.sha256})
	env.Set("crypto/uuid", core.GoFunc{Name: "crypto/uuid", Fn: p.uuid})
	return nil
}

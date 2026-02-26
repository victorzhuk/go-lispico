package lio

import (
	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	sandbox *Sandbox
}

func New(cfg Config) (*Plugin, error) {
	sandbox, err := NewSandbox(cfg)
	if err != nil {
		return nil, err
	}
	return &Plugin{sandbox: sandbox}, nil
}

func NewUnsafe() *Plugin {
	return &Plugin{sandbox: &Sandbox{cfg: Config{Mode: ModeNone}}}
}

func (p *Plugin) Name() string {
	return "io"
}

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "IO operations with sandbox security for go-lispico",
		Author:      "go-lispico team",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	env.Set("io/read-file", core.GoFunc{
		Name: "io/read-file",
		Fn:   p.readFile,
	})

	env.Set("io/write-file", core.GoFunc{
		Name: "io/write-file",
		Fn:   p.writeFile,
	})

	env.Set("io/exists?", core.GoFunc{
		Name: "io/exists?",
		Fn:   p.exists,
	})

	env.Set("io/ls", core.GoFunc{
		Name: "io/ls",
		Fn:   p.ls,
	})

	env.Set("io/mkdir", core.GoFunc{
		Name: "io/mkdir",
		Fn:   p.mkdir,
	})

	env.Set("io/stat", core.GoFunc{
		Name: "io/stat",
		Fn:   p.stat,
	})

	env.Set("io/env-get", core.GoFunc{
		Name: "io/env-get",
		Fn:   p.envGet,
	})

	env.Set("io/env-set", core.GoFunc{
		Name: "io/env-set",
		Fn:   p.envSet,
	})

	return nil
}

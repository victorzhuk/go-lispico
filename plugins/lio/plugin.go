package lio

import (
	"sync"

	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	sandbox *Sandbox

	envMu      sync.RWMutex
	envOverlay map[string]string
}

func New(cfg Config) (*Plugin, error) {
	sandbox, err := NewSandbox(cfg)
	if err != nil {
		return nil, err
	}
	return &Plugin{sandbox: sandbox, envOverlay: make(map[string]string)}, nil
}

func NewUnsafe() *Plugin {
	return &Plugin{sandbox: &Sandbox{cfg: Config{Mode: ModeNone}}, envOverlay: make(map[string]string)}
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
	for _, fn := range []core.GoFunc{
		{Name: "io/read-file", Fn: p.readFile},
		{Name: "io/write-file", Fn: p.writeFile},
		{Name: "io/exists?", Fn: p.exists},
		{Name: "io/ls", Fn: p.ls},
		{Name: "io/mkdir", Fn: p.mkdir},
		{Name: "io/stat", Fn: p.stat},
		{Name: "io/env-get", Fn: p.envGet},
		{Name: "io/env-set", Fn: p.envSet},
	} {
		if err := env.Set(fn.Name, fn); err != nil {
			return err
		}
	}
	return nil
}

package runtime

import (
	"fmt"
	"sort"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type PluginStatus struct {
	Name     string
	Version  string
	Status   string
	LoadedAt time.Time
}

func (e *engineImpl) Use(p core.Plugin) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.registry.Register(p); err != nil {
		return fmt.Errorf("register plugin %s: %w", p.Name(), err)
	}

	if err := p.Init(e.rootEnv); err != nil {
		e.registry.Unregister(p.Name())
		return fmt.Errorf("init plugin %s: %w", p.Name(), err)
	}

	e.stats.incPlugins()
	e.logger.Info("plugin loaded", "name", p.Name(), "version", p.Metadata().Version)

	return nil
}

func (e *engineImpl) UnloadPlugin(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := e.registry.Get(name)
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	e.registry.Unregister(name)
	e.stats.decPlugins()
	e.logger.Info("plugin unloaded", "name", name, "version", p.Metadata().Version)

	return nil
}

func (e *engineImpl) ReloadPlugin(p core.Plugin) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := p.Name()
	oldPlugin, hadOld := e.registry.Get(name)

	if hadOld {
		e.registry.Unregister(name)
	}

	if err := e.registry.Register(p); err != nil {
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	if err := p.Init(e.rootEnv); err != nil {
		e.registry.Unregister(name)
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("init plugin %s: %w", name, err)
	}

	if !hadOld {
		e.stats.incPlugins()
	}

	e.logger.Info("plugin reloaded", "name", name, "version", p.Metadata().Version)

	return nil
}

func (e *engineImpl) ListPlugins() []PluginStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	names := e.registry.Namespaces()
	statuses := make([]PluginStatus, 0, len(names))

	for _, name := range names {
		p, ok := e.registry.Get(name)
		if !ok {
			continue
		}

		meta := p.Metadata()
		statuses = append(statuses, PluginStatus{
			Name:    name,
			Version: meta.Version,
			Status:  "active",
		})
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	return statuses
}

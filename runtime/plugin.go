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

// bindings tracks per-plugin names to delete on unload/reload.
// Last writer wins; unload removes what this plugin introduced.
func (e *engineImpl) snapshotBindings() map[string]struct{} {
	return diff(unionOf(e.rootEnv.VarNames(), e.rootEnv.FuncNames()), nil)
}

func (e *engineImpl) Use(p core.Plugin) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.registry.Register(p); err != nil {
		return fmt.Errorf("register plugin %s: %w", p.Name(), err)
	}

	before := e.snapshotBindings()

	if err := p.Init(e.rootEnv); err != nil {
		e.registry.Unregister(p.Name())
		return fmt.Errorf("init plugin %s: %w", p.Name(), err)
	}

	e.applyVocabulary()

	after := e.snapshotBindings()
	added := diff(after, before)
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[p.Name()] = added
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

	for n := range e.bindings[name] {
		e.rootEnv.Delete(n)
	}
	delete(e.bindings, name)

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
		for n := range e.bindings[name] {
			e.rootEnv.Delete(n)
		}
		delete(e.bindings, name)
		e.registry.Unregister(name)
	}

	if err := e.registry.Register(p); err != nil {
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	before := e.snapshotBindings()

	if err := p.Init(e.rootEnv); err != nil {
		e.registry.Unregister(name)
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("init plugin %s: %w", name, err)
	}

	e.applyVocabulary()

	after := e.snapshotBindings()
	added := diff(after, before)
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[name] = added
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

// unionOf merges two string slices into one, deduplicating on the fly.
// Avoids allocations when both inputs are empty.
func unionOf(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// diff returns a set of names present in after but not in before.
// Accepts nil before (returning after as a set) or empty inputs.
func diff(after, before []string) map[string]struct{} {
	result := make(map[string]struct{}, len(after))
	beforeSet := make(map[string]struct{}, len(before))
	for _, s := range before {
		beforeSet[s] = struct{}{}
	}
	for _, s := range after {
		if _, ok := beforeSet[s]; !ok {
			result[s] = struct{}{}
		}
	}
	return result
}

package runtime

import (
	"context"
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
func (e *engineImpl) snapshotBindings() []string {
	return unionOf(e.rootEnv.LocalNames(), e.rootEnv.LocalFuncNames())
}

// populateTemplateBindings merges every name the lazy template layer holds
// for pluginName into e.bindings (so UnloadPlugin deletes template entries
// along with materialized ones) and activates the layer on this engine.
func (e *engineImpl) populateTemplateBindings(pluginName string) {
	if e.lazyMaterializer == nil {
		return
	}
	dialectFP := e.lazyMaterializer.dialectFP
	k := stdlibTemplateKey{dialectFP: dialectFP, pluginName: pluginName}
	layer, ok := stdlibLazyTemplateRegistry.layerFor(k)
	if !ok {
		return
	}
	entries := stdlibLazyTemplateRegistry.snapshotEntries(layer)
	if len(entries) == 0 {
		return
	}
	if e.bindings == nil {
		e.bindings = make(map[string]map[string]struct{})
	}
	owned, ok := e.bindings[pluginName]
	if !ok {
		owned = make(map[string]struct{}, len(entries))
		e.bindings[pluginName] = owned
	}
	for name := range entries {
		owned[name] = struct{}{}
	}
	e.lazyMaterializer.activate(pluginName, owned)
}

func (e *engineImpl) removePluginBindings(name string) {
	if len(e.bindings[name]) == 0 {
		delete(e.bindings, name)
		return
	}
	for n := range e.bindings[name] {
		e.rootEnv.Delete(n)
	}
	delete(e.bindings, name)
	e.rootEnv.BumpMacroEpoch()
}

func (e *engineImpl) Use(p core.Plugin) (err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.registry.Register(p); err != nil {
		return fmt.Errorf("register plugin %s: %w", p.Name(), err)
	}

	before := e.snapshotBindings()
	beforeUsage := retainedUsageOf(e.rootEnv)
	beforeRootCells := snapshotCellSet(e.rootEnv)
	ctx := e.evalResourceContext(context.Background())
	top, startErr := core.StartEval(ctx)
	if startErr != nil {
		e.registry.Unregister(p.Name())
		return startErr
	}
	defer func() {
		if finishErr := core.FinishEval(ctx, top); finishErr != nil && err == nil {
			err = finishErr
		}
	}()

	e.loadingPlugin = p.Name()
	if initErr := p.Init(e.rootEnv); initErr != nil {
		e.loadingPlugin = ""
		e.registry.Unregister(p.Name())
		return fmt.Errorf("init plugin %s: %w", p.Name(), initErr)
	}
	e.loadingPlugin = ""

	if vocabErr := e.applyVocabulary(); vocabErr != nil {
		e.registry.Unregister(p.Name())
		return fmt.Errorf("apply vocabulary for plugin %s: %w", p.Name(), vocabErr)
	}

	after := e.snapshotBindings()
	added := diff(after, before)
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[p.Name()] = added
	}
	e.populateTemplateBindings(p.Name())

	if err := e.settleRetained(ctx, beforeUsage, beforeRootCells, nil, retainedUsage{}, nil); err != nil {
		return err
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

	e.removePluginBindings(name)
	if e.lazyMaterializer != nil {
		e.lazyMaterializer.deactivate(name)
	}

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
		e.removePluginBindings(name)
		e.registry.Unregister(name)
	}

	if err := e.registry.Register(p); err != nil {
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	before := e.snapshotBindings()

	e.loadingPlugin = name
	if err := p.Init(e.rootEnv); err != nil {
		e.loadingPlugin = ""
		e.registry.Unregister(name)
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("init plugin %s: %w", name, err)
	}
	e.loadingPlugin = ""

	if err := e.applyVocabulary(); err != nil {
		e.registry.Unregister(name)
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
		}
		return fmt.Errorf("apply vocabulary for plugin %s: %w", name, err)
	}

	after := e.snapshotBindings()
	added := diff(after, before)
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[name] = added
	}
	e.populateTemplateBindings(name)

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

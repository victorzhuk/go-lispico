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
func (e *engineImpl) populateTemplateBindings(pluginName, pluginVersion string) {
	if e.lazyMaterializer == nil {
		return
	}
	dialectFP := e.lazyMaterializer.dialectFP
	k := stdlibTemplateKey{dialectFP: dialectFP, pluginName: pluginName, pluginVersion: pluginVersion}
	layer, ok := stdlibLazyTemplateRegistry.layerFor(k)
	if !ok {
		return
	}
	entries := layer.publishedEntries()
	if len(entries) == 0 {
		return
	}
	if e.bindings == nil {
		e.bindings = make(map[string]map[string]struct{})
	}
	// owned stays a genuine per-engine map, never the shared published one:
	// applyVocabulary (engine.go) can already have bound names into
	// e.rootEnv through a dialect adapter before this runs, so
	// e.bindings[pluginName] may already exist here and gets mutated below.
	// Aliasing entries' own map onto it would let one engine's adapter
	// bookkeeping corrupt every sibling reading the same published set
	// (runtime/dialect_vocab_test.go exercises this path).
	owned, ok := e.bindings[pluginName]
	if !ok {
		owned = make(map[string]struct{}, len(entries))
		e.bindings[pluginName] = owned
	}
	for name := range entries {
		owned[name] = struct{}{}
	}
	e.lazyMaterializer.activate(pluginName, pluginVersion, owned)
}

// initPlugin runs p.Init, short-circuiting when a completed process-level
// template layer already covers this dialect fingerprint + plugin identity
// (name and version). Scope is fail-closed by the same structural guard
// RegisterValue/RegisterSource already enforce: only a plugin whose Name()
// is "" ever defers registration into the template, so only that plugin's
// key can ever have a layer to attach; every other plugin's Init always runs
// here unconditionally, byte-for-byte as before this function existed. That
// gate also protects concurrency: ensureLayer single-flights per key, and
// single-flighting a non-template plugin's Init across two engines would
// silently skip one engine's own env writes, which is only safe when Init's
// only observable effect is the shared, env-independent template entry.
func (e *engineImpl) initPlugin(p core.Plugin, name, version string) error {
	e.loadingPlugin = name
	if e.lazyMaterializer != nil {
		e.lazyMaterializer.loadingVersion = version
	}
	defer func() {
		e.loadingPlugin = ""
		if e.lazyMaterializer != nil {
			e.lazyMaterializer.loadingVersion = ""
		}
	}()

	if name != "" || e.lazyMaterializer == nil {
		return p.Init(e.rootEnv)
	}
	key := stdlibTemplateKey{dialectFP: e.lazyMaterializer.dialectFP, pluginName: name, pluginVersion: version}
	return stdlibLazyTemplateRegistry.ensureLayer(key, func() error {
		return p.Init(e.rootEnv)
	})
}

func (e *engineImpl) removePluginBindings(name string) {
	if len(e.bindings[name]) == 0 {
		delete(e.bindings, name)
		return
	}
	for n := range e.bindings[name] {
		e.rootEnv.Delete(n)
		e.callCache.drop(n)
	}
	delete(e.bindings, name)
	e.rootEnv.BumpMacroEpoch()
}

type rootEnvSnapshot struct {
	vars  map[string]core.Value
	funcs map[string]core.Value
}

func (e *engineImpl) snapshotRootEnv() rootEnvSnapshot {
	vars := make(map[string]core.Value)
	for _, name := range e.rootEnv.LocalNames() {
		if v, ok := e.rootEnv.Get(name); ok {
			vars[name] = v
		}
	}
	funcs := make(map[string]core.Value)
	for _, name := range e.rootEnv.LocalFuncNames() {
		if v, ok := e.rootEnv.GetFunc(name); ok {
			funcs[name] = v
		}
	}
	return rootEnvSnapshot{vars: vars, funcs: funcs}
}

func (e *engineImpl) restoreRootEnv(s rootEnvSnapshot) {
	for _, name := range e.rootEnv.LocalNames() {
		if _, ok := s.vars[name]; !ok {
			e.rootEnv.Delete(name)
		}
	}
	for _, name := range e.rootEnv.LocalFuncNames() {
		if _, ok := s.funcs[name]; !ok {
			e.rootEnv.Delete(name)
		}
	}
	for name, v := range s.vars {
		_ = e.rootEnv.Set(name, v)
	}
	for name, v := range s.funcs {
		_ = e.rootEnv.SetFunc(name, v)
	}
	e.rootEnv.Rebuild()
	e.rootEnv.BumpMacroEpoch()
}

func (e *engineImpl) Use(p core.Plugin) (err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := p.Name()
	version := p.Metadata().Version
	if err := e.registry.Register(p); err != nil {
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	before := e.snapshotBindings()
	ctx := e.evalResourceContext(context.Background())
	top, startErr := core.StartEval(ctx)
	if startErr != nil {
		e.registry.Unregister(name)
		return startErr
	}
	finished := false
	defer func() {
		if !finished {
			if finishErr := core.FinishEval(ctx, top); finishErr != nil && err == nil {
				err = finishErr
			}
		}
		if err != nil {
			e.rollbackPluginUse(name, before)
		}
	}()

	if initErr := e.initPlugin(p, name, version); initErr != nil {
		return fmt.Errorf("init plugin %s: %w", name, initErr)
	}

	if vocabErr := e.applyVocabulary(); vocabErr != nil {
		return fmt.Errorf("apply vocabulary for plugin %s: %w", name, vocabErr)
	}

	after := e.snapshotBindings()
	added := diff(after, before)
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[name] = added
	}
	e.populateTemplateBindings(name, version)

	finished = true
	if finishErr := core.FinishEval(ctx, top); finishErr != nil {
		return finishErr
	}

	e.stats.incPlugins()
	e.logger.Info("plugin loaded", "name", name, "version", version)

	return nil
}

func (e *engineImpl) rollbackPluginUse(name string, before []string) {
	e.loadingPlugin = ""
	e.registry.Unregister(name)
	after := e.snapshotBindings()
	added := diff(after, before)
	for n := range added {
		e.rootEnv.Delete(n)
	}
	if len(added) > 0 {
		e.rootEnv.Rebuild()
		e.rootEnv.BumpMacroEpoch()
	}
	delete(e.bindings, name)
	if e.lazyMaterializer != nil {
		e.lazyMaterializer.deactivate(name)
	}
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

func (e *engineImpl) ReloadPlugin(p core.Plugin) (err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := p.Name()
	version := p.Metadata().Version
	oldPlugin, hadOld := e.registry.Get(name)
	oldRoot := e.snapshotRootEnv()
	var oldBindings map[string]struct{}
	if hadOld {
		oldBindings = e.bindings[name]
	}

	if hadOld {
		e.removePluginBindings(name)
		e.registry.Unregister(name)
	}

	if err := e.registry.Register(p); err != nil {
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
			if oldBindings != nil {
				e.bindings[name] = oldBindings
			}
			e.restoreRootEnv(oldRoot)
		}
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	before := e.snapshotBindings()

	ctx := e.evalResourceContext(context.Background())
	top, startErr := core.StartEval(ctx)
	if startErr != nil {
		e.rollbackPluginUse(name, before)
		if hadOld {
			e.registry.RegisterNoCheck(oldPlugin)
			if oldBindings != nil {
				e.bindings[name] = oldBindings
			}
			e.restoreRootEnv(oldRoot)
		}
		return startErr
	}

	finished := false
	defer func() {
		if !finished {
			if finishErr := core.FinishEval(ctx, top); finishErr != nil && err == nil {
				err = finishErr
			}
		}
		if err != nil {
			e.rollbackPluginUse(name, before)
			if hadOld {
				e.registry.RegisterNoCheck(oldPlugin)
				if oldBindings != nil {
					e.bindings[name] = oldBindings
				}
				e.restoreRootEnv(oldRoot)
			}
		}
	}()

	if initErr := e.initPlugin(p, name, version); initErr != nil {
		return fmt.Errorf("init plugin %s: %w", name, initErr)
	}

	if err := e.applyVocabulary(); err != nil {
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
	e.populateTemplateBindings(name, version)

	if !hadOld {
		e.stats.incPlugins()
	}

	finished = true
	if finishErr := core.FinishEval(ctx, top); finishErr != nil {
		return finishErr
	}

	e.logger.Info("plugin reloaded", "name", name, "version", version)

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
		status := meta.Lifecycle
		if status == "" {
			status = "active"
		}
		statuses = append(statuses, PluginStatus{
			Name:    name,
			Version: meta.Version,
			Status:  status,
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

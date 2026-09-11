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
func (e *engineImpl) initPlugin(p core.Plugin, env *core.Env, name, version string) error {
	if name != "" || e.lazyMaterializer == nil {
		return p.Init(env)
	}
	key := stdlibTemplateKey{dialectFP: e.lazyMaterializer.dialectFP, pluginName: name, pluginVersion: version}
	return stdlibLazyTemplateRegistry.ensureLayer(key, e.lazyMaterializer.opEager(), func() error {
		return p.Init(env)
	})
}

// beginLazyOp attributes the lazy-state changes made through view to the
// plugin operation it belongs to, so a failed operation can undo them.
func (e *engineImpl) beginLazyOp(view *core.Env, name, version string) {
	if e.lazyMaterializer != nil {
		e.lazyMaterializer.beginOp(view, name, version, stdlibLazyTemplateRegistry.snapshotDisabled())
	}
}

// removePluginBindings deletes the names name owns through env. The caller
// settles e.bindings: unload drops the entry, reload replaces it on success.
func (e *engineImpl) removePluginBindings(env *core.Env, name string) {
	if len(e.bindings[name]) == 0 {
		return
	}
	for n := range e.bindings[name] {
		env.Delete(n)
		e.callCache.drop(n)
	}
	env.BumpMacroEpoch()
}

// abortPlugin rolls back a failed plugin operation. It leaves the registry,
// e.bindings, lazy activation and plugin stats as the operation found them.
func (e *engineImpl) abortPlugin(reg *core.Registration, name string) {
	e.lazyMaterializer.endOp(false)
	reg.Abort()
	for n := range e.bindings[name] {
		e.callCache.drop(n)
	}
}

func (e *engineImpl) Use(p core.Plugin) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := p.Name()
	version := p.Metadata().Version
	gen := e.registry.Generation(name)
	if gen != 0 {
		return fmt.Errorf("register plugin %s: plugin %q already registered", name, name)
	}
	reg, err := e.rootEnv.BeginRegistration()
	if err != nil {
		return fmt.Errorf("register plugin %s: %w", name, err)
	}
	e.beginLazyOp(reg.Env(), name, version)

	added, err := e.loadPlugin(p, reg.Env(), name, version)
	if err == nil {
		err = e.publishPlugin(p, gen)
	}
	if err != nil {
		e.abortPlugin(reg, name)
		return err
	}

	reg.Complete()
	e.lazyMaterializer.endOp(true)
	e.publishBindings(name, version, added)
	e.stats.incPlugins()
	e.logger.Info("plugin loaded", "name", name, "version", version)

	return nil
}

// loadPlugin runs p.Init and the vocabulary pass through env inside one
// accounted evaluation and returns the root names they added. It publishes
// nothing: the caller settles the registration and the engine state.
func (e *engineImpl) loadPlugin(p core.Plugin, env *core.Env, name, version string) (added map[string]struct{}, err error) {
	before := e.snapshotBindings()
	ctx := e.evalResourceContext(context.Background())
	top, err := core.StartEval(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if finishErr := core.FinishEval(ctx, top); finishErr != nil && err == nil {
			added, err = nil, finishErr
		}
	}()
	defer e.lazyMaterializer.fence()

	if initErr := e.initPlugin(p, env, name, version); initErr != nil {
		return nil, fmt.Errorf("init plugin %s: %w", name, initErr)
	}
	if vocabErr := e.applyVocabulary(env); vocabErr != nil {
		return nil, fmt.Errorf("apply vocabulary for plugin %s: %w", name, vocabErr)
	}
	return diff(e.snapshotBindings(), before), nil
}

// publishPlugin stores p in the registry unless the host edited its entry
// since the operation observed generation gen.
func (e *engineImpl) publishPlugin(p core.Plugin, gen uint64) error {
	if err := e.registry.PublishIf(p, gen); err != nil {
		return fmt.Errorf("publish plugin %s: %w", p.Name(), err)
	}
	return nil
}

func (e *engineImpl) publishBindings(name, version string, added map[string]struct{}) {
	if len(added) > 0 {
		if e.bindings == nil {
			e.bindings = make(map[string]map[string]struct{})
		}
		e.bindings[name] = added
	} else {
		delete(e.bindings, name)
	}
	e.populateTemplateBindings(name, version)
}

func (e *engineImpl) UnloadPlugin(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := e.registry.Get(name)
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	e.registry.Unregister(name)

	e.removePluginBindings(e.rootEnv, name)
	delete(e.bindings, name)
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
	version := p.Metadata().Version
	gen := e.registry.Generation(name)
	_, hadOld := e.registry.Get(name)
	reg, err := e.rootEnv.BeginRegistration()
	if err != nil {
		return fmt.Errorf("register plugin %s: %w", name, err)
	}
	view := reg.Env()
	e.beginLazyOp(view, name, version)
	if hadOld {
		e.removePluginBindings(view, name)
	}

	added, err := e.loadPlugin(p, view, name, version)
	if err == nil {
		err = e.publishPlugin(p, gen)
	}
	if err != nil {
		e.abortPlugin(reg, name)
		return err
	}

	reg.Complete()
	e.lazyMaterializer.endOp(true)
	e.publishBindings(name, version, added)
	if !hadOld {
		e.stats.incPlugins()
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

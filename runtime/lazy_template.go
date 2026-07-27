package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
	"golang.org/x/sync/singleflight"
)

type stdlibTemplateKind uint8

const (
	stdlibTemplateGoValue stdlibTemplateKind = iota
	stdlibTemplateBootstrap
)

// stdlibTemplateEntry is one deferred binding: a Go builtin value, or a
// pure-Lisp bootstrap definition (defmacro/defn) executed on first touch.
// Allocation of the binding is deferred from engine construction to first
// resolution; steady-state lookups never consult the template.
type stdlibTemplateEntry struct {
	name      string
	kind      stdlibTemplateKind
	value     core.Value
	canonical bool
	source    string
	reusable  bool
}

type stdlibTemplateKey struct {
	dialectFP     string
	pluginName    string
	pluginVersion string
}

// cacheKey renders key for singleflight.Group.Do, which takes a string key.
func (k stdlibTemplateKey) cacheKey() string {
	return k.dialectFP + "\x00" + k.pluginName + "\x00" + k.pluginVersion
}

type stdlibTemplateLayer struct {
	entries map[string]*stdlibTemplateEntry
	// complete marks a layer whose owning plugin's first Init finished
	// without error. Only ensureLayer's build callback ever sets this to
	// true; UnloadPlugin/ReloadPlugin/rollbackPluginUse never touch it — the
	// layer is process-scoped and outlives any single engine's attachment.
	complete bool
	// published holds the same entries built above, stored once by
	// markComplete in the same r.mu.Lock() section that flips complete to
	// true. putEntry refuses every write once complete is true (its guard),
	// so from that moment entries is never mutated again — Load here needs
	// no lock and every engine's attach path (publishedEntries, consulted by
	// populateTemplateBindings and ForceAll) reads through this pointer
	// directly instead of copying.
	published atomic.Pointer[map[string]*stdlibTemplateEntry]
}

type stdlibTemplateRegistry struct {
	mu sync.RWMutex
	// layers is never pruned: a completed layer is process-scoped by design,
	// since any engine may still attach it. Retention is bounded only by the
	// number of distinct keys, so a template-routed plugin reporting a version
	// that varies per build (a content hash, a timestamp) would strand one
	// layer per version for the process lifetime. Every plugin shipped here
	// reports a static version.
	layers   map[stdlibTemplateKey]*stdlibTemplateLayer
	disabled bool
	// flight single-flights concurrent first builds of one key. Its lock is
	// internal to singleflight and disjoint from mu, so build (which
	// reenters putEntry via RegisterValue/RegisterSource) never deadlocks
	// against a held mu.
	flight singleflight.Group
}

var stdlibLazyTemplateRegistry = &stdlibTemplateRegistry{
	layers: make(map[stdlibTemplateKey]*stdlibTemplateLayer),
}

// stdlibLazyEngineState is the per-engine side of the layer: which plugin
// templates are active here, which names already materialized, and which
// were explicitly deleted (a delete must never resurrect a deferred name,
// matching eager behavior where Delete removes the binding for good).
type stdlibLazyEngineState struct {
	mu sync.Mutex
	// active, installed, and tombstoned are nil until their first write
	// (activate, recordInstall, TombstoneForDelete respectively) — an engine
	// that never loads a template-routed plugin never allocates them. Reads
	// and deletes need no guard: both are legal on a nil map in Go.
	active       map[string]string // pluginName -> pluginVersion, the plugins currently attached on this engine
	activeList   atomic.Value      // []stdlibTemplateKey snapshot of active, rebuilt on activate/deactivate; read on the miss path without allocation
	installed    map[string]struct{}
	tombstoned   map[string]struct{}
	materialized int64
	// nameLocks is guarded by mu and left nil until the first materialization
	// on this engine. A per-engine sync.Map here cost each first touch its
	// own entry-node allocation (profiled: ~6.5% of a startup's total
	// alloc_objects) for a map that rarely outgrows a handful of keys per
	// engine lifetime; a plain map avoids that, but must stay nil-until-used
	// like sync.Map's zero value did — engines that never materialize
	// anything (e.g. no plugin loaded) must not pay for it. mu is held only
	// for the lookup/insert below, never across the per-name critical
	// section itself.
	nameLocks map[string]*sync.Mutex
}

func newStdlibLazyEngineState() *stdlibLazyEngineState {
	s := &stdlibLazyEngineState{}
	// activeKeys does Load().([]stdlibTemplateKey) with no comma-ok, and
	// installLazyLayer runs on every engine, so even a zero-plugin engine
	// reaches that read; a never-stored atomic.Value panics on the nil
	// interface. A nil slice boxed into an interface points at
	// runtime.zerobase, so this store costs no allocation.
	s.activeList.Store([]stdlibTemplateKey(nil))
	return s
}

func (s *stdlibLazyEngineState) getNameMutex(name string) *sync.Mutex {
	s.mu.Lock()
	mu, ok := s.nameLocks[name]
	if !ok {
		mu = &sync.Mutex{}
		if s.nameLocks == nil {
			s.nameLocks = make(map[string]*sync.Mutex)
		}
		s.nameLocks[name] = mu
	}
	s.mu.Unlock()
	return mu
}

func (r *stdlibTemplateRegistry) layerFor(key stdlibTemplateKey) (*stdlibTemplateLayer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled {
		return nil, false
	}
	l, ok := r.layers[key]
	return l, ok
}

// entryFor is the miss-path lookup; it must stay a single-entry read under
// RLock (no layer copy) so undefined-name lookups stay cheap.
func (r *stdlibTemplateRegistry) entryFor(key stdlibTemplateKey, name string) (*stdlibTemplateEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled {
		return nil, false
	}
	l, ok := r.layers[key]
	if !ok {
		return nil, false
	}
	entry, ok := l.entries[name]
	return entry, ok
}

// publishedEntries returns the layer's shared, read-only entry map — the exact
// map markComplete published, not a copy. Callers (populateTemplateBindings,
// ForceAll) must never write into the result: every other engine attaching
// this layer reads the same map concurrently. Nil until the layer is published,
// which is why a build that failed contributes no bindings rather than partial
// ones. Only used on the once-per-Use bookkeeping path, never the miss path.
func (l *stdlibTemplateLayer) publishedEntries() map[string]*stdlibTemplateEntry {
	if m := l.published.Load(); m != nil {
		return *m
	}
	return nil
}

// putEntry refuses to write once the layer is published: complete flips
// under this same lock and is never undone, so a write reaching here after
// that would mutate the exact map publishedEntries hands to every attached
// engine. That path is not expected to be reachable today (task 2.1), but
// the guard is what keeps it that way instead of merely documenting it.
func (r *stdlibTemplateRegistry) putEntry(key stdlibTemplateKey, entry *stdlibTemplateEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return nil
	}
	layer, ok := r.layers[key]
	if !ok {
		layer = &stdlibTemplateLayer{entries: make(map[string]*stdlibTemplateEntry)}
		r.layers[key] = layer
	}
	if layer.complete {
		return fmt.Errorf("stdlib template layer %s/%s/%s already published: refusing write to %q",
			key.dialectFP, key.pluginName, key.pluginVersion, entry.name)
	}
	layer.entries[entry.name] = entry
	return nil
}

// layerState reports whether key's layer is already complete, and whether
// the registry is disabled (the test-only eager fallback). disabled always
// wins so the caller bypasses single-flight and runs build directly, one
// call per engine, exactly as an unshared plugin would.
func (r *stdlibTemplateRegistry) layerState(key stdlibTemplateKey) (complete, disabled bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled {
		return false, true
	}
	l, ok := r.layers[key]
	return ok && l.complete, false
}

// markComplete flips the layer's complete flag after its first successful
// build and publishes its entries for the lock-free attach path. A key with
// no layer (its plugin never routed a registration through putEntry — a
// direct-env plugin) is left alone: there is nothing to mark, so layerState
// keeps reporting incomplete and that plugin's Init always reruns, exactly
// as before this registry existed.
func (r *stdlibTemplateRegistry) markComplete(key stdlibTemplateKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.layers[key]; ok {
		l.complete = true
		entries := l.entries
		l.published.Store(&entries)
	}
}

// ensureLayer builds key's layer at most once per process: concurrent first
// calls single-flight onto one build, and any call once the layer is
// complete returns immediately without calling build. build must never run
// with r.mu held — it reenters putEntry (via RegisterValue/RegisterSource),
// which takes r.mu itself; flight's own lock is disjoint from r.mu, so this
// can never deadlock against a held registry lock.
func (r *stdlibTemplateRegistry) ensureLayer(key stdlibTemplateKey, build func() error) error {
	if complete, disabled := r.layerState(key); disabled {
		return build()
	} else if complete {
		return nil
	}
	_, err, _ := r.flight.Do(key.cacheKey(), func() (any, error) {
		if complete, disabled := r.layerState(key); disabled {
			return nil, build()
		} else if complete {
			return nil, nil
		}
		if buildErr := build(); buildErr != nil {
			return nil, buildErr
		}
		r.markComplete(key)
		return nil, nil
	})
	return err
}

// stdlibLazyMaterializer is the core.LazyLayer installed on an engine's root
// env. It is consulted only on a real binding miss; materialization runs
// through env.Set/SetCanonical/SetFunc/SetFuncCanonical, each of which takes
// the env write lock briefly — the layer never holds env.mu across execution
// and never touches engine.mu, so recursive materialization (a materialized
// body resolving further deferred names) cannot deadlock either lock.
type stdlibLazyMaterializer struct {
	engine    *engineImpl
	state     *stdlibLazyEngineState
	dialectFP string
	// loadingVersion mirrors engine.loadingPlugin: the Metadata().Version of
	// the plugin whose Init is running inside Use/ReloadPlugin, set only for
	// that call's duration. RegisterValue/RegisterSource read it to build the
	// template key: name+version identifies the layer (task 2.3).
	loadingVersion string
}

func newStdlibLazyMaterializer(engine *engineImpl) *stdlibLazyMaterializer {
	// The bytecode evaluator already computed the dialect fingerprint for
	// its chunk cache; recompute only for tree-walker engines.
	fp := ""
	if be, ok := engine.evaluator.(*bytecodeEvaluator); ok {
		fp = be.dialectFP
	} else {
		fp = engine.config.dialect.Fingerprint()
	}
	return &stdlibLazyMaterializer{
		engine:    engine,
		state:     newStdlibLazyEngineState(),
		dialectFP: fp,
	}
}

// activeKeys reads the atomic snapshot so the miss path never allocates or
// takes state.mu; each key already carries the plugin's version.
func (m *stdlibLazyMaterializer) activeKeys() []stdlibTemplateKey {
	return m.state.activeList.Load().([]stdlibTemplateKey)
}

// LookupAndMaterialize resolves name from the active plugin templates,
// installing the binding into env on first touch. The boolean results are
// (found, canonical). A name the user explicitly deleted stays deleted:
// the tombstone check runs before any template consultation.
func (m *stdlibLazyMaterializer) LookupAndMaterialize(env *core.Env, name string) (core.Value, bool, bool) {
	if m == nil || m.engine == nil {
		return nil, false, false
	}
	m.state.mu.Lock()
	if _, dead := m.state.tombstoned[name]; dead {
		m.state.mu.Unlock()
		return nil, false, false
	}
	if _, live := m.state.installed[name]; live {
		m.state.mu.Unlock()
		v, ok, canon := env.GetCanonical(name)
		if ok {
			return v, true, canon
		}
		return nil, false, false
	}
	m.state.mu.Unlock()

	for _, key := range m.activeKeys() {
		if entry, ok := stdlibLazyTemplateRegistry.entryFor(key, name); ok {
			return m.materializeOne(env, key.pluginName, entry)
		}
	}
	return nil, false, false
}

func (m *stdlibLazyMaterializer) materializeOne(env *core.Env, pluginName string, entry *stdlibTemplateEntry) (core.Value, bool, bool) {
	// Per-name mutex: concurrent first-touch of one name serializes (the
	// loser observes the installed binding), disjoint names proceed in
	// parallel, and no global lock is held across env writes or execution.
	nameMu := m.state.getNameMutex(entry.name)
	nameMu.Lock()
	defer nameMu.Unlock()

	m.state.mu.Lock()
	_, live := m.state.installed[entry.name]
	m.state.mu.Unlock()
	if live {
		v, ok, canon := env.GetCanonical(entry.name)
		if ok {
			return v, true, canon
		}
		return nil, false, false
	}

	switch entry.kind {
	case stdlibTemplateGoValue:
		if err := m.installValue(env, pluginName, entry); err != nil {
			m.logMaterializeFailure(entry, err)
			return nil, false, false
		}
		return entry.value, true, entry.canonical
	case stdlibTemplateBootstrap:
		return m.materializeBootstrap(env, pluginName, entry)
	default:
		return nil, false, false
	}
}

// installValue binds a deferred Go builtin; caller holds the name's mutex.
// A cell the user already shadowed stays untouched — eager load binds
// everything before user code runs, so a user def/defun always wins;
// materialization must reproduce that order by filling only cells that are
// still empty.
func (m *stdlibLazyMaterializer) installValue(env *core.Env, pluginName string, entry *stdlibTemplateEntry) error {
	valueMissing := !env.HasLive(entry.name)
	funcMissing := m.engine.config.dialect.IsLisp2() && !env.HasLiveFunc(entry.name)

	if valueMissing && funcMissing {
		if entry.canonical {
			if err := env.SetBothCanonical(entry.name, entry.value); err != nil {
				return err
			}
		} else {
			if err := env.SetBoth(entry.name, entry.value); err != nil {
				return err
			}
		}
		m.recordInstall(pluginName, entry.name)
		return nil
	}

	if valueMissing {
		if entry.canonical {
			if err := env.SetCanonical(entry.name, entry.value); err != nil {
				return err
			}
		} else {
			if err := env.Set(entry.name, entry.value); err != nil {
				return err
			}
		}
	}

	if m.engine.config.dialect.IsLisp2() && funcMissing {
		if entry.canonical {
			if err := env.SetFuncCanonical(entry.name, entry.value); err != nil {
				return err
			}
		} else {
			if err := env.SetFunc(entry.name, entry.value); err != nil {
				return err
			}
		}
	}

	m.recordInstall(pluginName, entry.name)
	return nil
}

func (m *stdlibLazyMaterializer) recordInstall(pluginName, name string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.state.installed[name]; ok {
		return
	}
	if m.state.installed == nil {
		m.state.installed = make(map[string]struct{})
	}
	m.state.installed[name] = struct{}{}
	m.state.materialized++
}

// materializeBootstrap executes a deferred defmacro/defn form exactly as the
// eager bootstrap loader does: the reusable entry goes through the shared
// compiled-artifact cache, everything else through the full-kernel evaluator
// so dialects without defmacro (EmptyDialect) still get their macros. The
// defining form's body is not evaluated at definition time, so execution
// never re-enters materialization for the same name.
func (m *stdlibLazyMaterializer) materializeBootstrap(env *core.Env, pluginName string, entry *stdlibTemplateEntry) (core.Value, bool, bool) {
	ctx := context.Background()
	if entry.reusable {
		if be, ok := env.Evaluator().(*bytecodeEvaluator); ok {
			if _, err := be.EvalStdlibBootstrap(ctx, entry.source, env); err != nil {
				m.logMaterializeFailure(entry, err)
				return nil, false, false
			}
			return m.publishBootstrap(env, pluginName, entry.name)
		}
	}
	forms, err := core.Read(entry.source)
	if err != nil {
		m.logMaterializeFailure(entry, err)
		return nil, false, false
	}
	evaluator := core.NewEvaluator()
	for _, form := range forms {
		if _, err := evaluator.Eval(ctx, form, env); err != nil {
			m.logMaterializeFailure(entry, err)
			return nil, false, false
		}
	}
	return m.publishBootstrap(env, pluginName, entry.name)
}

// logMaterializeFailure surfaces a deferred-definition failure the miss path
// cannot propagate (lookup APIs return values, not errors). The name stays
// uninstalled, so the next touch retries; eager load fails Use() instead.
func (m *stdlibLazyMaterializer) logMaterializeFailure(entry *stdlibTemplateEntry, err error) {
	m.engine.logger.Warn("lazy stdlib materialization failed", "name", entry.name, "error", err)
}

// publishBootstrap mirrors the freshly defined name into the function cell
// when it is not already there, replicating the eager mirrorBootstrapBindings
// pass (head-position resolution under Lisp-2; harmless under Lisp-1).
// Bootstrap entries are defmacro/defn — never canonical operators.
func (m *stdlibLazyMaterializer) publishBootstrap(env *core.Env, pluginName, name string) (core.Value, bool, bool) {
	v, ok := env.Get(name)
	if !ok {
		return nil, false, false
	}
	if !env.HasLiveFunc(name) {
		if err := env.SetFunc(name, v); err != nil {
			m.logMaterializeFailure(&stdlibTemplateEntry{name: name}, err)
			return nil, false, false
		}
	}
	m.recordInstall(pluginName, name)
	return v, true, false
}

// TombstoneForDelete records an explicit env.Delete so a later miss does not
// resurrect the name from the template. Tombstones persist until the plugin
// is re-Used (activation clears them).
func (m *stdlibLazyMaterializer) TombstoneForDelete(env *core.Env, name string) {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	if m.state.tombstoned == nil {
		m.state.tombstoned = make(map[string]struct{})
	}
	m.state.tombstoned[name] = struct{}{}
	delete(m.state.installed, name)
	m.state.mu.Unlock()
}

// RegisterValue defers a Go builtin binding into the loading plugin's
// template layer. Only the stdlib plugin (name "") defers: its values are
// audited stateless (no registration-time capture of engine state), which a
// process-shared template requires; every other plugin binds immediately,
// exactly as before. With the layer disabled (tests) it likewise falls back
// to an immediate value-cell bind; applyVocabulary's bridge then mirrors
// the function cell exactly as on an engine without the layer.
func (m *stdlibLazyMaterializer) RegisterValue(env *core.Env, name string, val core.Value, canonical bool) error {
	if m == nil {
		return nil
	}
	stdlibLazyTemplateRegistry.mu.RLock()
	disabled := stdlibLazyTemplateRegistry.disabled
	stdlibLazyTemplateRegistry.mu.RUnlock()
	if disabled || m.engine.loadingPlugin != "" {
		if canonical {
			return env.SetCanonical(name, val)
		}
		return env.Set(name, val)
	}

	dialect := m.engine.config.dialect
	vocab := dialect.Vocab()
	key := stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: m.engine.loadingPlugin, pluginVersion: m.loadingVersion}

	// Vocabulary renames bind the visible name to the canonical GoFunc. The
	// alias is a plain (non-canonical) binding, matching the eager Set in
	// applyVocabulary; it is registered even when the canonical name itself
	// is stripped by an EmptyDialect allowlist (the eager apply phase
	// resolves renames from the pre-strip snapshot).
	for visible, ve := range vocab {
		if ve.Adapter == nil && ve.Canonical == name {
			if err := stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
				name:  visible,
				kind:  stdlibTemplateGoValue,
				value: val,
			}); err != nil {
				return err
			}
		}
	}

	if vocab != nil && dialect.IsBaseEmpty() {
		if _, allowed := vocab[name]; !allowed {
			return nil
		}
	}

	return stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name:      name,
		kind:      stdlibTemplateGoValue,
		value:     val,
		canonical: canonical,
	})
}

// RegisterSource defers a pure-Lisp bootstrap definition (defmacro/defn).
// Same stdlib-only restriction as RegisterValue; it reports false for other
// plugins and when the layer is disabled so the caller evaluates eagerly.
func (m *stdlibLazyMaterializer) RegisterSource(env *core.Env, name, source string, reusable bool) bool {
	if m == nil {
		return false
	}
	stdlibLazyTemplateRegistry.mu.RLock()
	disabled := stdlibLazyTemplateRegistry.disabled
	stdlibLazyTemplateRegistry.mu.RUnlock()
	if disabled || m.engine.loadingPlugin != "" {
		return false
	}
	key := stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: m.engine.loadingPlugin, pluginVersion: m.loadingVersion}
	if err := stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name:     name,
		kind:     stdlibTemplateBootstrap,
		source:   source,
		reusable: reusable,
	}); err != nil {
		m.engine.logger.Warn("stdlib template layer already published, falling back to eager bootstrap", "name", name, "error", err)
		return false
	}
	return true
}

// rebuildActiveList refreshes the atomic snapshot; caller holds state.mu.
func (s *stdlibLazyEngineState) rebuildActiveList(dialectFP string) {
	keys := make([]stdlibTemplateKey, 0, len(s.active))
	for name, version := range s.active {
		keys = append(keys, stdlibTemplateKey{dialectFP: dialectFP, pluginName: name, pluginVersion: version})
	}
	s.activeList.Store(keys)
}

// activate revives the plugin's names from tombstones left by an earlier
// UnloadPlugin: a re-Used plugin must load as if fresh.
func (m *stdlibLazyMaterializer) activate(pluginName, pluginVersion string, names map[string]struct{}) {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.active == nil {
		m.state.active = make(map[string]string)
	}
	m.state.active[pluginName] = pluginVersion
	m.state.rebuildActiveList(m.dialectFP)
	for name := range names {
		delete(m.state.tombstoned, name)
	}
}

// deactivate only stops consulting the layer on THIS engine; the
// process-level layer stays because sibling engines may still use it.
func (m *stdlibLazyMaterializer) deactivate(pluginName string) {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	delete(m.state.active, pluginName)
	m.state.rebuildActiveList(m.dialectFP)
}

// MaterializeCount reports how many bindings this engine materialized;
// tests use it to prove at-most-once first-touch.
func (m *stdlibLazyMaterializer) MaterializeCount() int {
	if m == nil {
		return 0
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return int(m.state.materialized)
}

// ForceAll honors tombstones (an explicitly deleted name must not
// resurrect) and skips installed names, so enumeration is safe to call
// repeatedly.
func (m *stdlibLazyMaterializer) ForceAll(env *core.Env) {
	if m == nil || m.engine == nil {
		return
	}
	m.state.mu.Lock()
	keys := make([]stdlibTemplateKey, 0, len(m.state.active))
	for name, version := range m.state.active {
		keys = append(keys, stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: name, pluginVersion: version})
	}
	m.state.mu.Unlock()
	for _, key := range keys {
		layer, ok := stdlibLazyTemplateRegistry.layerFor(key)
		if !ok {
			continue
		}
		for name, entry := range layer.publishedEntries() {
			m.state.mu.Lock()
			_, dead := m.state.tombstoned[name]
			_, live := m.state.installed[name]
			m.state.mu.Unlock()
			if dead || live {
				continue
			}
			m.materializeOne(env, key.pluginName, entry)
		}
	}
}

func installLazyLayer(engine *engineImpl) *stdlibLazyMaterializer {
	m := newStdlibLazyMaterializer(engine)
	engine.rootEnv.SetLazyLayer(m)
	engine.lazyMaterializer = m
	return m
}

// SetStdlibLazyDisabledForTesting forces eager stdlib registration (the lazy
// layer stops accepting and consulting templates) and returns a restore
// func. Process-global: tests using it must not run in parallel with tests
// that depend on lazy materialization.
func SetStdlibLazyDisabledForTesting(disabled bool) func() {
	stdlibLazyTemplateRegistry.mu.Lock()
	prev := stdlibLazyTemplateRegistry.disabled
	stdlibLazyTemplateRegistry.disabled = disabled
	stdlibLazyTemplateRegistry.mu.Unlock()
	return func() {
		stdlibLazyTemplateRegistry.mu.Lock()
		stdlibLazyTemplateRegistry.disabled = prev
		stdlibLazyTemplateRegistry.mu.Unlock()
	}
}

func setStdlibLazyDisabledForTesting(disabled bool) func() {
	return SetStdlibLazyDisabledForTesting(disabled)
}

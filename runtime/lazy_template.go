package runtime

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
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
	dialectFP  string
	pluginName string
}

type stdlibTemplateLayer struct {
	entries map[string]*stdlibTemplateEntry
}

type stdlibTemplateRegistry struct {
	mu       sync.RWMutex
	layers   map[stdlibTemplateKey]*stdlibTemplateLayer
	disabled bool
}

var stdlibLazyTemplateRegistry = &stdlibTemplateRegistry{
	layers: make(map[stdlibTemplateKey]*stdlibTemplateLayer),
}

// stdlibLazyEngineState is the per-engine side of the layer: which plugin
// templates are active here, which names already materialized, and which
// were explicitly deleted (a delete must never resurrect a deferred name,
// matching eager behavior where Delete removes the binding for good).
type stdlibLazyEngineState struct {
	mu           sync.Mutex
	active       map[string]struct{}
	activeList   atomic.Value // []string snapshot of active, rebuilt on activate/deactivate; read on the miss path without allocation
	installed    map[string]struct{}
	tombstoned   map[string]struct{}
	materialized int64
	nameLocks    sync.Map
}

func newStdlibLazyEngineState() *stdlibLazyEngineState {
	s := &stdlibLazyEngineState{
		active:     make(map[string]struct{}),
		installed:  make(map[string]struct{}),
		tombstoned: make(map[string]struct{}),
	}
	s.activeList.Store([]string(nil))
	return s
}

func (s *stdlibLazyEngineState) getNameMutex(name string) *sync.Mutex {
	if v, ok := s.nameLocks.Load(name); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := s.nameLocks.LoadOrStore(name, mu)
	return actual.(*sync.Mutex)
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

// snapshotEntries copies the layer so iteration never holds the registry
// lock; only used on the once-per-Use bookkeeping path, never the miss path.
func (r *stdlibTemplateRegistry) snapshotEntries(layer *stdlibTemplateLayer) map[string]*stdlibTemplateEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*stdlibTemplateEntry, len(layer.entries))
	for k, v := range layer.entries {
		out[k] = v
	}
	return out
}

func (r *stdlibTemplateRegistry) putEntry(key stdlibTemplateKey, entry *stdlibTemplateEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	layer, ok := r.layers[key]
	if !ok {
		layer = &stdlibTemplateLayer{entries: make(map[string]*stdlibTemplateEntry)}
		r.layers[key] = layer
	}
	layer.entries[entry.name] = entry
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

// activePlugins reads the atomic snapshot so the miss path never
// allocates or takes state.mu.
func (m *stdlibLazyMaterializer) activePlugins() []string {
	return m.state.activeList.Load().([]string)
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
		v, canon, ok := env.GetCanonical(name)
		if ok {
			return v, true, canon
		}
		return nil, false, false
	}
	m.state.mu.Unlock()

	for _, ns := range m.activePlugins() {
		if entry, ok := stdlibLazyTemplateRegistry.entryFor(stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: ns}, name); ok {
			return m.materializeOne(env, ns, entry)
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
		v, canon, ok := env.GetCanonical(entry.name)
		if ok {
			return v, true, canon
		}
		return nil, false, false
	}

	switch entry.kind {
	case stdlibTemplateGoValue:
		m.installValue(env, pluginName, entry)
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
func (m *stdlibLazyMaterializer) installValue(env *core.Env, pluginName string, entry *stdlibTemplateEntry) {
	if !env.HasLive(entry.name) {
		if entry.canonical {
			env.SetCanonical(entry.name, entry.value)
		} else {
			env.Set(entry.name, entry.value)
		}
	}
	if m.engine.config.dialect.IsLisp2() && !env.HasLiveFunc(entry.name) {
		if entry.canonical {
			env.SetFuncCanonical(entry.name, entry.value)
		} else {
			env.SetFunc(entry.name, entry.value)
		}
	}
	m.recordInstall(pluginName, entry.name)
}

func (m *stdlibLazyMaterializer) recordInstall(pluginName, name string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.state.installed[name]; ok {
		return
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
		env.SetFunc(name, v)
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
func (m *stdlibLazyMaterializer) RegisterValue(env *core.Env, name string, val core.Value, canonical bool) {
	if m == nil {
		return
	}
	stdlibLazyTemplateRegistry.mu.RLock()
	disabled := stdlibLazyTemplateRegistry.disabled
	stdlibLazyTemplateRegistry.mu.RUnlock()
	if disabled || m.engine.loadingPlugin != "" {
		if canonical {
			env.SetCanonical(name, val)
		} else {
			env.Set(name, val)
		}
		return
	}

	dialect := m.engine.config.dialect
	vocab := dialect.Vocab()
	key := stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: m.engine.loadingPlugin}

	// Vocabulary renames bind the visible name to the canonical GoFunc. The
	// alias is a plain (non-canonical) binding, matching the eager Set in
	// applyVocabulary; it is registered even when the canonical name itself
	// is stripped by an EmptyDialect allowlist (the eager apply phase
	// resolves renames from the pre-strip snapshot).
	for visible, ve := range vocab {
		if ve.Adapter == nil && ve.Canonical == name {
			stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
				name:  visible,
				kind:  stdlibTemplateGoValue,
				value: val,
			})
		}
	}

	if vocab != nil && dialect.IsBaseEmpty() {
		if _, allowed := vocab[name]; !allowed {
			return
		}
	}

	stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
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
	key := stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: m.engine.loadingPlugin}
	stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name:     name,
		kind:     stdlibTemplateBootstrap,
		source:   source,
		reusable: reusable,
	})
	return true
}

// rebuildActiveList refreshes the atomic snapshot; caller holds state.mu.
func (s *stdlibLazyEngineState) rebuildActiveList() {
	active := make([]string, 0, len(s.active))
	for ns := range s.active {
		active = append(active, ns)
	}
	s.activeList.Store(active)
}

// activate revives the plugin's names from tombstones left by an earlier
// UnloadPlugin: a re-Used plugin must load as if fresh.
func (m *stdlibLazyMaterializer) activate(pluginName string, names map[string]struct{}) {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.active[pluginName] = struct{}{}
	m.state.rebuildActiveList()
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
	m.state.rebuildActiveList()
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
	active := make([]string, 0, len(m.state.active))
	for ns := range m.state.active {
		active = append(active, ns)
	}
	m.state.mu.Unlock()
	dialectFP := m.dialectFP
	for _, ns := range active {
		layer, ok := stdlibLazyTemplateRegistry.layerFor(stdlibTemplateKey{dialectFP: dialectFP, pluginName: ns})
		if !ok {
			continue
		}
		for name, entry := range stdlibLazyTemplateRegistry.snapshotEntries(layer) {
			m.state.mu.Lock()
			_, dead := m.state.tombstoned[name]
			_, live := m.state.installed[name]
			m.state.mu.Unlock()
			if dead || live {
				continue
			}
			m.materializeOne(env, ns, entry)
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

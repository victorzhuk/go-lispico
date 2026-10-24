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
	// fn mirrors the value into the function cell on install (a GoFunc
	// under Lisp-2); alias marks a vocabulary rename, which owns its
	// visible name over any own registration of that name.
	fn     bool
	alias  bool
	source string
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
	// true; UnloadPlugin/ReloadPlugin never touch it — the
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
	// (activate, recordInstall, BeginDelete respectively) — an engine
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
	// op is the plugin operation running on this engine, nil outside one.
	op *lazyOp
}

// lazyOp attributes the lazy-state changes made through one plugin
// operation's registration view, so a failed operation undoes exactly those
// and keeps every change made through the root meanwhile. Guarded by the
// owning state's mu, except inflight.
type lazyOp struct {
	view *core.Env
	// closed stops the view from admitting new materializations; inflight
	// counts the admitted ones still running. Add happens only under the
	// state's mu while !closed, so it never races fence's Wait.
	closed   bool
	inflight sync.WaitGroup
	name     string
	version  string
	eager    bool
	// entries holds the before-image of each name the operation touched.
	entries  map[string]*lazyOpEntry
	installs int64
}

// lazyOpEntry is one name's tombstoned/installed membership before the
// operation first touched it. A foreign entry saw an unattributed change
// since, which undo keeps.
type lazyOpEntry struct {
	tomb      bool
	installed bool
	foreign   bool
}

// own records name's membership as the before-image on the operation's first
// touch, and again after a foreign change so undo restores what the host
// left. Caller holds s.mu.
func (op *lazyOp) own(s *stdlibLazyEngineState, name string) {
	ent, ok := op.entries[name]
	if ok && !ent.foreign {
		return
	}
	if !ok {
		ent = &lazyOpEntry{}
		if op.entries == nil {
			op.entries = make(map[string]*lazyOpEntry)
		}
		op.entries[name] = ent
	}
	_, ent.tomb = s.tombstoned[name]
	_, ent.installed = s.installed[name]
	ent.foreign = false
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

// layerFor resolves key's layer. It ignores the process-global disabled
// flag: engines latched to lazy at plugin-load time must still see the
// layer they build or attach, and eager-latched engines never route
// registrations here, so the flag only belongs in the latch read.
func (r *stdlibTemplateRegistry) layerFor(key stdlibTemplateKey) (*stdlibTemplateLayer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.layers[key]
	return l, ok
}

// entryFor is the miss-path lookup; it must stay a single-entry read under
// RLock (no layer copy) so undefined-name lookups stay cheap. Like layerFor
// it ignores the disabled flag; the open operation's eager latch gates the
// miss path instead.
func (r *stdlibTemplateRegistry) entryFor(key stdlibTemplateKey, name string) (*stdlibTemplateEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
// engine. Only lazy-latched builds reach here at all (RegisterValue/
// RegisterSource consult the operation's eager latch), so the write is
// unconditional below the publish guard: a concurrent flip of the global
// disabled flag can no longer silently drop half of one build's entries,
// which is how a partial layer used to get published.
func (r *stdlibTemplateRegistry) putEntry(key stdlibTemplateKey, entry *stdlibTemplateEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	layer, ok := r.layers[key]
	if !ok {
		layer = &stdlibTemplateLayer{entries: make(map[string]*stdlibTemplateEntry)}
		r.layers[key] = layer
	}
	if layer.complete {
		return fmt.Errorf("stdlib template layer %s/%s/%s already published: refusing write to %q",
			key.dialectFP, key.pluginName, key.pluginVersion, entry.name)
	}
	if prev, ok := layer.entries[entry.name]; ok && prev.alias && !entry.alias {
		return nil
	}
	layer.entries[entry.name] = entry
	return nil
}

// layerState reports whether key's layer is already complete.
func (r *stdlibTemplateRegistry) layerState(key stdlibTemplateKey) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.layers[key]
	return ok && l.complete
}

// snapshotDisabled reads the process-global disable flag once. beginOp
// latches its value onto the engine's plugin operation for the whole load,
// so a test toggling the flag mid-build cannot split one build's
// registrations between eager env binds and deferred template entries.
func (r *stdlibTemplateRegistry) snapshotDisabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.disabled
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
// lazy calls single-flight onto one build, and any call once the layer is
// complete returns immediately without calling build. eager is the
// operation's latch, read before the build starts: an eager
// build never touches the registry, so it runs directly, one call per
// engine, exactly as an unshared plugin would — and never in a window that
// races the flight builder for the same key. build must never run with
// r.mu held — it reenters putEntry (via RegisterValue/RegisterSource),
// which takes r.mu itself; flight's own lock is disjoint from r.mu, so this
// can never deadlock against a held registry lock.
func (r *stdlibTemplateRegistry) ensureLayer(key stdlibTemplateKey, eager bool, build func() error) error {
	if eager {
		return build()
	}
	if r.layerState(key) {
		return nil
	}
	_, err, _ := r.flight.Do(key.cacheKey(), func() (any, error) {
		if r.layerState(key) {
			return nil, nil
		}
		if buildErr := build(); buildErr != nil {
			// The build may have registered entries before failing; drop
			// the incomplete layer so a retry starts clean instead of
			// publishing stale names alongside fresh ones. A concurrently
			// completed layer is left alone, and single-flight keeps any
			// other lazy build for this key from racing the delete.
			r.discardLayer(key)
			return nil, buildErr
		}
		r.markComplete(key)
		return nil, nil
	})
	return err
}

// discardLayer drops key's layer after a failed build so a retry rebuilds
// from scratch rather than onto names the failed build registered. Safe
// under r.mu: an incomplete layer was never published, so no attached
// engine holds its entries.
func (r *stdlibTemplateRegistry) discardLayer(key stdlibTemplateKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.layers[key]; ok && !l.complete {
		delete(r.layers, key)
	}
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

// beginOp opens the plugin operation writing through view. eager latches the
// registry's disable flag for the whole load: every routing decision inside
// the build reads it instead of the live flag, so a parallel test toggling
// SetStdlibLazyDisabledForTesting mid-build cannot split the build.
func (m *stdlibLazyMaterializer) beginOp(view *core.Env, name, version string, eager bool) {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	m.state.op = &lazyOp{view: view, name: name, version: version, eager: eager}
	m.state.mu.Unlock()
}

// endOp closes the open operation. On failure it restores the membership of
// every name the operation still owns and takes back its installs; the
// caller aborts the registration afterwards.
func (m *stdlibLazyMaterializer) endOp(commit bool) {
	if m == nil {
		return
	}
	s := m.state
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.op
	if op == nil {
		return
	}
	op.closed = true
	s.op = nil
	if commit {
		return
	}
	for name, ent := range op.entries {
		if ent.foreign {
			continue
		}
		s.tombstoned = setMember(s.tombstoned, name, ent.tomb)
		s.installed = setMember(s.installed, name, ent.installed)
	}
	s.materialized -= op.installs
}

// fence closes the open operation to new view materializations and waits
// for the admitted ones, so their writes land while the registration is
// still active. It holds no lock while waiting and is idempotent.
func (m *stdlibLazyMaterializer) fence() {
	if m == nil {
		return
	}
	m.state.mu.Lock()
	op := m.state.op
	if op != nil {
		op.closed = true
	}
	m.state.mu.Unlock()
	if op != nil {
		op.inflight.Wait()
	}
}

// opFor returns the open operation when env is its view and the operation
// still admits work, or nil. Caller holds state.mu.
func (m *stdlibLazyMaterializer) opFor(env *core.Env) *lazyOp {
	if op := m.state.op; op != nil && !op.closed && env == op.view {
		return op
	}
	return nil
}

// opEager reports whether the open operation latched eager registration.
func (m *stdlibLazyMaterializer) opEager() bool {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.op != nil && m.state.op.eager
}

// noteLocked attributes a change to name made through env: the open operation
// owns it when env is its view, closed or not, since a materialization
// admitted before fence still writes through the view; otherwise an entry the
// operation holds for name turns foreign. Caller holds state.mu.
func (m *stdlibLazyMaterializer) noteLocked(env *core.Env, name string) *lazyOp {
	op := m.state.op
	if op == nil {
		return nil
	}
	if env == op.view {
		op.own(m.state, name)
		return op
	}
	if ent, ok := op.entries[name]; ok {
		ent.foreign = true
	}
	return nil
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
func (m *stdlibLazyMaterializer) LookupAndMaterialize(env *core.Env, name string, funcNS bool) (core.Value, bool, bool) {
	if m == nil || m.engine == nil {
		return nil, false, false
	}
	m.state.mu.Lock()
	if op := m.state.op; op != nil && op.eager {
		m.state.mu.Unlock()
		return nil, false, false
	}
	if _, dead := m.state.tombstoned[name]; dead {
		m.state.mu.Unlock()
		return nil, false, false
	}
	if _, live := m.state.installed[name]; live {
		m.state.mu.Unlock()
		// Already materialized: answer only in the caller's namespace, from
		// materialized cells, so a lookup that raced its own install still
		// resolves and a binding never leaks across cells.
		if funcNS {
			if v, ok, canon := env.GetMaterializedFuncCanonical(name); ok {
				return v, true, canon
			}
			return nil, false, false
		}
		if v, ok, canon := env.GetMaterializedCanonical(name); ok {
			return v, true, canon
		}
		return nil, false, false
	}
	m.state.mu.Unlock()

	for _, key := range m.activeKeys() {
		if entry, ok := stdlibLazyTemplateRegistry.entryFor(key, name); ok {
			return m.materializeOne(env, key, entry, funcNS)
		}
	}
	return nil, false, false
}

func (m *stdlibLazyMaterializer) materializeOne(env *core.Env, key stdlibTemplateKey, entry *stdlibTemplateEntry, funcNS bool) (core.Value, bool, bool) {
	pluginName := key.pluginName
	// Per-name mutex: concurrent first-touch of one name serializes (the
	// loser observes the installed binding), disjoint names proceed in
	// parallel, and no global lock is held across env writes or execution.
	// It is never held across bootstrap execution: the definition is staged
	// without it so BeginDelete and UnloadPlugin can complete while a
	// deferred definition runs.
	nameMu := m.state.getNameMutex(entry.name)

	// Admission: revalidate under the name lock, because the selection the
	// lookup performed ran before the lock. The in-flight registration is
	// part of the same state.mu critical section as the opFor check: fence
	// closes op.closed under this mutex before its Wait, so an admitted
	// materialization is always counted before fence can observe the
	// counter, and WaitGroup's admission-at-nonzero contract holds.
	nameMu.Lock()
	m.state.mu.Lock()
	live := m.stateInstalled(entry.name)
	stale := m.stateStale(key)
	op := m.opFor(env)
	if op != nil {
		op.inflight.Add(1)
	}
	m.state.mu.Unlock()
	nameMu.Unlock()
	if op == nil {
		env = m.engine.rootEnv
	} else {
		defer op.inflight.Done()
	}
	if live || stale {
		if live {
			return m.materializedValue(env, entry.name, funcNS)
		}
		return nil, false, false
	}

	// Staging: execute the deferred definition without the name lock and
	// without publishing into env, so an UnloadPlugin or Delete that
	// completes while the definition runs is still honored by the
	// publication check below.
	staged, stagedFunc, stagedCanon, ok := m.stageEntry(env, pluginName, entry)
	if !ok {
		return nil, false, false
	}

	// Publication: a short critical section on the name's mutex. BeginDelete
	// takes this same mutex before tombstoning, so a delete or unload that
	// completed since admission is observed here and the staged binding is
	// discarded. The rollback never removes cells: nothing was written into
	// env during staging, so newer host state cannot be repaired away.
	nameMu.Lock()
	defer nameMu.Unlock()
	m.state.mu.Lock()
	live = m.stateInstalled(entry.name)
	dead := m.stateTombstoned(entry.name)
	stale = m.stateStale(key)
	m.state.mu.Unlock()
	if dead || stale {
		return nil, false, false
	}
	if live {
		return m.materializedValue(env, entry.name, funcNS)
	}
	if err := m.publishStaged(env, pluginName, entry, staged, stagedFunc, stagedCanon); err != nil {
		m.logMaterializeFailure(entry, err)
		return nil, false, false
	}
	return m.materializedValue(env, entry.name, funcNS)
}

// materializedValue answers from the materialized cells in the caller's
// namespace only.
func (m *stdlibLazyMaterializer) materializedValue(env *core.Env, name string, funcNS bool) (core.Value, bool, bool) {
	if funcNS {
		v, ok, canon := env.GetMaterializedFuncCanonical(name)
		return v, ok, canon
	}
	v, ok, canon := env.GetMaterializedCanonical(name)
	return v, ok, canon
}

// stateInstalled reports whether name is recorded installed. Separate
// helpers keep the state map reads dead-code-free at the admission and
// publication sites without widening the critical sections.
func (m *stdlibLazyMaterializer) stateInstalled(name string) bool {
	_, live := m.state.installed[name]
	return live
}

func (m *stdlibLazyMaterializer) stateTombstoned(name string) bool {
	_, dead := m.state.tombstoned[name]
	return dead
}

func (m *stdlibLazyMaterializer) stateStale(key stdlibTemplateKey) bool {
	return m.state.active[key.pluginName] != key.pluginVersion
}

// stageEntry produces the binding a deferred entry yields without writing
// into env: a Go builtin stages itself; a bootstrap definition is evaluated
// in a staging child scope and read back from there. The staged result is
// (value, bound in the function cell, canonical).
func (m *stdlibLazyMaterializer) stageEntry(env *core.Env, pluginName string, entry *stdlibTemplateEntry) (core.Value, bool, bool, bool) {
	switch entry.kind {
	case stdlibTemplateGoValue:
		return entry.value, entry.fn, entry.canonical, true
	case stdlibTemplateBootstrap:
		return m.stageBootstrap(env, entry)
	default:
		return nil, false, false, false
	}
}

// stageBootstrap executes a deferred defmacro/defn form through the env's
// installed evaluator (its core.BootstrapDefiner capability) in a staging
// child scope, exactly as the eager bootstrap loader does but without
// publishing into env. The defining form's body is not evaluated at
// definition time, so execution never re-enters materialization for the
// same name.
func (m *stdlibLazyMaterializer) stageBootstrap(env *core.Env, entry *stdlibTemplateEntry) (core.Value, bool, bool, bool) {
	owner := env.Evaluator()
	definer, ok := owner.(core.BootstrapDefiner)
	if !ok {
		m.logMaterializeFailure(entry, fmt.Errorf("stdlib bootstrap: installed evaluator %T does not implement core.BootstrapDefiner", owner))
		return nil, false, false, false
	}
	stage := env.Child()
	if _, err := definer.DefineBootstrap(context.Background(), entry.source, stage); err != nil {
		m.logMaterializeFailure(entry, err)
		return nil, false, false, false
	}
	if v, ok, canon := stage.GetMaterializedCanonical(entry.name); ok {
		return v, false, canon, true
	}
	if v, ok, canon := stage.GetMaterializedFuncCanonical(entry.name); ok {
		return v, true, canon, true
	}
	m.logMaterializeFailure(entry, fmt.Errorf("stdlib bootstrap: definer bound no cell for %s", entry.name))
	return nil, false, false, false
}

// publishStaged installs a staged binding into env. Every destination cell is
// filled only when still unoccupied, in each namespace independently: a host
// binding that landed while the staged bootstrap definition executed (the
// name mutex is not held across staging) wins, exactly as a user def/defun
// wins over the eager load. Occupancy and write run as one env-level
// insert-if-unoccupied operation, so a concurrent host write cannot slip
// between the check and the write even outside the name mutex.
func (m *stdlibLazyMaterializer) publishStaged(env *core.Env, pluginName string, entry *stdlibTemplateEntry, staged core.Value, stagedFunc, stagedCanon bool) error {
	if entry.kind == stdlibTemplateGoValue {
		return m.installValue(env, pluginName, entry)
	}
	staged = rebindStagedClosure(staged, env)
	var err error
	if stagedFunc {
		_, err = env.SetFuncIfUnoccupied(entry.name, staged, stagedCanon)
	} else {
		_, err = env.SetIfUnoccupied(entry.name, staged, stagedCanon)
		if err == nil {
			lisp2 := false
			if axis, ok := env.Evaluator().(interface{ IsLisp2() bool }); ok {
				lisp2 = axis.IsLisp2()
			}
			if lisp2 {
				_, err = env.SetFuncIfUnoccupied(entry.name, staged, false)
			}
		}
	}
	if err != nil {
		return err
	}
	m.recordInstall(env, pluginName, entry.name)
	return nil
}

// rebindStagedClosure retargets a staged defn/defmacro closure from the
// staging child scope to the destination env. The staging child exists only
// so the defining form evaluates without publishing into env; a published
// Lambda or Macro must capture the destination like its eager twin does, or
// an escaped recursive function keeps resolving through the private staged
// definition cell after the root binding is replaced or deleted.
func rebindStagedClosure(v core.Value, dest *core.Env) core.Value {
	switch c := v.(type) {
	case core.Lambda:
		if c.Env != dest {
			c.Env = dest
			return c
		}
	case *core.Lambda:
		if c.Env != dest {
			cp := *c
			cp.Env = dest
			return &cp
		}
	case core.Macro:
		if c.Env != dest {
			c.Env = dest
			return c
		}
	case *core.Macro:
		if c.Env != dest {
			cp := *c
			cp.Env = dest
			return &cp
		}
	}
	return v
}

// installValue binds a deferred Go builtin; caller holds the name's mutex.
// A cell the user already shadowed stays untouched — eager load binds
// everything before user code runs, so a user def/defun always wins;
// materialization must reproduce that order by filling only cells that are
// still empty.
func (m *stdlibLazyMaterializer) installValue(env *core.Env, pluginName string, entry *stdlibTemplateEntry) error {
	if entry.fn {
		if _, err := env.SetBothIfUnoccupied(entry.name, entry.value, entry.canonical); err != nil {
			return err
		}
	} else {
		if _, err := env.SetIfUnoccupied(entry.name, entry.value, entry.canonical); err != nil {
			return err
		}
	}
	m.recordInstall(env, pluginName, entry.name)
	return nil
}

func (m *stdlibLazyMaterializer) recordInstall(env *core.Env, pluginName, name string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.state.installed[name]; ok {
		return
	}
	if op := m.noteLocked(env, name); op != nil {
		op.installs++
	}
	if m.state.installed == nil {
		m.state.installed = make(map[string]struct{})
	}
	m.state.installed[name] = struct{}{}
	m.state.materialized++
}

// logMaterializeFailure surfaces a deferred-definition failure the miss path
// cannot propagate (lookup APIs return values, not errors). The name stays
// uninstalled, so the next touch retries; eager load fails Use() instead.
func (m *stdlibLazyMaterializer) logMaterializeFailure(entry *stdlibTemplateEntry, err error) {
	m.engine.logger.Warn("lazy stdlib materialization failed", "name", entry.name, "error", err)
}

// BeginDelete opens the deletion critical section for name: it takes the
// name's mutex, tombstones the name so no later miss resurrects it from the
// template, and returns the release func that closes the section. Env.Delete
// calls it before removing the cells and defers the release, so a
// materialization admitted before the delete cannot publish past it: the
// publication section in materializeOne takes this same mutex and re-checks
// the tombstone. Tombstones persist until the plugin is re-Used (activation
// clears them); one taken through the open operation's view is undone when
// the operation fails.
func (m *stdlibLazyMaterializer) BeginDelete(env *core.Env, name string) func() {
	if m == nil {
		return func() {}
	}
	nameMu := m.state.getNameMutex(name)
	nameMu.Lock()
	m.state.mu.Lock()
	m.noteLocked(env, name)
	if m.state.tombstoned == nil {
		m.state.tombstoned = make(map[string]struct{})
	}
	m.state.tombstoned[name] = struct{}{}
	delete(m.state.installed, name)
	m.state.mu.Unlock()
	return nameMu.Unlock
}

// deferKey returns the template key a registration through env defers into.
// Only the open stdlib (name "") operation's view defers: stdlib values are
// audited stateless (no registration-time capture of engine state), which a
// process-shared template requires. Any other env, plugin, or an eager
// operation binds immediately.
func (m *stdlibLazyMaterializer) deferKey(env *core.Env) (stdlibTemplateKey, bool) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	op := m.opFor(env)
	if op == nil || op.eager || op.name != "" {
		return stdlibTemplateKey{}, false
	}
	return stdlibTemplateKey{dialectFP: m.dialectFP, pluginName: op.name, pluginVersion: op.version}, true
}

// RegisterValue defers a Go builtin binding into the loading stdlib's
// template layer (see deferKey); otherwise it binds the value cell
// immediately, and applyVocabulary's bridge mirrors the function cell
// exactly as on an engine without the layer.
func (m *stdlibLazyMaterializer) RegisterValue(env *core.Env, name string, val core.Value, canonical bool) error {
	if m == nil {
		return nil
	}
	key, deferred := m.deferKey(env)
	if !deferred {
		if canonical {
			return env.SetCanonical(name, val)
		}
		return env.Set(name, val)
	}

	var scratch [4]core.VocabBinding
	for _, b := range m.engine.config.dialect.AppendVocabBindings(scratch[:0], name, val, canonical) {
		if err := stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
			name:      b.Name,
			kind:      stdlibTemplateGoValue,
			value:     b.Value,
			canonical: b.Canonical,
			fn:        b.Func,
			alias:     b.Name != name,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RegisterSource defers a pure-Lisp bootstrap definition (defmacro/defn).
// Same restriction as RegisterValue; it reports false whenever deferKey does
// so the caller evaluates eagerly.
func (m *stdlibLazyMaterializer) RegisterSource(env *core.Env, name, source string) bool {
	if m == nil {
		return false
	}
	key, deferred := m.deferKey(env)
	if !deferred {
		return false
	}
	if err := stdlibLazyTemplateRegistry.putEntry(key, &stdlibTemplateEntry{
		name:   name,
		kind:   stdlibTemplateBootstrap,
		source: source,
	}); err != nil {
		m.engine.logger.Warn("stdlib template layer already published, falling back to eager bootstrap", "name", name, "error", err)
		return false
	}
	return true
}

func setMember(set map[string]struct{}, name string, in bool) map[string]struct{} {
	if !in {
		delete(set, name)
		return set
	}
	if set == nil {
		set = make(map[string]struct{})
	}
	set[name] = struct{}{}
	return set
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
			m.materializeOne(env, key, entry, false)
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

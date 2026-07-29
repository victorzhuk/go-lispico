package runtime

import (
	"maps"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
)

// maxCallCacheEntries bounds engineImpl.callCache. Once len(entries) reaches
// it, the next miss flushes the cache instead of growing it — every cached
// name simply re-resolves afterward, so an overflow is a performance event,
// not a correctness one.
const maxCallCacheEntries = 1024

// callCacheEntry is a name's resolved cell cached by Engine.Call. It holds
// the same counter Fn/PinnedFn would resolve via Stats.counterFor, so
// attribution stays exact whether a call hits the cache or not. value and
// cellVer are the cell snapshot captured coherently at publish time (under
// the env lock via Env.ReadCellSnapshot): the lean path re-reads only the
// cell's atomic version and serves value while it still matches, skipping
// the per-call env RLock; any mismatch falls back to locked re-resolution.
type callCacheEntry struct {
	env     *core.Env
	gen     uint64
	cell    *core.Cell
	counter *atomic.Int64
	value   core.Value
	cellVer uint64
}

// callCache is engineImpl's per-engine name→cell cache for Engine.Call.
// Env.Rebuild is the only operation that changes which cell a name resolves
// to, and it bumps Env.NameGen, so a hit is valid only while entry.env and
// entry.gen still match the live root env. The cached {value, cellVer} pair
// is guarded by the cell's atomic mutation version: every cell mutation
// bumps version under the env write lock, so a lock-free version match
// proves the cached value is still the cell's live value — the same
// versioned-snapshot contract the VM site cache (core/vm/chunk.go siteEntry)
// relies on. Redefinition, deletion, unload, and hot-reload all mutate a
// cell in place, bumping version, so a stale value can never be served: the
// version check fails and the call re-resolves through Env.ReadCell.
type callCache struct {
	entries atomic.Pointer[map[string]*callCacheEntry]
}

func (c *callCache) snapshot() map[string]*callCacheEntry {
	if m := c.entries.Load(); m != nil {
		return *m
	}
	return nil
}

// lookup returns the cached entry for name if it is still valid for env, or
// nil on a miss — name was never cached, or its generation guard failed.
func (c *callCache) lookup(name string, env *core.Env) *callCacheEntry {
	entry, ok := c.snapshot()[name]
	if !ok || entry.env != env || entry.gen != env.NameGen() {
		return nil
	}
	return entry
}

// store publishes entry for name by cloning the current map (copy-on-write).
// A cache already at maxCallCacheEntries flushes instead of growing.
func (c *callCache) store(name string, entry *callCacheEntry) {
	for {
		old := c.entries.Load()
		var m map[string]*callCacheEntry
		if old != nil {
			m = *old
		}
		if len(m) >= maxCallCacheEntries {
			m = nil
		}
		next := make(map[string]*callCacheEntry, len(m)+1)
		maps.Copy(next, m)
		next[name] = entry
		if c.entries.CompareAndSwap(old, &next) {
			return
		}
	}
}

// drop removes name from the cache. Hygiene only, called from
// removePluginBindings: a stale entry is already harmless under the
// generation guard, this just bounds cache waste sooner.
func (c *callCache) drop(name string) {
	for {
		old := c.entries.Load()
		if old == nil {
			return
		}
		m := *old
		if _, ok := m[name]; !ok {
			return
		}
		next := maps.Clone(m)
		delete(next, name)
		if c.entries.CompareAndSwap(old, &next) {
			return
		}
	}
}

// resolveFuncCell resolves the cell Func exposes as "the operator": under
// Lisp-2 strictly the function cell — a def-bound value cell of the same
// name is a different binding to the embedder, not a fallback (pinned by
// runtime/func_handle_test.go's "lisp2 value-only closure is not a function
// binding" case).
func (e *engineImpl) resolveFuncCell(env *core.Env, name string) (*core.Cell, bool) {
	if !e.config.dialect.IsLisp2() {
		return env.Cell(name)
	}
	return env.FuncCell(name)
}

// resolveCallCell resolves the cell Engine.Call invokes, mirroring
// core/eval.go's resolveHead: under Lisp-2, the function cell first, falling
// back to the value cell — the same order a source form `(name ...)`
// resolves its head. cacheable reports whether this particular hit is safe
// for callCache to store.
//
// A Lisp-2 value-cell fallback is NEVER cacheable: Env.SetFuncWithContext
// (core/env.go) does not bump Env.NameGen when it later creates a function
// cell for name, so a cached fallback entry would never observe that
// binding appear and would keep serving the value cell forever. Every other
// hit — the sole Lisp-1 cell, or a Lisp-2 function-cell hit — is cacheable:
// Env.Rebuild is the only operation that changes cell identity there, and it
// always bumps NameGen.
func (e *engineImpl) resolveCallCell(env *core.Env, name string) (cell *core.Cell, cacheable, ok bool) {
	if !e.config.dialect.IsLisp2() {
		cell, ok = env.Cell(name)
		return cell, true, ok
	}
	if cell, ok = env.FuncCell(name); ok {
		return cell, true, true
	}
	cell, ok = env.Cell(name)
	return cell, false, ok
}

// resolveCallEntry resolves name fresh — never consulting the cache — and
// publishes the result when cacheable. gen is read BEFORE resolving: a
// Delete+Rebuild+redefine landing between resolving the cell and reading the
// generation would otherwise pair a post-Rebuild generation with the
// orphaned pre-Rebuild cell, an entry that then passes the guard forever
// while pointing at a cell Rebuild permanently tombstoned. Reading gen first
// means any mutation overlapping the resolve leaves the stored generation
// stale instead, which only costs a spurious miss.
//
// The published entry carries a coherent {value, version} snapshot of the
// cell taken under the env lock, so the lean call path can serve cached
// hits by re-reading only the cell's atomic version.
func (e *engineImpl) resolveCallEntry(env *core.Env, name string) (*callCacheEntry, bool) {
	gen := env.NameGen()
	cell, cacheable, ok := e.resolveCallCell(env, name)
	if !ok {
		return nil, false
	}
	value, live, _, ver := env.ReadCellSnapshot(cell)
	entry := &callCacheEntry{env: env, gen: gen, cell: cell, counter: e.stats.counterFor(name)}
	if live {
		entry.value = value
		entry.cellVer = ver
	}
	if cacheable {
		e.callCache.store(name, entry)
	}
	return entry, true
}

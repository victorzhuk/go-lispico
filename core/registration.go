package core

// Registration is the handle for one registration operation on a root Env.
type Registration struct {
	root    *Env
	view    *Env
	entries map[registrationKey]*registrationEntry
}

type registrationKey struct {
	name string
	fn   bool
}

type registrationEntry struct {
	prior     *Cell
	v         Value
	canonical bool
	last      *Cell
	lastVer   uint64
}

// BeginRegistration opens a registration operation on the root e resolves to.
// A root runs at most one registration at a time.
func (e *Env) BeginRegistration() (*Registration, error) {
	root := e.owner()
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.reg.Load() != nil {
		return nil, NewRegistrationActiveError()
	}
	view := &Env{
		parent:           root,
		eval:             root.eval,
		maxRetainedBytes: root.maxRetainedBytes,
		maxRetainedSlots: root.maxRetainedSlots,
	}
	r := &Registration{root: root, view: view}
	view.reg.Store(r)
	root.reg.Store(r)
	return r, nil
}

// Env returns the environment registrations write through.
func (r *Registration) Env() *Env { return r.view }

// Complete keeps the operation's writes and ends the registration.
func (r *Registration) Complete() { r.finish() }

// Abort rolls back the operation's writes and ends the registration. A key is
// restored only while the operation still owns it: its map cell is the op's
// last written cell at the version that write left. A key a foreign write
// touched afterwards keeps that write.
func (r *Registration) Abort() {
	root := r.root
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.reg.Load() != r {
		return
	}
	for key, ent := range r.entries {
		cells := root.vars
		if key.fn {
			cells = root.funcs
		}
		last := ent.last
		if last == nil || cells[key.name] != last || last.Version() != ent.lastVer {
			continue
		}
		switch ent.prior {
		case nil:
			last.v, last.canonical = nil, false
		case last:
			last.v, last.canonical = ent.v, ent.canonical
		default:
			// The op replaced the cell: reinstall the prior one and tombstone the
			// op's cell so holders of it re-resolve.
			cells[key.name] = ent.prior
			ent.prior.v, ent.prior.canonical = ent.v, ent.canonical
			ent.prior.version.Add(1)
			last.v, last.canonical = nil, false
		}
		last.version.Add(1)
	}
	root.reg.Store(nil)
	r.entries = nil
}

// finish is a no-op once r has ended, so a stale handle never touches a later
// registration on the same root.
func (r *Registration) finish() {
	r.root.mu.Lock()
	defer r.root.mu.Unlock()
	if r.root.reg.Load() != r {
		return
	}
	r.root.reg.Store(nil)
	r.entries = nil
}

// active returns r while it is e's running registration, or nil, so a write
// forwarded by a finished view stays unattributed. Caller holds e.mu.
func (e *Env) active(r *Registration) *Registration {
	if r != nil && e.reg.Load() == r {
		return r
	}
	return nil
}

// beforeWrite records the before-image of key on the first view write to it.
// Later writes keep it unless a foreign write moved the key since the op's
// last write; the before-image then advances to that foreign state, so abort
// restores what the host left rather than what preceded the operation.
// cur is the map cell before the write, nil when absent. Caller holds root.mu.
func (r *Registration) beforeWrite(key registrationKey, cur *Cell) {
	if r == nil {
		return
	}
	ent, ok := r.entries[key]
	if ok {
		if cur == ent.last && cur != nil && cur.Version() == ent.lastVer {
			return
		}
	} else {
		ent = &registrationEntry{}
		if r.entries == nil {
			r.entries = make(map[registrationKey]*registrationEntry)
		}
		r.entries[key] = ent
	}
	ent.prior, ent.v, ent.canonical = cur, nil, false
	if cur != nil {
		ent.v, ent.canonical = cur.v, cur.canonical
	}
}

// afterWrite marks cell, now in the map for key, as the operation's last
// write. Caller holds root.mu.
func (r *Registration) afterWrite(key registrationKey, cell *Cell) {
	if r == nil {
		return
	}
	ent := r.entries[key]
	ent.last = cell
	ent.lastVer = cell.Version()
}

// owner is the canonical root a registration view forwards to, or e itself.
func (e *Env) owner() *Env {
	if r := e.reg.Load(); r != nil {
		return r.root
	}
	return e
}

// viewReg returns the registration whose view e is, or nil.
func (e *Env) viewReg() *Registration {
	if r := e.reg.Load(); r != nil && r.view == e {
		return r
	}
	return nil
}

// lazy reads e's own lazy layer; a view has none, so its lookups fall through
// to the root, which consults its layer with itself as the env.
func (e *Env) lazy() LazyLayer {
	if p := e.lazyLayer.Load(); p != nil {
		return *p
	}
	return nil
}

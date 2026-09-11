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

var (
	_ = registrationKey{name: "", fn: false}
	_ = registrationEntry{prior: nil, v: nil, canonical: false, last: nil, lastVer: 0}
	_ = (*Env).viewReg
)

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

// Abort rolls back the operation's writes and ends the registration.
func (r *Registration) Abort() { r.finish() }

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

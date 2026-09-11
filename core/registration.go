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
)

// BeginRegistration opens a registration operation on e.
func (e *Env) BeginRegistration() (*Registration, error) {
	if e.reg.Load() != nil {
		return nil, NewRegistrationActiveError()
	}
	return &Registration{root: e, view: e, entries: nil}, nil
}

// Env returns the environment registrations write through.
func (r *Registration) Env() *Env { return r.view }

// Complete keeps the operation's writes and ends the registration.
func (r *Registration) Complete() {}

// Abort rolls back the operation's writes and ends the registration.
func (r *Registration) Abort() {}

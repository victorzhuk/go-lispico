package agent

import "sync"

type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
}

func newRegistry() *Registry {
	return &Registry{
		agents: make(map[string]*Agent),
	}
}

func (r *Registry) Register(a *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[a.ID] = a
}

func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	return ids
}

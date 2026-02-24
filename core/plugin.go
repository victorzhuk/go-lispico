package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Plugin is the extension point for go-lispico.
// Implement this interface to add a new domain namespace.
type Plugin interface {
	// Name returns the namespace prefix, e.g. "llm", "fs".
	// Functions registered as "complete" are callable as (llm/complete ...).
	Name() string

	// Init registers functions into env under the plugin's namespace.
	// Called once at interpreter startup. Must be idempotent.
	Init(env *Env) error

	// Metadata returns human-readable plugin information.
	Metadata() PluginMeta
}

// PluginMeta carries descriptive information about a plugin.
type PluginMeta struct {
	Version     string
	Description string
	Author      string
	Deps        []string // Go module paths this plugin requires
}

// Registry is a thread-safe store of registered plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]Plugin)}
}

// Register adds plugin p. Returns an error if a plugin with the same name is already registered.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if _, ok := r.plugins[name]; ok {
		return fmt.Errorf("plugin %q already registered", name)
	}

	r.plugins[name] = p
	return nil
}

// Get retrieves a plugin by namespace name.
func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// Namespaces returns all registered plugin names in sorted order.
func (r *Registry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unregister removes the named plugin. No-op if not registered.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, name)
}

// HasPrefix reports whether name conflicts with any registered plugin namespace.
// e.g. HasPrefix("llm/complete") returns true when "llm" plugin is registered.
func (r *Registry) HasPrefix(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ns := range r.plugins {
		if strings.HasPrefix(name, ns+"/") || name == ns {
			return true
		}
	}
	return false
}

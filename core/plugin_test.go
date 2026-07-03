package core

import (
	"testing"
)

type stubPlugin struct {
	name string
	meta PluginMeta
}

func (s *stubPlugin) Name() string         { return s.name }
func (s *stubPlugin) Init(_ *Env) error    { return nil }
func (s *stubPlugin) Metadata() PluginMeta { return s.meta }

func newStub(name string) Plugin {
	return &stubPlugin{
		name: name,
		meta: PluginMeta{Version: "1.0.0", Description: "test plugin"},
	}
}

func TestRegistry_RegisterGet(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	p := newStub("llm")

	if err := reg.Register(p); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	got, ok := reg.Get("llm")
	if !ok {
		t.Fatal("Get should find registered plugin")
	}
	if got.Name() != "llm" {
		t.Errorf("Name = %q, want llm", got.Name())
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(newStub("fs"))

	err := reg.Register(newStub("fs"))
	if err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	_, ok := reg.Get("missing")
	if ok {
		t.Error("Get should return false for unregistered plugin")
	}
}

func TestRegistry_Namespaces(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(newStub("z-plugin"))
	reg.Register(newStub("a-plugin"))
	reg.Register(newStub("m-plugin"))

	names := reg.Namespaces()
	if len(names) != 3 {
		t.Fatalf("len = %d, want 3", len(names))
	}
	// must be sorted
	if names[0] != "a-plugin" || names[1] != "m-plugin" || names[2] != "z-plugin" {
		t.Errorf("Namespaces not sorted: %v", names)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(newStub("http"))

	reg.Unregister("http")

	_, ok := reg.Get("http")
	if ok {
		t.Error("plugin should be gone after Unregister")
	}
}

func TestRegistry_UnregisterNotFound(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	// should not panic
	reg.Unregister("nonexistent")
}

func TestRegistry_HasPrefix(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(newStub("llm"))

	tests := []struct {
		name string
		want bool
	}{
		{"llm/complete", true},
		{"llm", true},
		{"llm-other", false},
		{"fs/read", false},
		{"", false},
	}
	for _, tt := range tests {
		got := reg.HasPrefix(tt.name)
		if got != tt.want {
			t.Errorf("HasPrefix(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRegistry_Namespaces_Empty(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	names := reg.Namespaces()
	if len(names) != 0 {
		t.Errorf("empty registry namespaces = %v, want []", names)
	}
}

func TestPlugin_Metadata(t *testing.T) {
	t.Parallel()
	p := newStub("test")
	meta := p.Metadata()
	if meta.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", meta.Version)
	}
}

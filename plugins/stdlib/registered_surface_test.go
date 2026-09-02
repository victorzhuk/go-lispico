package stdlib

import (
	"sort"
	"testing"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// TestStdlibRegisteredSurfaceFrozen pins the Go builtin surface each stdlib
// file registers. Each register func runs against its own empty Env, so the
// live set is per-file and free of the Lisp names bootstrap.go defines.
func TestStdlibRegisteredSurfaceFrozen(t *testing.T) {
	p := &Plugin{}
	files := map[string]func(*core.Env) error{
		"plugins/stdlib/arithmetic.go":   p.registerArithmetic,
		"plugins/stdlib/comparison.go":   p.registerComparison,
		"plugins/stdlib/strings.go":      p.registerStrings,
		"plugins/stdlib/collections.go":  p.registerCollections,
		"plugins/stdlib/higher_order.go": p.registerHigherOrder,
		"plugins/stdlib/control.go":      p.registerControl,
		"plugins/stdlib/types.go":        p.registerTypes,
	}

	if len(files) != len(inventory.RegisteredNames) {
		t.Fatalf("inventory covers %d files, test drives %d", len(inventory.RegisteredNames), len(files))
	}

	for _, file := range sortedKeys(files) {
		frozen, ok := inventory.RegisteredNames[file]
		if !ok {
			t.Errorf("%s: registers builtins but is absent from inventory.RegisteredNames", file)
			continue
		}

		env := core.NewEnv(nil)
		if err := files[file](env); err != nil {
			t.Fatalf("%s: register: %v", file, err)
		}
		live := env.LocalNames()
		sort.Strings(live)

		for _, name := range added(live, frozen) {
			t.Errorf("%s: %q registered but not in inventory.RegisteredNames", file, name)
		}
		for _, name := range added(frozen, live) {
			t.Errorf("%s: %q in inventory.RegisteredNames but no longer registered", file, name)
		}
	}

	for file := range inventory.RegisteredNames {
		if _, ok := files[file]; !ok {
			t.Errorf("%s: in inventory.RegisteredNames but not driven by this test", file)
		}
	}
}

// TestCLAdapterSurfaceFrozen pins the CL dialect's adapter ids. An adapter is
// a semantics-differing binding, so a new one is a dialect behavior change.
func TestCLAdapterSurfaceFrozen(t *testing.T) {
	var live []string
	for _, entry := range cl.Dialect().Vocab() {
		if entry.Adapter != nil {
			live = append(live, entry.AdapterID)
		}
	}
	sort.Strings(live)

	for _, id := range added(live, inventory.CLAdapterIDs) {
		t.Errorf("adapter %q bound but not in inventory.CLAdapterIDs", id)
	}
	for _, id := range added(inventory.CLAdapterIDs, live) {
		t.Errorf("adapter %q in inventory.CLAdapterIDs but no longer bound", id)
	}
}

// added returns the members of a that b does not contain.
func added(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, s := range b {
		have[s] = true
	}
	var out []string
	for _, s := range a {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]func(*core.Env) error) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

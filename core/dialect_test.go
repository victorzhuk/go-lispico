package core

import (
	"reflect"
	"testing"
)

func samePtr(a, b formFn) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func TestDialect_FullBaseIsIdentity(t *testing.T) {
	table, err := FullDialect().resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(table) != len(kernel) {
		t.Fatalf("full base size = %d, want %d", len(table), len(kernel))
	}
	for name, fn := range kernel {
		got, ok := table[name]
		if !ok {
			t.Fatalf("full base missing %q", name)
		}
		if !samePtr(got, fn) {
			t.Fatalf("full base %q not canonical form", name)
		}
	}
}

func TestDialect_EmptyBaseFailClosed(t *testing.T) {
	table, err := EmptyDialect().resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(table) != 0 {
		t.Fatalf("empty base size = %d, want 0", len(table))
	}

	table, err = EmptyDialect().Add("if", "if").resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(table) != 1 {
		t.Fatalf("empty+add size = %d, want 1", len(table))
	}
	if _, ok := table["def"]; ok {
		t.Fatal("empty base leaked kernel form def")
	}
	if !samePtr(table["if"], kernel["if"]) {
		t.Fatal("added if is not the canonical form")
	}
}

func TestDialect_Rename(t *testing.T) {
	table, err := FullDialect().Rename("if", "si").resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := table["if"]; ok {
		t.Fatal("rename left original name callable")
	}
	if !samePtr(table["si"], kernel["if"]) {
		t.Fatal("renamed name does not resolve to canonical form")
	}
}

func TestDialect_Remove(t *testing.T) {
	table, err := FullDialect().Remove("if").resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := table["if"]; ok {
		t.Fatal("removed form still callable")
	}
	if _, ok := table["def"]; !ok {
		t.Fatal("remove dropped an unrelated form")
	}
}

func TestDialect_UnknownCanonicalErrors(t *testing.T) {
	if _, err := EmptyDialect().Add("x", "no-such-form").resolve(); err == nil {
		t.Fatal("add of unknown canonical form did not error")
	}
	if _, err := FullDialect().Rename("no-such-form", "y").resolve(); err == nil {
		t.Fatal("rename of unknown canonical form did not error")
	}
}

func TestDialect_ResolveIsFreshPerCall(t *testing.T) {
	a, err := FullDialect().resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	b, err := FullDialect().resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	delete(a, "if")
	if _, ok := b["if"]; !ok {
		t.Fatal("resolved tables share backing state")
	}
}

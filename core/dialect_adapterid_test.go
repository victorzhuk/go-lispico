package core

import (
	"context"
	"strings"
	"testing"
)

func noopAdapter(name string) GoFunc {
	return GoFunc{
		Name: name,
		Fn: func(context.Context, Evaluator, []Value, *Env) (Value, error) {
			return nil, nil
		},
	}
}

// TestDialect_WithAdapterStoresSemanticID pins the VocabEntry shape
// WithAdapter produces: the semantic ID lands in AdapterID, the bound value
// in Adapter, and Canonical stays empty — an adapter entry is not a rename.
func TestDialect_WithAdapterStoresSemanticID(t *testing.T) {
	d := FullDialect().WithAdapter("sort", "cl/sort@1", noopAdapter("sort-noop"))
	entry, ok := d.Vocab()["sort"]
	if !ok {
		t.Fatal(`WithAdapter("sort", "cl/sort@1", ...) must add a "sort" vocab entry`)
	}
	if entry.AdapterID != "cl/sort@1" {
		t.Errorf("AdapterID = %q, want %q", entry.AdapterID, "cl/sort@1")
	}
	if entry.Adapter == nil {
		t.Error("Adapter = nil, want the bound value")
	}
	if entry.Canonical != "" {
		t.Errorf("Canonical = %q, want empty: an adapter entry must not double as a rename", entry.Canonical)
	}
}

// TestDialect_ResolveRejectsEmptyAdapterID asserts Dialect resolution refuses
// an adapter entry whose AdapterID is empty, instead of silently resolving it.
func TestDialect_ResolveRejectsEmptyAdapterID(t *testing.T) {
	d := FullDialect().WithAdapter("x", "", noopAdapter("x-noop"))
	if _, err := NewEvaluatorWithDialect(d); err == nil {
		t.Fatal(`NewEvaluatorWithDialect resolved an adapter entry with empty AdapterID; want error containing "has no semantic ID"`)
	} else if !strings.Contains(err.Error(), "has no semantic ID") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "has no semantic ID")
	}
}

// TestDialect_Fingerprint_AdapterIDDeterminism asserts the fingerprint hashes
// the AdapterID rather than the bound Go value: distinct values under one ID
// hash identically, one value under two IDs hashes differently, and no Go
// type identity leaks into the hash.
func TestDialect_Fingerprint_AdapterIDDeterminism(t *testing.T) {
	a := noopAdapter("sort-noop-a")
	b := noopAdapter("sort-noop-b")
	id1 := FullDialect().WithAdapter("sort", "cl/sort@1", a)
	id1Again := FullDialect().WithAdapter("sort", "cl/sort@1", b)
	id2 := FullDialect().WithAdapter("sort", "cl/sort@2", a)

	fp := id1.Fingerprint()
	if other := id1Again.Fingerprint(); other != fp {
		t.Errorf("distinct GoFunc values under (sort, cl/sort@1) must fingerprint identically; got %s vs %s", fp, other)
	}
	if rebuilt := FullDialect().WithAdapter("sort", "cl/sort@1", a).Fingerprint(); rebuilt != fp {
		t.Errorf("Fingerprint must be stable across independent builds; got %s vs %s", fp, rebuilt)
	}
	if changed := id2.Fingerprint(); changed == fp {
		t.Error("the same value under cl/sort@1 and cl/sort@2 must fingerprint differently")
	}
	if strings.Contains(fp, "core.GoFunc") {
		t.Errorf("fingerprint %q must not contain the Go type identity %q", fp, "core.GoFunc")
	}
}

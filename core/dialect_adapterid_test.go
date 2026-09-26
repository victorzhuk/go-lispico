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

// TestDialect_AdapterStoresSemanticID pins the VocabEntry shape an Adapters
// entry produces: the semantic ID lands in AdapterID, the bound value
// in Adapter, and Canonical stays empty — an adapter entry is not a rename.
func TestDialect_AdapterStoresSemanticID(t *testing.T) {
	d := mustDialect(t, spec{Adapters: map[string]Adapter{"sort": {ID: "cl/sort@1", Value: noopAdapter("sort-noop")}}})
	entry, ok := d.Vocab()["sort"]
	if !ok {
		t.Fatal(`Adapters{"sort": {ID: "cl/sort@1"}} must add a "sort" vocab entry`)
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

// TestDialect_RejectsEmptyAdapterID asserts NewDialect refuses an adapter
// whose ID is empty, instead of silently accepting it.
func TestDialect_RejectsEmptyAdapterID(t *testing.T) {
	_, err := NewDialect(spec{Adapters: map[string]Adapter{"x": {ID: "", Value: noopAdapter("x-noop")}}})
	if err == nil {
		t.Fatal(`NewDialect accepted an adapter with an empty ID; want error containing "has no semantic ID"`)
	}
	if !strings.Contains(err.Error(), "has no semantic ID") {
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
	id1 := mustDialect(t, spec{Adapters: map[string]Adapter{"sort": {ID: "cl/sort@1", Value: a}}})
	id1Again := mustDialect(t, spec{Adapters: map[string]Adapter{"sort": {ID: "cl/sort@1", Value: b}}})
	id2 := mustDialect(t, spec{Adapters: map[string]Adapter{"sort": {ID: "cl/sort@2", Value: a}}})

	fp := id1.Fingerprint()
	if other := id1Again.Fingerprint(); other != fp {
		t.Errorf("distinct GoFunc values under (sort, cl/sort@1) must fingerprint identically; got %s vs %s", fp, other)
	}
	if rebuilt := mustDialect(t, spec{Adapters: map[string]Adapter{"sort": {ID: "cl/sort@1", Value: a}}}).Fingerprint(); rebuilt != fp {
		t.Errorf("Fingerprint must be stable across independent builds; got %s vs %s", fp, rebuilt)
	}
	if changed := id2.Fingerprint(); changed == fp {
		t.Error("the same value under cl/sort@1 and cl/sort@2 must fingerprint differently")
	}
	if strings.Contains(fp, "core.GoFunc") {
		t.Errorf("fingerprint %q must not contain the Go type identity %q", fp, "core.GoFunc")
	}
}

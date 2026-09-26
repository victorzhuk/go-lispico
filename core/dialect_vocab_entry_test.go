package core

import (
	"context"
	"testing"
)

func TestDialect_VocabEntryLookup(t *testing.T) {
	adapter := GoFunc{Name: "noop", Fn: func(context.Context, Evaluator, []Value, *Env) (Value, error) {
		return Nil{}, nil
	}}
	d := mustDialect(t, spec{
		Vocab:    map[string]string{"car": "first"},
		Adapters: map[string]Adapter{"noop": {ID: "noop@1", Value: adapter}},
	})

	t.Run("present rename", func(t *testing.T) {
		entry, ok := d.VocabEntry("car")
		if !ok {
			t.Fatal("VocabEntry(car) not found")
		}
		if entry.Canonical != "first" || entry.Adapter != nil {
			t.Fatalf("VocabEntry(car) = %+v, want Canonical first", entry)
		}
	})

	t.Run("present adapter", func(t *testing.T) {
		entry, ok := d.VocabEntry("noop")
		if !ok {
			t.Fatal("VocabEntry(noop) not found")
		}
		if entry.AdapterID != "noop@1" || entry.Adapter == nil {
			t.Fatalf("VocabEntry(noop) = %+v, want AdapterID noop@1 and a non-nil Adapter", entry)
		}
	})

	t.Run("absent name", func(t *testing.T) {
		entry, ok := d.VocabEntry("cdr")
		if ok || entry != (VocabEntry{}) {
			t.Fatalf("VocabEntry(cdr) = %+v, %v; want zero entry, false", entry, ok)
		}
	})

	t.Run("nil vocabulary", func(t *testing.T) {
		entry, ok := Dialect{}.VocabEntry("car")
		if ok || entry != (VocabEntry{}) {
			t.Fatalf("Dialect{}.VocabEntry(car) = %+v, %v; want zero entry, false", entry, ok)
		}
	})
}

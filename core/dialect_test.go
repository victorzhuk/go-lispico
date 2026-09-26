package core

import (
	"context"
	"reflect"
	"testing"
)

func samePtr(a, b formFn) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func TestDialect_FullBaseIsIdentity(t *testing.T) {
	table := Dialect{}.resolve()
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
	if table := mustDialect(t, spec{Base: emptyBase}).resolve(); len(table) != 0 {
		t.Fatalf("empty base size = %d, want 0", len(table))
	}

	table := mustDialect(t, spec{Base: emptyBase, Forms: map[string]string{"if": "if"}}).resolve()
	if len(table) != 1 {
		t.Fatalf("empty+if size = %d, want 1", len(table))
	}
	if _, ok := table["def"]; ok {
		t.Fatal("empty base leaked kernel form def")
	}
	if !samePtr(table["if"], kernel["if"]) {
		t.Fatal("mapped if is not the canonical form")
	}
}

func TestDialect_Rename(t *testing.T) {
	table := mustDialect(t, spec{Forms: map[string]string{"si": "if"}, Hide: []string{"if"}}).resolve()
	if _, ok := table["if"]; ok {
		t.Fatal("rename left original name callable")
	}
	if !samePtr(table["si"], kernel["if"]) {
		t.Fatal("renamed name does not resolve to canonical form")
	}
}

func TestDialect_Remove(t *testing.T) {
	table := mustDialect(t, spec{Hide: []string{"if"}}).resolve()
	if _, ok := table["if"]; ok {
		t.Fatal("removed form still callable")
	}
	if _, ok := table["def"]; !ok {
		t.Fatal("remove dropped an unrelated form")
	}
}

func TestDialect_UnknownCanonicalErrors(t *testing.T) {
	if _, err := NewDialect(spec{Base: emptyBase, Forms: map[string]string{"x": "no-such-form"}}); err == nil {
		t.Fatal("mapping to an unknown canonical form did not error")
	}
}

func TestDialect_VocabReturnsCopy(t *testing.T) {
	t.Run("vocab set", func(t *testing.T) {
		d := mustDialect(t, spec{Vocab: map[string]string{"car": "first"}})
		fp := d.Fingerprint()

		v := d.Vocab()
		v["cdr"] = VocabEntry{Canonical: "rest"}
		delete(v, "car")

		got := d.Vocab()
		if len(got) != 1 {
			t.Fatalf("writes to the returned map leaked into the Dialect: vocab = %v, want only car", got)
		}
		if got["car"].Canonical != "first" {
			t.Fatalf("Vocab()[car].Canonical = %q, want first after deleting car from the returned map", got["car"].Canonical)
		}
		if d.Fingerprint() != fp {
			t.Fatal("Fingerprint changed after writes to the map Vocab returned")
		}
	})

	t.Run("vocab nil", func(t *testing.T) {
		if got := (Dialect{}).Vocab(); got != nil {
			t.Fatalf("Dialect{}.Vocab() = %v, want nil", got)
		}
	})

	t.Run("vocab empty", func(t *testing.T) {
		d := mustDialect(t, spec{Base: emptyBase, Vocab: map[string]string{}})
		v := d.Vocab()
		if v == nil || len(v) != 0 {
			t.Fatalf("Vocab() = %#v, want a non-nil empty map", v)
		}
		v["first"] = VocabEntry{Canonical: "first"}
		if got := d.Vocab(); got == nil || len(got) != 0 {
			t.Fatalf("write to the returned map leaked: next Vocab() = %v, want non-nil and empty", got)
		}
	})
}

func TestDialect_FingerprintStoredAtBuild(t *testing.T) {
	cl := clShapedSpec()
	cl.Vocab = map[string]string{"car": "first", "cdr": "rest"}
	corpus := []struct {
		name string
		spec spec
	}{
		{name: "identity", spec: spec{}},
		{name: "empty base", spec: spec{Base: emptyBase, Forms: map[string]string{"if": "if"}}},
		{name: "cl-shaped", spec: cl},
		{name: "clojure-shaped", spec: spec{FlatCond: true}},
	}

	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			d := mustDialect(t, tc.spec)
			if got, want := d.Fingerprint(), mustDialect(t, tc.spec).Fingerprint(); got != want {
				t.Fatalf("Fingerprint() = %q, want byte-identical to an independent build %q", got, want)
			}
			if allocs := testing.AllocsPerRun(50, func() { _ = d.Fingerprint() }); allocs != 0 {
				t.Fatalf("Fingerprint() allocs = %.1f, want 0", allocs)
			}
		})
	}
}

func TestDialect_FingerprintFieldsDoNotCollide(t *testing.T) {
	tests := []struct {
		name string
		a, b Dialect
	}{
		{
			name: "vocabulary separator in name vs canonical",
			a:    mustDialect(t, spec{Vocab: map[string]string{"a:b": "c"}}),
			b:    mustDialect(t, spec{Vocab: map[string]string{"a": "b:c"}}),
		},
		{
			name: "form separators in one name vs two names",
			a:    mustDialect(t, spec{Forms: map[string]string{"a:if|1:b": "if"}}),
			b:    mustDialect(t, spec{Forms: map[string]string{"a": "if", "b": "if"}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if fa, fb := tt.a.Fingerprint(), tt.b.Fingerprint(); fa == fb {
				t.Fatalf("Fingerprint() collides at %q for dialects differing only in separator placement; want distinct digests", fa)
			}
		})
	}
}

func TestDialect_RedefinitionDoesNotLeakAcrossEngines(t *testing.T) {
	var d Dialect

	engA, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect engA: %v", err)
	}
	engB, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect engB: %v", err)
	}
	if !samePtr(engA.forms["if"], engB.forms["if"]) {
		t.Fatal("engines built from one dialect must share the resolved forms table")
	}

	ctx := context.Background()
	envA := NewEnv(nil)
	envB := NewEnv(nil)

	defForm, err := Read(`(def if "shadowed")`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := engA.Eval(ctx, defForm[0], envA); err != nil {
		t.Fatalf("def if on engine A: %v", err)
	}

	ifForm, err := Read(`(if false :y :n)`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	gotA, err := engA.Eval(ctx, ifForm[0], envA)
	if err != nil {
		t.Fatalf("engine A if: %v", err)
	}
	if !(Keyword{V: "n"}).Equals(gotA) {
		t.Fatalf("engine A: redefining if as a value must not change special-form dispatch, got %v", gotA)
	}

	gotB, err := engB.Eval(ctx, ifForm[0], envB)
	if err != nil {
		t.Fatalf("engine B if: %v", err)
	}
	if !(Keyword{V: "n"}).Equals(gotB) {
		t.Fatalf("engine B: engine A's redefinition leaked through the shared dialect, got %v", gotB)
	}

	val, ok := envA.Get("if")
	if !ok || !(String{V: "shadowed"}).Equals(val) {
		t.Fatal("def if on engine A should still bind the value cell")
	}
	if _, ok := envB.Get("if"); ok {
		t.Fatal("engine B's env must not see engine A's binding")
	}
}

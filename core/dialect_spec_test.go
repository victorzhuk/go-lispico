package core

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type (
	spec    = DialectSpec
	adapter = Adapter
)

const emptyBase = BaseEmpty

var newDialect = NewDialect

func buildDialect(s spec) (Dialect, error) { return newDialect(s) }

func mustDialect(t *testing.T, s spec) Dialect {
	t.Helper()
	d, err := buildDialect(s)
	if err != nil {
		t.Fatalf("NewDialect: %v", err)
	}
	return d
}

func clShapedSpec() spec {
	return spec{
		Forms:       map[string]string{"progn": "do", "setq": "set!"},
		Hide:        []string{"do", "set!"},
		Lisp2:       true,
		NoBrackets:  true,
		FunctionRef: true,
		Vocab:       map[string]string{"car": "first"},
	}
}

func assertRefused(t *testing.T, s spec, name, want string) {
	t.Helper()
	d, err := buildDialect(s)
	if err == nil {
		t.Fatalf("NewDialect accepted the spec; want an error containing %q", want)
	}
	if !reflect.DeepEqual(d, Dialect{}) {
		t.Errorf("NewDialect returned %+v alongside an error, want the zero Dialect", d)
	}
	msg := err.Error()
	for _, sub := range []string{"dialect: ", strconv.Quote(name), want} {
		if !strings.Contains(msg, sub) {
			t.Errorf("error = %q, want it to contain %q", msg, sub)
		}
	}
}

func TestNewDialect_RejectsUnknownKernelForm(t *testing.T) {
	assertRefused(t, spec{Forms: map[string]string{"nope": "nope"}}, "nope", "maps to unknown kernel form")
}

func TestNewDialect_RejectsHiddenNameAbsentFromBase(t *testing.T) {
	tests := []struct {
		name string
		spec spec
		hide string
	}{
		{name: "full base", spec: spec{Hide: []string{"nope"}}, hide: "nope"},
		{name: "empty base", spec: spec{Base: emptyBase, Hide: []string{"if"}}, hide: "if"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRefused(t, tt.spec, tt.hide, "is not in the base")
		})
	}
}

func TestNewDialect_RejectsAdapterWithoutID(t *testing.T) {
	s := spec{Adapters: map[string]adapter{"x": {Value: noopAdapter("x-noop")}}}
	assertRefused(t, s, "x", "has no semantic ID")
}

func TestNewDialect_RejectsAdapterWithoutValue(t *testing.T) {
	s := spec{Adapters: map[string]adapter{"x": {ID: "x@1"}}}
	assertRefused(t, s, "x", "has no value")
}

func TestNewDialect_RejectsVocabAdapterConflict(t *testing.T) {
	s := spec{
		Vocab:    map[string]string{"x": "first"},
		Adapters: map[string]adapter{"x": {ID: "x@1", Value: noopAdapter("x-noop")}},
	}
	assertRefused(t, s, "x", "is both a vocabulary rename and an adapter")
}

func TestNewDialect_RejectsHiddenAndMappedName(t *testing.T) {
	s := spec{Forms: map[string]string{"if": "if"}, Hide: []string{"if"}}
	assertRefused(t, s, "if", "is both hidden and mapped")
}

func TestNewDialect_RejectsLisp2ReservedForm(t *testing.T) {
	for _, name := range []string{"funcall", "function"} {
		t.Run(name, func(t *testing.T) {
			s := spec{Lisp2: true, Forms: map[string]string{name: "if"}}
			assertRefused(t, s, name, "is reserved by the Lisp-2 namespace")
		})
	}
}

func TestNewDialect_SpecMutationDoesNotLeak(t *testing.T) {
	s := spec{
		Forms:    map[string]string{"progn": "do"},
		Hide:     []string{"do"},
		Vocab:    map[string]string{"car": "first"},
		Adapters: map[string]adapter{"sort": {ID: "cl/sort@1", Value: noopAdapter("sort-noop")}},
	}
	d := mustDialect(t, s)
	fp := d.Fingerprint()

	s.Vocab["car"] = "rest"
	s.Vocab["cdr"] = "rest"
	s.Forms["progn"] = "if"
	s.Forms["begin"] = "do"
	s.Hide[0] = "if"
	s.Adapters["sort"] = adapter{ID: "cl/sort@2", Value: noopAdapter("sort-noop-2")}
	s.Adapters["uniq"] = adapter{ID: "cl/uniq@1", Value: noopAdapter("uniq-noop")}

	v := d.Vocab()
	if len(v) != 2 {
		t.Errorf("Vocab() has %d entries after caller mutation, want 2 (car, sort): %v", len(v), v)
	}
	if got := v["car"]; got.Canonical != "first" {
		t.Errorf("Vocab()[car].Canonical = %q after caller mutation, want %q", got.Canonical, "first")
	}
	if got := v["sort"]; got.AdapterID != "cl/sort@1" || got.Adapter == nil {
		t.Errorf("Vocab()[sort] = {AdapterID: %q, Adapter: %v} after caller mutation, want AdapterID %q with a value", got.AdapterID, got.Adapter, "cl/sort@1")
	}

	names := []struct {
		name      string
		canonical string
		isPresent bool
	}{
		{name: "progn", canonical: "do", isPresent: true},
		{name: "do"},
		{name: "if", canonical: "if", isPresent: true},
		{name: "begin"},
	}
	for _, tt := range names {
		canonical, ok := d.CanonicalName(tt.name)
		if canonical != tt.canonical || ok != tt.isPresent {
			t.Errorf("CanonicalName(%q) = (%q, %v) after caller mutation, want (%q, %v)",
				tt.name, canonical, ok, tt.canonical, tt.isPresent)
		}
	}

	if got := d.Fingerprint(); got != fp {
		t.Errorf("Fingerprint changed after caller mutation: %s, want %s", got, fp)
	}
}

func TestNewDialect_RenameThroughSpec(t *testing.T) {
	d := mustDialect(t, spec{Forms: map[string]string{"si": "if"}, Hide: []string{"if"}})

	if canonical, ok := d.CanonicalName("si"); canonical != "if" || !ok {
		t.Errorf(`CanonicalName("si") = (%q, %v), want ("if", true)`, canonical, ok)
	}
	if canonical, ok := d.CanonicalName("if"); canonical != "" || ok {
		t.Errorf(`CanonicalName("if") = (%q, %v), want ("", false)`, canonical, ok)
	}

	e, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect: %v", err)
	}
	fn, ok := e.forms["si"]
	if !ok || !samePtr(fn, kernel["if"]) {
		t.Error("si must dispatch to the kernel if form")
	}
	if _, ok := e.forms["if"]; ok {
		t.Error("hidden if must be absent from the dispatch table")
	}
	if len(e.forms) != len(kernel) {
		t.Errorf("dispatch table size = %d, want %d (one rename)", len(e.forms), len(kernel))
	}
}

func TestNewDialect_RemovalMakesFormUncallable(t *testing.T) {
	d := mustDialect(t, spec{Hide: []string{"when"}})

	if canonical, ok := d.CanonicalName("when"); canonical != "" || ok {
		t.Errorf(`CanonicalName("when") = (%q, %v), want ("", false)`, canonical, ok)
	}

	e, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect: %v", err)
	}
	if _, ok := e.forms["when"]; ok {
		t.Error("hidden when must be absent from the dispatch table")
	}
	if len(e.forms) != len(kernel)-1 {
		t.Errorf("dispatch table size = %d, want %d", len(e.forms), len(kernel)-1)
	}
}

func TestNewDialect_EmptyBaseExposesOnlySpecifiedForms(t *testing.T) {
	d := mustDialect(t, spec{Base: emptyBase, Forms: map[string]string{"if": "if", "si": "if", "quote": "quote"}})

	if !d.IsBaseEmpty() {
		t.Error("IsBaseEmpty() = false, want true")
	}
	if canonical, ok := d.CanonicalName("def"); canonical != "" || ok {
		t.Errorf(`CanonicalName("def") = (%q, %v), want ("", false)`, canonical, ok)
	}

	e, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect: %v", err)
	}
	want := map[string]string{"if": "if", "si": "if", "quote": "quote"}
	if len(e.forms) != len(want) {
		t.Errorf("dispatch table size = %d, want %d", len(e.forms), len(want))
	}
	for name, canonical := range want {
		fn, ok := e.forms[name]
		if !ok || !samePtr(fn, kernel[canonical]) {
			t.Errorf("%s must dispatch to the kernel %s form", name, canonical)
		}
	}
}

func TestNewDialect_FrozenAxesAndVocabulary(t *testing.T) {
	t.Run("axes on", func(t *testing.T) {
		d := mustDialect(t, spec{Lisp2: true, NoBrackets: true, FunctionRef: true, ReaderVector: true, FlatCond: true})

		if !d.IsLisp2() {
			t.Error("IsLisp2() = false, want true")
		}
		if _, err := d.Read("[1]"); err == nil {
			t.Error("NoBrackets must make [1] fail to parse")
		}
		vals, err := d.Read("#'f")
		if err != nil {
			t.Fatalf("Read(#'f): %v", err)
		}
		if want := NewList([]Value{Symbol{V: "function"}, Symbol{V: "f"}}); !want.Equals(vals[0]) {
			t.Errorf("FunctionRef: #'f read as %v, want (function f)", vals[0])
		}
		vals, err = d.Read("#(1 2)")
		if err != nil {
			t.Fatalf("Read(#(1 2)): %v", err)
		}
		if _, ok := vals[0].(Vector); !ok {
			t.Errorf("ReaderVector: #(1 2) read as %T, want Vector", vals[0])
		}
		clauses, err := d.NormalizeCond([]Value{Bool{V: true}, Int{V: 1}})
		if err != nil {
			t.Fatalf("NormalizeCond: %v", err)
		}
		if len(clauses) != 1 {
			t.Errorf("FlatCond: got %d clauses, want 1", len(clauses))
		}
	})

	t.Run("axes off", func(t *testing.T) {
		d := mustDialect(t, spec{})

		if d.IsLisp2() {
			t.Error("IsLisp2() = true, want false")
		}
		vals, err := d.Read("[1]")
		if err != nil {
			t.Fatalf("Read([1]): %v", err)
		}
		if _, ok := vals[0].(Vector); !ok {
			t.Errorf("[1] read as %T, want Vector", vals[0])
		}
		if _, err := d.NormalizeCond([]Value{Bool{V: true}, Int{V: 1}}); err == nil {
			t.Error("nested cond must reject a bare test/expression pair")
		}
	})

	t.Run("vocabulary", func(t *testing.T) {
		tests := []struct {
			name    string
			spec    spec
			wantNil bool
			wantLen int
		}{
			{name: "vocab and adapters nil", spec: spec{}, wantNil: true},
			{name: "empty vocab", spec: spec{Vocab: map[string]string{}}},
			{name: "empty adapters", spec: spec{Adapters: map[string]adapter{}}},
			{name: "vocab set", spec: spec{Vocab: map[string]string{"car": "first"}}, wantLen: 1},
			{
				name:    "adapters set",
				spec:    spec{Adapters: map[string]adapter{"sort": {ID: "cl/sort@1", Value: noopAdapter("sort-noop")}}},
				wantLen: 1,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v := mustDialect(t, tt.spec).Vocab()
				if tt.wantNil {
					if v != nil {
						t.Errorf("Vocab() = %v, want nil", v)
					}
					return
				}
				if v == nil || len(v) != tt.wantLen {
					t.Errorf("Vocab() = %#v, want a non-nil map of %d entries", v, tt.wantLen)
				}
			})
		}
	})

	t.Run("duplicate hide", func(t *testing.T) {
		d := mustDialect(t, spec{Hide: []string{"when", "when"}})
		if canonical, ok := d.CanonicalName("when"); canonical != "" || ok {
			t.Errorf(`CanonicalName("when") = (%q, %v), want ("", false)`, canonical, ok)
		}
	})
}

func TestNewDialect_AccessorsDoNotAllocate(t *testing.T) {
	d := mustDialect(t, clShapedSpec())

	accessors := []struct {
		name string
		fn   func()
	}{
		{name: "Fingerprint", fn: func() { _ = d.Fingerprint() }},
		{name: "CanonicalName", fn: func() { _, _ = d.CanonicalName("setq") }},
		{name: "IsLisp2", fn: func() { _ = d.IsLisp2() }},
		{name: "VocabEntry", fn: func() { _, _ = d.VocabEntry("car") }},
	}
	for _, tt := range accessors {
		t.Run(tt.name, func(t *testing.T) {
			if allocs := testing.AllocsPerRun(100, tt.fn); allocs != 0 {
				t.Errorf("%s allocates %v times per call, want 0", tt.name, allocs)
			}
		})
	}
}

func TestNewDialect_EvaluatorsShareResolution(t *testing.T) {
	d := mustDialect(t, clShapedSpec())

	a, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect: %v", err)
	}
	b, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect: %v", err)
	}
	if reflect.ValueOf(a.forms).UnsafePointer() != reflect.ValueOf(b.forms).UnsafePointer() {
		t.Error("two evaluators built from one spec-built Dialect must share one forms map")
	}
}

func TestNewDialect_CondBodyUsesVisibleDo(t *testing.T) {
	d := mustDialect(t, spec{Forms: map[string]string{"begin": "do", "progn": "do"}, Hide: []string{"do"}})

	args, err := d.Read("(x 1 2)")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	clauses, err := d.NormalizeCond(args)
	if err != nil {
		t.Fatalf("NormalizeCond: %v", err)
	}
	if len(clauses) != 1 {
		t.Fatalf("got %d clauses, want 1", len(clauses))
	}
	body, ok := clauses[0].(List).At(1).(List)
	if !ok {
		t.Fatalf("clause body = %v, want a wrapped list", clauses[0])
	}
	if head := body.At(0); !(Symbol{V: "begin"}).Equals(head) {
		t.Errorf("multi-body clause wrapped with %v, want begin", head)
	}
}

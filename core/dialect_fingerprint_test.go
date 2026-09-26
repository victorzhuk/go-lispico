package core

import "testing"

func kernelIdentityForms() map[string]string {
	forms := make(map[string]string, len(kernel))
	for name := range kernel {
		forms[name] = name
	}
	return forms
}

func TestNewDialect_EqualDispatchEqualFingerprint(t *testing.T) {
	t.Parallel()

	t.Run("full base identity mapping equals no mapping and zero dialect", func(t *testing.T) {
		t.Parallel()
		mapped := mustDialect(t, spec{Forms: map[string]string{"if": "if"}}).Fingerprint()
		bare := mustDialect(t, spec{}).Fingerprint()
		zero := Dialect{}.Fingerprint()
		if mapped != bare {
			t.Errorf("Forms{if:if} fingerprint %s != DialectSpec{} fingerprint %s: an identity mapping must not change the fingerprint", mapped, bare)
		}
		if bare != zero {
			t.Errorf("DialectSpec{} fingerprint %s != Dialect{} fingerprint %s", bare, zero)
		}
	})

	t.Run("full base redundant identity entry", func(t *testing.T) {
		t.Parallel()
		a := mustDialect(t, spec{Forms: map[string]string{"x": "if"}}).Fingerprint()
		b := mustDialect(t, spec{Forms: map[string]string{"x": "if", "let": "let"}}).Fingerprint()
		if a != b {
			t.Errorf("Forms{x:if} fingerprint %s != Forms{x:if, let:let} fingerprint %s: equal dispatch must give equal fingerprints", a, b)
		}
	})

	t.Run("empty base map literal order", func(t *testing.T) {
		t.Parallel()
		a := mustDialect(t, spec{Base: emptyBase, Forms: map[string]string{"if": "if", "let": "let"}}).Fingerprint()
		b := mustDialect(t, spec{Base: emptyBase, Forms: map[string]string{"let": "let", "if": "if"}}).Fingerprint()
		if a != b {
			t.Errorf("empty base Forms{if, let} fingerprints differ by literal order: %s != %s", a, b)
		}
	})
}

func TestDialect_FingerprintSeparatesVocabPresenceAndBase(t *testing.T) {
	t.Parallel()

	t.Run("empty base nil vocab vs empty vocab", func(t *testing.T) {
		t.Parallel()
		none := mustDialect(t, spec{Base: emptyBase}).Fingerprint()
		empty := mustDialect(t, spec{Base: emptyBase, Vocab: map[string]string{}}).Fingerprint()
		if none == empty {
			t.Errorf("empty base with nil Vocab and with Vocab{} share fingerprint %s: vocabulary presence must be hashed", none)
		}
	})

	t.Run("full base vs empty base listing every kernel form", func(t *testing.T) {
		t.Parallel()
		full := mustDialect(t, spec{}).Fingerprint()
		listed := mustDialect(t, spec{Base: emptyBase, Forms: kernelIdentityForms()}).Fingerprint()
		if full == listed {
			t.Errorf("full base and empty base listing every kernel form share fingerprint %s: base must be hashed", full)
		}
	})
}

func TestNewDialect_NoOpFormsKeepIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec spec
		want bool
	}{
		{name: "identity mapping", spec: spec{Forms: map[string]string{"if": "if"}}, want: true},
		{name: "empty spec", spec: spec{}, want: true},
		{name: "flat cond only", spec: spec{FlatCond: true}, want: true},
		{name: "lisp2", spec: spec{Lisp2: true}, want: false},
		{name: "real rename", spec: spec{Forms: map[string]string{"progn": "do"}, Hide: []string{"do"}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mustDialect(t, tt.spec).IsIdentity(); got != tt.want {
				t.Errorf("IsIdentity() = %v, want %v", got, tt.want)
			}
		})
	}
}

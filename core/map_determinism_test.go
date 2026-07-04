package core

import "testing"

func TestEval_MapLiteralDeterministic(t *testing.T) {
	t.Parallel()
	// Two distinct key forms that evaluate to the same key: the result must be
	// the same on every run, not dependent on Go's randomized map order.
	want, _ := NewHashMap().Assoc(Keyword{V: "dup"}, Int{V: 2})
	for range 100 {
		env := newTestEnv()
		env.Set("x", Keyword{V: "dup"})
		env.Set("y", Keyword{V: "dup"})
		got := evalStr(t, env, "{x 1 y 2}")
		if !got.Equals(want) {
			t.Fatalf("{x 1 y 2} with x=y=:dup = %s, want %s (nondeterministic map eval)", got.String(), want.String())
		}
	}
}

func TestHashMap_StringDeterministic(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		var err error
		m, err = m.Assoc(Keyword{V: k}, Int{V: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	first := m.String()
	for range 50 {
		if got := m.String(); got != first {
			t.Fatalf("String() nondeterministic: %s != %s", got, first)
		}
	}
}

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

func TestDialect_MemoizedConstructionSharesResolution(t *testing.T) {
	d := FullDialect().Memoized()
	if d.cache == nil {
		t.Fatal("Memoized() must populate a cache")
	}

	a, err := d.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	b, err := d.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	delete(a, "if")
	if _, ok := b["if"]; ok {
		t.Fatal("a memoized Dialect's resolve() must return the same shared table on repeated calls, not recompute it")
	}

	if got := d.Fingerprint(); got != d.cache.fp {
		t.Fatalf("Fingerprint() on a memoized Dialect must return the cached hash, got %q want %q", got, d.cache.fp)
	}
}

func TestDialect_MutationInvalidatesCache(t *testing.T) {
	base := FullDialect().Lisp2().Memoized()
	baseTable, err := base.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	baseFP := base.Fingerprint()
	baseCache := base.cache
	if baseCache == nil {
		t.Fatal("base dialect must be memoized")
	}

	assertInvalidated := func(t *testing.T, mutated Dialect) {
		t.Helper()
		if mutated.cache != nil {
			t.Fatal("mutated copy must not carry the base's cache")
		}
		if base.cache != baseCache {
			t.Fatal("mutating a copy must not disturb the singleton's own cache")
		}
		if mutated.Fingerprint() == baseFP {
			t.Fatal("mutated copy's Fingerprint() must differ from the singleton's")
		}

		table, err := base.resolve()
		if err != nil {
			t.Fatalf("resolve base: %v", err)
		}
		if !reflect.DeepEqual(table, baseTable) {
			t.Fatal("mutating a copy must not change the singleton's resolved table")
		}
	}

	t.Run("Add", func(t *testing.T) {
		mutated := base.Add("si", "if")
		table, err := mutated.resolve()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, ok := table["si"]; !ok {
			t.Fatal("Add must appear in the mutated copy's resolved table")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("Rename", func(t *testing.T) {
		mutated := base.Rename("if", "si")
		table, err := mutated.resolve()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, ok := table["if"]; ok {
			t.Fatal("Rename must drop the original name from the mutated copy")
		}
		if _, ok := table["si"]; !ok {
			t.Fatal("Rename must add the new name to the mutated copy")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("Remove", func(t *testing.T) {
		mutated := base.Remove("if")
		table, err := mutated.resolve()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, ok := table["if"]; ok {
			t.Fatal("Remove must drop the name from the mutated copy")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("FlatCond", func(t *testing.T) {
		mutated := base.FlatCond()
		clauses, err := mutated.NormalizeCond([]Value{Bool{V: true}, Int{V: 1}})
		if err != nil {
			t.Fatalf("NormalizeCond: %v", err)
		}
		if len(clauses) != 1 {
			t.Fatalf("FlatCond must parse flat pairs, got %d clauses", len(clauses))
		}
		assertInvalidated(t, mutated)
	})

	t.Run("Lisp2", func(t *testing.T) {
		mutated := FullDialect().Memoized().Lisp2()
		table, err := mutated.resolve()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, ok := table["funcall"]; !ok {
			t.Fatal("Lisp2 must inject funcall into the mutated copy's resolved table")
		}
		if mutated.cache != nil {
			t.Fatal("mutated copy must not carry the base's cache")
		}
	})

	t.Run("WithoutBracketLiterals", func(t *testing.T) {
		mutated := base.WithoutBracketLiterals()
		if _, err := mutated.Read("[1]"); err == nil {
			t.Fatal("WithoutBracketLiterals must make [1] fail to parse in the mutated copy")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("WithFunctionRef", func(t *testing.T) {
		mutated := base.WithFunctionRef()
		vals, err := mutated.Read("#'f")
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		want := NewList([]Value{Symbol{V: "function"}, Symbol{V: "f"}})
		if !want.Equals(vals[0]) {
			t.Fatalf("WithFunctionRef must enable #' parsing in the mutated copy, got %v", vals[0])
		}
		assertInvalidated(t, mutated)
	})

	t.Run("WithReaderVector", func(t *testing.T) {
		mutated := base.WithReaderVector()
		vals, err := mutated.Read("#(1 2)")
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if _, ok := vals[0].(Vector); !ok {
			t.Fatal("WithReaderVector must enable #(...) parsing in the mutated copy")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("Vocabulary", func(t *testing.T) {
		mutated := base.Vocabulary(map[string]string{"car": "first"})
		if _, ok := mutated.Vocab()["car"]; !ok {
			t.Fatal("Vocabulary must appear in the mutated copy")
		}
		assertInvalidated(t, mutated)
	})

	t.Run("WithAdapter", func(t *testing.T) {
		adapter := GoFunc{Name: "noop", Fn: func(context.Context, Evaluator, []Value, *Env) (Value, error) {
			return Nil{}, nil
		}}
		mutated := base.WithAdapter("noop", adapter)
		entry, ok := mutated.Vocab()["noop"]
		if !ok || entry.Adapter == nil {
			t.Fatal("WithAdapter must appear in the mutated copy's vocabulary")
		}
		assertInvalidated(t, mutated)
	})
}

func TestDialect_FingerprintStableUnderMemoization(t *testing.T) {
	corpus := []struct {
		name       string
		build      func() Dialect
		buildFresh func() Dialect
	}{
		{
			name:       "identity",
			build:      func() Dialect { return FullDialect().Memoized() },
			buildFresh: func() Dialect { return FullDialect() },
		},
		{
			name:       "empty base",
			build:      func() Dialect { return EmptyDialect().Add("if", "if").Memoized() },
			buildFresh: func() Dialect { return EmptyDialect().Add("if", "if") },
		},
		{
			name: "cl-shaped",
			build: func() Dialect {
				return FullDialect().
					Lisp2().
					WithoutBracketLiterals().
					WithFunctionRef().
					WithReaderVector().
					Add("defun", "defn").
					Rename("set!", "setq").
					Rename("do", "progn").
					Vocabulary(map[string]string{"car": "first", "cdr": "rest"}).
					Memoized()
			},
			buildFresh: func() Dialect {
				return FullDialect().
					Lisp2().
					WithoutBracketLiterals().
					WithFunctionRef().
					WithReaderVector().
					Add("defun", "defn").
					Rename("set!", "setq").
					Rename("do", "progn").
					Vocabulary(map[string]string{"car": "first", "cdr": "rest"})
			},
		},
		{
			name:       "clojure-shaped",
			build:      func() Dialect { return FullDialect().FlatCond().Memoized() },
			buildFresh: func() Dialect { return FullDialect().FlatCond() },
		},
	}

	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			memoized := tc.build()
			fresh := tc.buildFresh()
			if got, want := memoized.Fingerprint(), fresh.Fingerprint(); got != want {
				t.Fatalf("memoized Fingerprint() = %q, want byte-identical to uncached %q", got, want)
			}
		})
	}
}

func TestDialect_RedefinitionDoesNotLeakAcrossEngines(t *testing.T) {
	d := FullDialect().Memoized()

	engA, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect engA: %v", err)
	}
	engB, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("NewEvaluatorWithDialect engB: %v", err)
	}
	if !samePtr(engA.forms["if"], engB.forms["if"]) {
		t.Fatal("engines built from a memoized dialect must share the resolved forms table")
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

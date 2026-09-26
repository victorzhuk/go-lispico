package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/runtime"
)

type canonicalGolden struct {
	name      string
	canonical string
	ok        bool
}

func TestCL_StockFormTable(t *testing.T) {
	d := cl.Dialect()

	t.Run("canonical names", func(t *testing.T) {
		tests := []canonicalGolden{
			{name: "if", canonical: "if", ok: true},
			{name: "def", canonical: "def", ok: true},
			{name: "defn", canonical: "defn", ok: true},
			{name: "defmacro", canonical: "defmacro", ok: true},
			{name: "fn", canonical: "fn", ok: true},
			{name: "let", canonical: "let", ok: true},
			{name: "let*", canonical: "let*", ok: true},
			{name: "do"},
			{name: "quote", canonical: "quote", ok: true},
			{name: "quasiquote", canonical: "quasiquote", ok: true},
			{name: "set!"},
			{name: "when", canonical: "when", ok: true},
			{name: "cond", canonical: "cond", ok: true},
			{name: "loop", canonical: "loop", ok: true},
			{name: "recur", canonical: "recur", ok: true},
			{name: "try", canonical: "try", ok: true},
			{name: "catch", canonical: "catch", ok: true},
			{name: "throw", canonical: "throw", ok: true},
			{name: "and", canonical: "and", ok: true},
			{name: "or", canonical: "or", ok: true},
			{name: "not", canonical: "not", ok: true},
			{name: "defun", canonical: "defn", ok: true},
			{name: "setq", canonical: "set!", ok: true},
			{name: "progn", canonical: "do", ok: true},
			{name: "funcall", canonical: "funcall", ok: true},
			{name: "function", canonical: "function", ok: true},
			{name: "no-such-form"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				canonical, ok := d.CanonicalName(tc.name)
				assert.Equal(t, tc, canonicalGolden{name: tc.name, canonical: canonical, ok: ok})
			})
		}
	})

	t.Run("vocab", func(t *testing.T) {
		renames := map[string]string{
			"car":     "first",
			"cdr":     "rest",
			"null":    "nil?",
			"cons":    "cons",
			"list":    "list",
			"append":  "concat",
			"length":  "count",
			"reverse": "reverse",
			"apply":   "apply",
			"type":    "type",
		}
		adapters := map[string]string{
			"nth":    "cl/nth@1",
			"mapcar": "cl/mapcar@1",
			"sort":   "cl/sort@1",
		}

		vocab := d.Vocab()
		require.Len(t, vocab, len(renames)+len(adapters))
		for name, canonical := range renames {
			entry, ok := vocab[name]
			require.True(t, ok, "vocab must bind %q", name)
			assert.Equal(t, core.VocabEntry{Canonical: canonical}, entry, "vocab entry %q", name)

			got, ok := d.VocabEntry(name)
			require.True(t, ok, "VocabEntry(%q)", name)
			assert.Equal(t, entry, got, "VocabEntry(%q) must match Vocab()", name)
		}
		for name, id := range adapters {
			entry, ok := vocab[name]
			require.True(t, ok, "vocab must bind adapter %q", name)
			assert.Empty(t, entry.Canonical, "adapter %q canonical", name)
			assert.Equal(t, id, entry.AdapterID, "adapter %q id", name)
			fn, ok := entry.Adapter.(core.GoFunc)
			require.True(t, ok, "adapter %q must be a GoFunc, got %T", name, entry.Adapter)
			assert.Equal(t, name, fn.Name, "adapter %q GoFunc name", name)

			got, ok := d.VocabEntry(name)
			require.True(t, ok, "VocabEntry(%q)", name)
			assert.Equal(t, id, got.AdapterID, "VocabEntry(%q) adapter id", name)
		}
		_, ok := d.VocabEntry("first")
		assert.False(t, ok, "canonical builtin names are not vocab keys")
	})

	t.Run("axes", func(t *testing.T) {
		assert.True(t, d.IsLisp2(), "IsLisp2")
		assert.False(t, d.IsBaseEmpty(), "IsBaseEmpty")
		assert.False(t, d.IsIdentity(), "IsIdentity")
		assert.NotEmpty(t, d.Fingerprint(), "Fingerprint")
		assert.Equal(t, d.Fingerprint(), cl.Dialect().Fingerprint(), "Fingerprint must be stable")
	})

	t.Run("reader", func(t *testing.T) {
		tests := []struct {
			src     string
			wantErr bool
			want    string
			kind    string
		}{
			{src: "[1]", wantErr: true},
			{src: "#'f", want: "(function f)", kind: "core.List"},
			{src: "#(1 2)", want: "[1 2]", kind: "core.Vector"},
		}
		for _, tc := range tests {
			t.Run(tc.src, func(t *testing.T) {
				forms, err := d.Read(tc.src)
				if tc.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.Len(t, forms, 1)
				assert.Equal(t, tc.want, forms[0].String())
				assert.Equal(t, tc.kind, typeName(forms[0]))
			})
		}
	})

	t.Run("normalize cond", func(t *testing.T) {
		args, err := d.Read("(a 1 2) (b 3)")
		require.NoError(t, err)
		clauses, err := d.NormalizeCond(args)
		require.NoError(t, err)
		assert.Equal(t, []string{"(a (progn 1 2))", "(b 3)"}, valueStrings(clauses))

		flat, err := d.Read("a 1 b 2")
		require.NoError(t, err)
		_, err = d.NormalizeCond(flat)
		require.Error(t, err, "flat pairs must be rejected under nested cond")
	})
}

func TestCL_StockCorpusBothEvaluators(t *testing.T) {
	corpus := []struct {
		name string
		src  []string
		want core.Value
	}{
		{name: "defun", src: []string{"(defun sq (x) (* x x))", "(sq 7)"}, want: core.Int{V: 49}},
		{name: "defn kernel name", src: []string{"(defn inc2 (x) (+ x 2))", "(inc2 1)"}, want: core.Int{V: 3}},
		{name: "def and setq", src: []string{"(def x 1)", "(setq x 5)", "x"}, want: core.Int{V: 5}},
		{name: "progn", src: []string{"(progn 1 2 3)"}, want: core.Int{V: 3}},
		{name: "cond multi-body", src: []string{"(cond (false 1) (true 2 3))"}, want: core.Int{V: 3}},
		{name: "cond fallthrough", src: []string{"(cond (false 1))"}, want: core.Nil{}},
		{name: "let", src: []string{"(let ((a 1) (b 2)) (+ a b))"}, want: core.Int{V: 3}},
		{name: "loop recur", src: []string{"(loop ((i 0) (acc 0)) (if (< i 4) (recur (+ i 1) (+ acc i)) acc))"}, want: core.Int{V: 6}},
		{name: "when", src: []string{"(when true 1 2)"}, want: core.Int{V: 2}},
		{name: "try throw", src: []string{"(try (throw :boom) (catch e e))"}, want: core.Keyword{V: "boom"}},
		{name: "defmacro", src: []string{"(defmacro twice (x) (list 'progn x x))", "(twice 4)"}, want: core.Int{V: 4}},
		{name: "funcall function ref", src: []string{"(defun id (x) x)", "(funcall #'id 9)"}, want: core.Int{V: 9}},
		{name: "lisp2 cells", src: []string{"(def f 10)", "(defun f (x) (+ x 1))", "(f f)"}, want: core.Int{V: 11}},
		{name: "car cdr", src: []string{"(car (cdr '(1 2 3)))"}, want: core.Int{V: 2}},
		{name: "append length", src: []string{"(length (append '(1 2) '(3)))"}, want: core.Int{V: 3}},
		{name: "null", src: []string{"(null nil)"}, want: core.Bool{V: true}},
		{name: "cons reverse", src: []string{"(reverse (cons 1 (list 2 3)))"}, want: intList(3, 2, 1)},
		{name: "nth", src: []string{"(nth 1 '(10 20 30))"}, want: core.Int{V: 20}},
		{name: "nth out of range", src: []string{"(nth 5 '(10 20 30))"}, want: core.Nil{}},
		{name: "mapcar", src: []string{"(mapcar #'+ '(1 2) '(10 20))"}, want: intList(11, 22)},
		{name: "mapcar fn", src: []string{"(mapcar (fn (x) (* x 2)) '(1 2 3))"}, want: intList(2, 4, 6)},
		{name: "sort", src: []string{"(sort '(3 1 2) #'<)"}, want: intList(1, 2, 3)},
		{name: "sort key", src: []string{"(sort '(\"bb\" \"a\" \"ccc\") #'< :key #'length)"}, want: core.NewList([]core.Value{core.String{V: "a"}, core.String{V: "bb"}, core.String{V: "ccc"}})},
		{name: "and or not", src: []string{"(list (and 1 2) (or nil 3) (not nil))"}, want: core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 3}, core.Bool{V: true}})},
	}

	modes := []struct {
		name string
		opt  runtime.EngineOption
	}{
		{name: "tree-walker", opt: runtime.WithTreeWalker()},
		{name: "vm", opt: runtime.WithBytecode()},
	}

	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			results := make([]core.Value, len(modes))
			for i, mode := range modes {
				e := newEngine(t, mode.opt)
				var got core.Value
				for _, src := range tc.src {
					var err error
					got, err = e.Eval(context.Background(), "cl-parity", src)
					require.NoError(t, err, "%s: %s", mode.name, src)
				}
				require.True(t, tc.want.Equals(got), "%s: got %v, want %v", mode.name, got, tc.want)
				results[i] = got
			}
			assert.True(t, results[0].Equals(results[1]), "evaluators disagree: tree-walker %v, vm %v", results[0], results[1])
		})
	}
}

func typeName(v core.Value) string {
	switch v.(type) {
	case core.List:
		return "core.List"
	case core.Vector:
		return "core.Vector"
	default:
		return "other"
	}
}

func valueStrings(vals []core.Value) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = v.String()
	}
	return out
}

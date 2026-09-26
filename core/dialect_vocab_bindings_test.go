package core

import (
	"reflect"
	"testing"
)

type vocabBinding = VocabBinding

var appendVocabBindings = Dialect.AppendVocabBindings

func assertBindings(t *testing.T, got, want []vocabBinding) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AppendVocabBindings = %+v; want %+v", got, want)
	}
}

func TestDialect_AppendVocabBindings_Table(t *testing.T) {
	t.Parallel()
	first := GoFunc{Name: "first"}
	car := GoFunc{Name: "car"}
	plus := GoFunc{Name: "+"}
	tail := GoFunc{Name: "tail"}
	cons := GoFunc{Name: "cons"}
	listStar := GoFunc{Name: "list*"}
	clListStar := GoFunc{Name: "cl-list*"}

	tests := []struct {
		name      string
		zero      bool
		spec      spec
		reg       string
		v         Value
		canonical bool
		want      []vocabBinding
	}{
		{
			name:      "non-GoFunc under Lisp-2 binds only its own name",
			spec:      spec{Lisp2: true, Vocab: map[string]string{"car": "first"}},
			reg:       "first",
			v:         Int{V: 7},
			canonical: true,
			want:      []vocabBinding{{Name: "first", Value: Int{V: 7}, Canonical: true, Func: false}},
		},
		{
			name:      "zero dialect",
			zero:      true,
			reg:       "+",
			v:         plus,
			canonical: true,
			want:      []vocabBinding{{Name: "+", Value: plus, Canonical: true, Func: false}},
		},
		{
			name:      "nil vocabulary under Lisp-1 has no function cell",
			spec:      spec{NoBrackets: true},
			reg:       "first",
			v:         first,
			canonical: false,
			want:      []vocabBinding{{Name: "first", Value: first, Canonical: false, Func: false}},
		},
		{
			name:      "nil vocabulary under Lisp-2 binds the function cell",
			spec:      spec{Lisp2: true},
			reg:       "first",
			v:         first,
			canonical: true,
			want:      []vocabBinding{{Name: "first", Value: first, Canonical: true, Func: true}},
		},
		{
			name:      "identity entry keeps canonical and is not its own alias",
			spec:      spec{Lisp2: true, Vocab: map[string]string{"+": "+"}},
			reg:       "+",
			v:         plus,
			canonical: true,
			want:      []vocabBinding{{Name: "+", Value: plus, Canonical: true, Func: true}},
		},
		{
			name: "adapter entry binds the adapter value non-canonical",
			spec: spec{Lisp2: true, Adapters: map[string]adapter{
				"list*": {ID: "cl-list*", Value: clListStar},
			}},
			reg:       "list*",
			v:         listStar,
			canonical: true,
			want:      []vocabBinding{{Name: "list*", Value: clListStar, Canonical: false, Func: true}},
		},
		{
			name:      "empty base drops an unlisted name",
			spec:      spec{Base: emptyBase, Lisp2: true, Vocab: map[string]string{"car": "first"}},
			reg:       "cons",
			v:         cons,
			canonical: true,
			want:      nil,
		},
		{
			name:      "empty base rename through a dropped canonical emits only the alias",
			spec:      spec{Base: emptyBase, Lisp2: true, Vocab: map[string]string{"car": "first"}},
			reg:       "first",
			v:         first,
			canonical: true,
			want:      []vocabBinding{{Name: "car", Value: first, Canonical: false, Func: true}},
		},
		{
			name:      "full base keeps a plugin binding under a rename's visible name",
			spec:      spec{Lisp2: true, Vocab: map[string]string{"car": "first"}},
			reg:       "car",
			v:         car,
			canonical: true,
			want:      []vocabBinding{{Name: "car", Value: car, Canonical: true, Func: true}},
		},
		{
			name:      "aliases of one canonical follow in sorted order under Lisp-2",
			spec:      spec{Lisp2: true, Vocab: map[string]string{"rest": "tail", "cdr": "tail"}},
			reg:       "tail",
			v:         tail,
			canonical: true,
			want: []vocabBinding{
				{Name: "tail", Value: tail, Canonical: true, Func: true},
				{Name: "cdr", Value: tail, Canonical: false, Func: true},
				{Name: "rest", Value: tail, Canonical: false, Func: true},
			},
		},
		{
			name:      "aliases of one canonical follow in sorted order under Lisp-1",
			spec:      spec{Vocab: map[string]string{"rest": "tail", "cdr": "tail"}},
			reg:       "tail",
			v:         tail,
			canonical: false,
			want: []vocabBinding{
				{Name: "tail", Value: tail, Canonical: false, Func: false},
				{Name: "cdr", Value: tail, Canonical: false, Func: false},
				{Name: "rest", Value: tail, Canonical: false, Func: false},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var d Dialect
			if !tt.zero {
				d = mustDialect(t, tt.spec)
			}
			assertBindings(t, appendVocabBindings(d, nil, tt.reg, tt.v, tt.canonical), tt.want)
		})
	}
}

func TestDialect_AppendVocabBindings_AppendsToDst(t *testing.T) {
	t.Parallel()
	d := mustDialect(t, spec{Lisp2: true, Vocab: map[string]string{"car": "first"}})
	first := GoFunc{Name: "first"}
	prefix := vocabBinding{Name: "pre", Value: Int{V: 1}}
	dst := []vocabBinding{prefix}

	got := appendVocabBindings(d, dst, "first", first, true)

	assertBindings(t, got, []vocabBinding{
		prefix,
		{Name: "first", Value: first, Canonical: true, Func: true},
		{Name: "car", Value: first, Canonical: false, Func: true},
	})
}

func TestDialect_AppendVocabBindings_ZeroAllocs(t *testing.T) {
	d := mustDialect(t, spec{Lisp2: true, Vocab: map[string]string{"car": "first", "head": "first"}})
	name := "first"
	var v Value = GoFunc{Name: "first"}
	dst := make([]vocabBinding, 0, 3)

	got := appendVocabBindings(d, dst, name, v, true)
	names := make([]string, len(got))
	for i, b := range got {
		names[i] = b.Name
	}
	if want := []string{"first", "car", "head"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("AppendVocabBindings names = %v; want %v", names, want)
	}

	var sink []vocabBinding
	allocs := testing.AllocsPerRun(100, func() {
		sink = appendVocabBindings(d, dst[:0], name, v, true)
	})
	if allocs != 0 {
		t.Errorf("AppendVocabBindings with cap(dst) >= 1+aliases allocates %v per call; want 0", allocs)
	}
	_ = sink
}

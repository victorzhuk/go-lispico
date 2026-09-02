package stdlib

import (
	"context"
	"fmt"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// boundaryRow pins one boundary outcome: want != nil is a value row compared
// with Equals; want == nil is an error row asserted by code and message.
type boundaryRow struct {
	name, input string
	want        core.Value
	code, msg   string
}

func assertRow(t *testing.T, env *core.Env, row boundaryRow) {
	t.Helper()
	if row.want == nil {
		wantTypedError(t, evalErr(t, env, row.input), row.code, row.msg)
		return
	}
	got := eval(t, env, row.input)
	if !row.want.Equals(got) {
		t.Errorf("%s: expected %v, got %v", row.input, row.want, got)
	}
}

type callCounter struct {
	calls    []core.Value
	badArity error
}

func newCallCounterEnv(t *testing.T) (*core.Env, *callCounter) {
	t.Helper()
	env := setupEnv(t)
	c := &callCounter{}
	if err := env.Set("count2", core.GoFunc{
		Name: "count2",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				c.badArity = fmt.Errorf("count2: got %d arguments, want 1", len(args))
				return core.Nil{}, c.badArity
			}
			c.calls = append(c.calls, args[0])
			return core.Int{V: int64(len(c.calls) - 1)}, nil
		},
	}); err != nil {
		t.Fatalf("bind count2: %v", err)
	}
	return env, c
}

func TestNilSequence_EmptyListCharacterization(t *testing.T) {
	emptyList := core.NewList(nil)
	ints := func(vs ...int64) []core.Value {
		out := make([]core.Value, len(vs))
		for i, v := range vs {
			out[i] = core.Int{V: v}
		}
		return out
	}

	t.Run("values", func(t *testing.T) {
		env := setupEnv(t)
		rows := []boundaryRow{
			{name: "first", input: `(first '())`, want: core.Nil{}},
			{name: "rest", input: `(rest '())`, want: emptyList},
			{name: "last", input: `(last '())`, want: core.Nil{}},
			{name: "count", input: `(count '())`, want: core.Int{V: 0}},
			{name: "empty?", input: `(empty? '())`, want: core.Bool{V: true}},
			{name: "reverse", input: `(reverse '())`, want: emptyList},
			{name: "sort", input: `(sort '())`, want: emptyList},
			{name: "concat", input: `(concat '())`, want: emptyList},
			{name: "nth default", input: `(nth '() 0 :d)`, want: core.Keyword{V: "d"}},
			{name: "nth out of bounds", input: `(nth '() 0)`, code: "EvalError", msg: "nth: index out of bounds"},
			{name: "cons", input: `(cons 1 '())`, want: core.NewList(ints(1))},
			{name: "conj list keeps written order", input: `(conj '() 1 2)`, want: core.NewList(ints(1, 2))},
			{name: "conj vector", input: `(conj [] 1 2)`, want: core.NewVector(ints(1, 2))},
			{name: "string/join", input: `(string/join "," '())`, want: core.String{V: ""}},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
		}
	})

	t.Run("output types", func(t *testing.T) {
		env := setupEnv(t)
		for _, input := range []string{`(rest '())`, `(reverse '())`, `(sort '())`, `(concat '())`, `(cons 1 '())`, `(conj '() 1 2)`} {
			if got := eval(t, env, input); fmt.Sprintf("%T", got) != "core.List" {
				t.Errorf("%s: expected core.List, got %T", input, got)
			}
		}
		if got := eval(t, env, `(conj [] 1 2)`); fmt.Sprintf("%T", got) != "core.Vector" {
			t.Errorf("(conj [] 1 2): expected core.Vector, got %T", got)
		}
	})

	t.Run("callback counts", func(t *testing.T) {
		rows := []struct {
			name, input string
			want        core.Value
			calls       int
		}{
			{"map", `(map count2 '())`, emptyList, 0},
			{"filter", `(filter count2 '())`, emptyList, 0},
			{"reduce no init", `(reduce count2 '())`, core.Nil{}, 0},
			{"reduce init", `(reduce count2 :init '())`, core.Keyword{V: "init"}, 0},
			{"apply", `(apply count2 1 '())`, core.Int{V: 0}, 1},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				env, c := newCallCounterEnv(t)
				got := eval(t, env, row.input)
				if !row.want.Equals(got) {
					t.Errorf("%s: expected %v, got %v", row.input, row.want, got)
				}
				if len(c.calls) != row.calls {
					t.Errorf("%s: expected %d callbacks, got %d", row.input, row.calls, len(c.calls))
				}
				if c.badArity != nil {
					t.Errorf("%s: %v", row.input, c.badArity)
				}
			})
		}
	})

	t.Run("apply passes the leading argument", func(t *testing.T) {
		env, c := newCallCounterEnv(t)
		eval(t, env, `(apply count2 1 '())`)
		if len(c.calls) != 1 || !(core.Int{V: 1}).Equals(c.calls[0]) {
			t.Fatalf("expected count2 called once with 1, got %v", c.calls)
		}
	})
}

func TestNilSequence_ScalarCharacterization(t *testing.T) {
	env, c := newCallCounterEnv(t)
	rows := []boundaryRow{
		{name: "empty? 1", input: `(empty? 1)`, want: core.Bool{V: false}},
		{name: "empty? 5", input: `(empty? 5)`, want: core.Bool{V: false}},
		{name: "first", input: `(first 5)`, code: "TypeError", msg: "first: expected collection, got core.Int"},
		{name: "rest", input: `(rest 5)`, code: "TypeError", msg: "rest: expected collection, got core.Int"},
		{name: "last", input: `(last 5)`, code: "TypeError", msg: "last: expected collection, got core.Int"},
		{name: "count", input: `(count 5)`, code: "TypeError", msg: "count: expected collection, got core.Int"},
		{name: "reverse", input: `(reverse 5)`, code: "TypeError", msg: "reverse: expected collection, got core.Int"},
		{name: "sort", input: `(sort 5)`, code: "TypeError", msg: "sort: expected collection, got core.Int"},
		{name: "concat", input: `(concat 5)`, code: "TypeError", msg: "concat: expected collection, got core.Int"},
		{name: "nth", input: `(nth 5 0)`, code: "TypeError", msg: "nth: expected collection, got core.Int"},
		{name: "cons", input: `(cons 1 5)`, code: "TypeError", msg: "cons: expected collection, got core.Int"},
		{name: "conj", input: `(conj 5 1)`, code: "TypeError", msg: "conj: expected collection, got core.Int"},
		{name: "map", input: `(map count2 5)`, code: "TypeError", msg: "map: second argument must be collection"},
		{name: "filter", input: `(filter count2 5)`, code: "TypeError", msg: "filter: second argument must be collection"},
		{name: "reduce", input: `(reduce count2 5)`, code: "TypeError", msg: "reduce: last argument must be collection"},
		{name: "reduce init", input: `(reduce count2 0 5)`, code: "TypeError", msg: "reduce: last argument must be collection"},
		{name: "apply", input: `(apply count2 5)`, code: "TypeError", msg: "apply: last argument must be collection, got core.Int"},
		{name: "string/join coll", input: `(string/join "," 5)`, code: "TypeError", msg: "string/join: expected collection, got core.Int"},
		{name: "string/join sep", input: `(string/join 5 '())`, code: "TypeError", msg: "string/join: separator must be string"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
	}
	if len(c.calls) != 0 {
		t.Errorf("scalar inputs must not reach the callback, got %d calls", len(c.calls))
	}
}

func TestNilSequence_ValueModelUnchanged(t *testing.T) {
	env := setupEnv(t)
	rows := []boundaryRow{
		{name: "nil? nil", input: `(nil? nil)`, want: core.Bool{V: true}},
		{name: "nil? '()", input: `(nil? '())`, want: core.Bool{V: false}},
		{name: "list? nil", input: `(list? nil)`, want: core.Bool{V: false}},
		{name: "list? '()", input: `(list? '())`, want: core.Bool{V: true}},
		{name: "= nil '()", input: `(= nil '())`, want: core.Bool{V: false}},
		{name: "str nil", input: `(str nil)`, want: core.String{V: "nil"}},
		{name: "str '()", input: `(str '())`, want: core.String{V: "()"}},
		{name: "if nil", input: `(if nil :y :n)`, want: core.Keyword{V: "n"}},
		{name: "if '()", input: `(if '() :y :n)`, want: core.Keyword{V: "y"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
	}
}

// callSeqInput turns a panic from the adapter into a test failure so the
// remaining rows and tests still report.
func callSeqInput(t *testing.T, v core.Value) (got []core.Value, ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("seqInput(%v) panicked: %v", v, r)
		}
	}()
	return seqInput(v)
}

func TestSeqInput_AcceptedAndRejectedTypes(t *testing.T) {
	one, two := core.Int{V: 1}, core.Int{V: 2}
	rows := []struct {
		name string
		in   core.Value
		ok   bool
		want []core.Value
	}{
		{"Nil", core.Nil{}, true, nil},
		{"List", core.NewList([]core.Value{one, two}), true, []core.Value{one, two}},
		{"Vector", core.NewVector([]core.Value{one, two}), true, []core.Value{one, two}},
		{"Bool", core.Bool{V: true}, false, nil},
		{"Int", core.Int{V: 5}, false, nil},
		{"Float", core.Float{V: 1.5}, false, nil},
		{"String", core.String{V: "s"}, false, nil},
		{"Symbol", core.Symbol{V: "s"}, false, nil},
		{"Keyword", core.Keyword{V: "k"}, false, nil},
		{"HashMap", core.NewHashMap(), false, nil},
		{"GoFunc", core.GoFunc{Name: "f"}, false, nil},
		{"Lambda", core.Lambda{Name: "l"}, false, nil},
		{"Macro", core.Macro{Name: "m"}, false, nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := callSeqInput(t, row.in)
			if ok != row.ok {
				t.Fatalf("seqInput(%s): ok = %v, want %v", row.name, ok, row.ok)
			}
			if len(got) != len(row.want) {
				t.Fatalf("seqInput(%s): got %d elements, want %d", row.name, len(got), len(row.want))
			}
			for i, w := range row.want {
				if !w.Equals(got[i]) {
					t.Errorf("seqInput(%s)[%d] = %v, want %v", row.name, i, got[i], w)
				}
			}
		})
	}
}

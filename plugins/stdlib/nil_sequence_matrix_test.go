package stdlib

import (
	"context"
	"fmt"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestNilSequenceBoundary_ClosedMatrix is the closed nil-position table:
// accepted rows take the empty-list branch and must agree with the same call
// on '(); unlisted rows keep prior behavior so nil never leaks into a position
// the matrix does not name.
func TestNilSequenceBoundary_ClosedMatrix(t *testing.T) {
	emptyList := core.NewList(nil)
	one := core.NewList([]core.Value{core.Int{V: 1}})
	nilElem := core.NewList([]core.Value{core.Nil{}})

	t.Run("accepted", func(t *testing.T) {
		env := setupEnv(t)
		rows := []struct {
			row  boundaryRow
			twin string
		}{
			{boundaryRow{name: "first", input: `(first nil)`, want: core.Nil{}}, `(first '())`},
			{boundaryRow{name: "rest", input: `(rest nil)`, want: emptyList}, `(rest '())`},
			{boundaryRow{name: "last", input: `(last nil)`, want: core.Nil{}}, `(last '())`},
			{boundaryRow{name: "count", input: `(count nil)`, want: core.Int{V: 0}}, `(count '())`},
			{boundaryRow{name: "empty?", input: `(empty? nil)`, want: core.Bool{V: true}}, `(empty? '())`},
			{boundaryRow{name: "sort", input: `(sort nil)`, want: emptyList}, `(sort '())`},
			{boundaryRow{name: "concat", input: `(concat nil)`, want: emptyList}, `(concat '())`},
			{boundaryRow{name: "concat mixed", input: `(concat nil '(1) nil)`, want: one}, `(concat '() '(1) '())`},
			{boundaryRow{name: "reverse", input: `(reverse nil)`, want: emptyList}, `(reverse '())`},
			{boundaryRow{name: "nth out of bounds", input: `(nth nil 0)`, code: "EvalError", msg: "nth: index out of bounds"}, `(nth '() 0)`},
			{boundaryRow{name: "nth default", input: `(nth nil 0 :missing)`, want: core.Keyword{V: "missing"}}, `(nth '() 0 :missing)`},
		}
		for _, tt := range rows {
			t.Run(tt.row.name, func(t *testing.T) {
				assertRow(t, env, tt.row)
				twin := tt.row
				twin.input = tt.twin
				assertRow(t, env, twin)
			})
		}
	})

	t.Run("accepted output types", func(t *testing.T) {
		env := setupEnv(t)
		for _, input := range []string{`(rest nil)`, `(reverse nil)`, `(sort nil)`, `(concat nil)`} {
			got, err := evalValue(env, input)
			if err != nil {
				t.Errorf("%s: %v", input, err)
				continue
			}
			if fmt.Sprintf("%T", got) != "core.List" {
				t.Errorf("%s: expected core.List, got %T", input, got)
			}
		}
	})

	t.Run("unlisted", func(t *testing.T) {
		env := setupEnv(t)
		rows := []boundaryRow{
			{name: "nth index", input: `(nth '(1) nil)`, code: "TypeError", msg: "nth: index must be integer"},
			{name: "keys", input: `(keys nil)`, code: "TypeError", msg: "keys: expected map, got core.Nil"},
			{name: "vals", input: `(vals nil)`, code: "TypeError", msg: "vals: expected map, got core.Nil"},
			{name: "contains?", input: `(contains? nil :a)`, code: "TypeError", msg: "contains?: expected map, got core.Nil"},
			{name: "assoc", input: `(assoc nil :a 1)`, code: "TypeError", msg: "assoc: expected map, got core.Nil"},
			{name: "dissoc", input: `(dissoc nil :a)`, code: "TypeError", msg: "dissoc: expected map, got core.Nil"},
			{name: "string/join separator", input: `(string/join nil '())`, code: "TypeError", msg: "string/join: separator must be string"},
			{name: "cons element", input: `(cons nil '())`, want: nilElem},
			{name: "conj element", input: `(conj '() nil)`, want: nilElem},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
		}

		t.Run("apply non-final argument", func(t *testing.T) {
			env, c := newCallCounterEnv(t)
			got := eval(t, env, `(apply count2 nil '())`)
			if !(core.Int{V: 0}).Equals(got) {
				t.Errorf("(apply count2 nil '()): expected 0, got %v", got)
			}
			if len(c.calls) != 1 || !(core.Nil{}).Equals(c.calls[0]) {
				t.Errorf("expected count2 called once with nil, got %v", c.calls)
			}
			if c.badArity != nil {
				t.Errorf("%v", c.badArity)
			}
		})

		t.Run("function position", func(t *testing.T) {
			env := setupEnv(t)
			for _, input := range []string{`(map nil '(1))`, `(filter nil '(1))`} {
				if err := evalErr(t, env, input); err == nil {
					t.Errorf("%s: expected an error, got nil", input)
				}
			}
		})
	})
}

func TestNth_NilBothArities(t *testing.T) {
	env := setupEnv(t)
	rows := []boundaryRow{
		{name: "two args", input: `(nth nil 0)`, code: "EvalError", msg: "nth: index out of bounds"},
		{name: "three args", input: `(nth nil 0 :missing)`, want: core.Keyword{V: "missing"}},
		{name: "negative index", input: `(nth nil -1)`, code: "EvalError", msg: "nth: index out of bounds"},
		{name: "float index", input: `(nth nil 1.5)`, code: "TypeError", msg: "nth: index must be integer"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
	}
}

// evalValue is eval without the fatal on error, so a red row reports and the
// remaining rows still run.
func evalValue(env *core.Env, code string) (core.Value, error) {
	forms, err := core.Read(code)
	if err != nil {
		return nil, err
	}
	return core.NewEvaluator().Eval(context.Background(), forms[0], env)
}

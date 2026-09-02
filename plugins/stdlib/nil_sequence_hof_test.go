package stdlib

import (
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestNilSequenceBoundary_HigherOrderJoin(t *testing.T) {
	emptyList := core.NewList(nil)
	env, c := newCallCounterEnv(t)
	rows := []boundaryRow{
		{name: "map nil", input: `(map count2 nil)`, want: emptyList},
		{name: "filter nil", input: `(filter count2 nil)`, want: emptyList},
		{name: "reduce nil", input: `(reduce count2 nil)`, want: core.Nil{}},
		{name: "reduce init nil", input: `(reduce count2 :init nil)`, want: core.Keyword{V: "init"}},
		{name: "apply prefix retained", input: `(apply + 1 2 nil)`, want: core.Int{V: 3}},
		{name: "string/join nil", input: `(string/join "," nil)`, want: core.String{V: ""}},
		{name: "map scalar", input: `(map count2 5)`, code: "TypeError", msg: "map: second argument must be collection"},
		{name: "filter scalar", input: `(filter count2 5)`, code: "TypeError", msg: "filter: second argument must be collection"},
		{name: "reduce scalar", input: `(reduce count2 5)`, code: "TypeError", msg: "reduce: last argument must be collection"},
		{name: "reduce init scalar", input: `(reduce count2 0 5)`, code: "TypeError", msg: "reduce: last argument must be collection"},
		{name: "apply scalar", input: `(apply count2 5)`, code: "TypeError", msg: "apply: last argument must be collection, got core.Int"},
		{name: "string/join scalar coll", input: `(string/join "," 5)`, code: "TypeError", msg: "string/join: expected collection, got core.Int"},
		{name: "string/join scalar sep nil coll", input: `(string/join 5 nil)`, code: "TypeError", msg: "string/join: separator must be string"},
		{name: "string/join nil sep", input: `(string/join nil '())`, code: "TypeError", msg: "string/join: separator must be string"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
	}
	if len(c.calls) != 0 {
		t.Errorf("nil and scalar sequences must not reach the callback, got %d calls", len(c.calls))
	}
}

func TestNilSequence_HigherOrderNoCallback(t *testing.T) {
	emptyList := core.NewList(nil)

	t.Run("zero callbacks", func(t *testing.T) {
		rows := []struct {
			name, input string
			want        core.Value
		}{
			{"map", `(map count2 nil)`, emptyList},
			{"filter", `(filter count2 nil)`, emptyList},
			{"reduce no init", `(reduce count2 nil)`, core.Nil{}},
			{"reduce init", `(reduce count2 :init nil)`, core.Keyword{V: "init"}},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				env, c := newCallCounterEnv(t)
				got := eval(t, env, row.input)
				if !row.want.Equals(got) {
					t.Errorf("%s: expected %v, got %v", row.input, row.want, got)
				}
				if len(c.calls) != 0 {
					t.Errorf("%s: expected 0 callbacks, got %d", row.input, len(c.calls))
				}
				if c.badArity != nil {
					t.Errorf("%s: %v", row.input, c.badArity)
				}
			})
		}
	})

	t.Run("apply keeps the prefix and drops the nil tail", func(t *testing.T) {
		env, c := newCallCounterEnv(t)
		got := eval(t, env, `(apply count2 1 nil)`)
		if !(core.Int{V: 0}).Equals(got) {
			t.Errorf("expected 0, got %v", got)
		}
		if len(c.calls) != 1 || !(core.Int{V: 1}).Equals(c.calls[0]) {
			t.Fatalf("expected count2 called once with 1, got %v", c.calls)
		}
		if c.badArity != nil {
			t.Errorf("unexpected arity failure: %v", c.badArity)
		}
	})

	t.Run("apply nil tail reaches the target with zero args", func(t *testing.T) {
		env, c := newCallCounterEnv(t)
		err := evalErr(t, env, `(apply count2 nil)`)
		if c.badArity == nil {
			t.Fatalf("expected count2 to be invoked with zero args and reject them, got err %v", err)
		}
		if err == nil {
			t.Errorf("expected the target's arity error to surface, got nil")
		}
		if len(c.calls) != 0 {
			t.Errorf("expected no accepted calls, got %v", c.calls)
		}
	})

	t.Run("apply non-final nil stays a plain value", func(t *testing.T) {
		env, c := newCallCounterEnv(t)
		err := evalErr(t, env, `(apply count2 nil '(1))`)
		if c.badArity == nil {
			t.Fatalf("expected count2 to receive (nil 1) and reject the arity, got err %v", err)
		}
		if err == nil {
			t.Errorf("expected the target's arity error to surface, got nil")
		}
		if len(c.calls) != 0 {
			t.Errorf("expected no accepted calls, got %v", c.calls)
		}
	})
}

func TestStringJoin_NilCollection(t *testing.T) {
	env := setupEnv(t)
	got := eval(t, env, `(string/join "," nil)`)
	if !(core.String{V: ""}).Equals(got) {
		t.Errorf(`(string/join "," nil): expected "", got %v`, got)
	}
	if _, ok := got.(core.String); !ok {
		t.Errorf(`(string/join "," nil): expected core.String, got %T`, got)
	}
	wantTypedError(t, evalErr(t, env, `(string/join nil nil)`), "TypeError", "string/join: separator must be string")
}

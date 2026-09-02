package stdlib

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// wantTypedError asserts the classified Code alongside the diagnostic text.
// Error() no longer reports that text verbatim: typed construction prefixes
// the Code, so Message is where the pre-migration wording now lives.
func wantTypedError(t *testing.T, err error, code, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", msg)
	}
	var le *core.LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *core.LispicoError, got %T: %v", err, err)
	}
	if le.Code != code {
		t.Errorf("expected code %q, got %q", code, le.Code)
	}
	if le.Message != msg {
		t.Errorf("expected message %q, got %q", msg, le.Message)
	}
}

func TestNth_Characterization(t *testing.T) {
	env := setupEnv(t)

	t.Run("values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected core.Value
		}{
			{"list middle", `(nth '(1 2 3) 1)`, core.Int{V: 2}},
			{"vector first", `(nth [1 2] 0)`, core.Int{V: 1}},
			{"default past end", `(nth '(1 2 3) 10 :nf)`, core.Keyword{V: "nf"}},
			{"nil default", `(nth nil 0 :nf)`, core.Keyword{V: "nf"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := eval(t, env, tt.input)
				if !tt.expected.Equals(got) {
					t.Errorf("%s: expected %v, got %v", tt.input, tt.expected, got)
				}
			})
		}
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
			code  string
			want  string
		}{
			{"zero args", "(nth)", "ArityError", "nth: requires 2 or 3 arguments"},
			{"four args", "(nth '(1) 0 2 3)", "ArityError", "nth: requires 2 or 3 arguments"},
			{"float index", `(nth '(1 2) 1.5)`, "TypeError", "nth: index must be integer"},
			{"past end", "(nth '(1 2) 5)", "EvalError", "nth: index out of bounds"},
			{"negative index", "(nth '(1 2) -1)", "EvalError", "nth: index out of bounds"},
			{"int subject", "(nth 5 0)", "TypeError", "nth: expected collection, got core.Int"},
			{"nil subject", "(nth nil 0)", "EvalError", "nth: index out of bounds"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				wantTypedError(t, evalErr(t, env, tt.input), tt.code, tt.want)
			})
		}
	})
}

func TestMap_Characterization(t *testing.T) {
	type counter struct {
		calls    []core.Value
		badArity error
	}
	newCounterEnv := func(t *testing.T) (*core.Env, *counter) {
		t.Helper()
		env := setupEnv(t)
		c := &counter{}
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

	t.Run("list values in callback order", func(t *testing.T) {
		env, c := newCounterEnv(t)
		got := eval(t, env, `(map count2 (list 10 20 30))`)
		expected := core.NewList([]core.Value{core.Int{V: 0}, core.Int{V: 1}, core.Int{V: 2}})
		if !expected.Equals(got) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
		if _, ok := got.(core.List); !ok {
			t.Fatalf("expected core.List result, got %T", got)
		}
		if len(c.calls) != 3 {
			t.Fatalf("expected 3 callbacks, got %d", len(c.calls))
		}
		if c.badArity != nil {
			t.Fatalf("callback arity: %v", c.badArity)
		}
		for i, arg := range c.calls {
			if !(core.Int{V: int64(10 + 10*i)}).Equals(arg) {
				t.Errorf("callback %d received %v, want %d", i, arg, 10+10*i)
			}
		}
	})

	t.Run("vector input list result", func(t *testing.T) {
		env, c := newCounterEnv(t)
		got := eval(t, env, `(map count2 [1 2 3])`)
		expected := core.NewList([]core.Value{core.Int{V: 0}, core.Int{V: 1}, core.Int{V: 2}})
		if !expected.Equals(got) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
		if _, ok := got.(core.List); !ok {
			t.Fatalf("expected core.List result for vector input, got %T", got)
		}
		if len(c.calls) != 3 {
			t.Fatalf("expected 3 callbacks, got %d", len(c.calls))
		}
		if c.badArity != nil {
			t.Fatalf("callback arity: %v", c.badArity)
		}
	})

	t.Run("arity errors", func(t *testing.T) {
		env, _ := newCounterEnv(t)
		tests := []struct {
			name  string
			input string
			code  string
			want  string
		}{
			{"zero args", "(map count2)", "ArityError", "map: requires 2 arguments"},
			{"three args", "(map count2 (list 1) 2)", "ArityError", "map: requires 2 arguments"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				wantTypedError(t, evalErr(t, env, tt.input), tt.code, tt.want)
			})
		}
	})

	t.Run("collection errors", func(t *testing.T) {
		env, _ := newCounterEnv(t)
		tests := []struct {
			name  string
			input string
			code  string
			want  string
		}{
			{"nil second", "(map count2 nil)", "TypeError", "map: second argument must be collection"},
			{"int second", "(map count2 5)", "TypeError", "map: second argument must be collection"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				wantTypedError(t, evalErr(t, env, tt.input), tt.code, tt.want)
			})
		}
	})
}

func TestSort_Characterization(t *testing.T) {
	env := setupEnv(t)

	t.Run("natural order", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{"ints", "(sort [3 1 2])", "(1 2 3)"},
			{"list input", "(sort (list 3 1 2))", "(1 2 3)"},
			{"mixed numbers", "(sort [2.5 1 3])", "(1 2.5 3)"},
			{"strings", `(sort ["b" "a" "c"])`, `("a" "b" "c")`},
			{"keywords", "(sort [:c :a :b])", "(:a :b :c)"},
			{"empty vector", "(sort [])", "()"},
			{"nil", "(sort nil)", "()"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := eval(t, env, tt.input)
				if got.String() != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, got.String())
				}
			})
		}
	})

	t.Run("always list result", func(t *testing.T) {
		got := eval(t, env, "(sort [3 1 2])")
		if _, ok := got.(core.List); !ok {
			t.Fatalf("expected core.List result for vector input, got %T", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
			code  string
			want  string
		}{
			{"zero args", "(sort)", "ArityError", "sort: requires 1 argument"},
			{"two args", "(sort [1] [2])", "ArityError", "sort: requires 1 argument"},
			{"int subject", "(sort 5)", "TypeError", "sort: expected collection, got core.Int"},
			{"keyword subject", "(sort :k)", "TypeError", "sort: expected collection, got core.Keyword"},
			{"mixed kinds", `(sort [1 "a"])`, "EvalError", "sort: cannot compare core.String with core.Int"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				wantTypedError(t, evalErr(t, env, tt.input), tt.code, tt.want)
			})
		}
	})
}

// Ceilings are the pre-extraction measured baselines (nth 3, map 12, sort 8
// allocs/op) plus 2 for the one *BuiltinWorkBudget and one aux slice the
// extraction may add. Reduction totals are intentionally not pinned.
func TestCollections_CharacterizationAllocs(t *testing.T) {
	env := setupEnv(t)
	if err := env.Set("count2", core.GoFunc{
		Name: "count2",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return core.Nil{}, fmt.Errorf("count2: got %d arguments, want 1", len(args))
			}
			return args[0], nil
		},
	}); err != nil {
		t.Fatalf("bind count2: %v", err)
	}
	ev := core.NewEvaluator()

	measure := func(code string, ceiling int) {
		t.Helper()
		forms, err := core.Read(code)
		if err != nil {
			t.Fatalf("read %s: %v", code, err)
		}
		if _, err := ev.Eval(context.Background(), forms[0], env); err != nil {
			t.Fatalf("eval %s: %v", code, err)
		}
		got := testing.AllocsPerRun(500, func() {
			if _, err := ev.Eval(context.Background(), forms[0], env); err != nil {
				panic(err)
			}
		})
		if got > float64(ceiling) {
			t.Errorf("%s: %v allocs/op exceeds ceiling %d (pre-extraction baseline %d + 2)", code, got, ceiling, ceiling-2)
		}
	}

	measure(`(nth '(1 2 3) 1)`, 5)
	measure(`(map count2 (list 1 2 3))`, 14)
	measure(`(sort '(3 1 2))`, 10)
}

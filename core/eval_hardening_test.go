package core

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestEval_VectorEvaluatesElements(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 99})
	got := evalStr(t, env, "[1 x]")
	want := Vector{Items: []Value{Int{V: 1}, Int{V: 99}}}
	if !got.Equals(want) {
		t.Fatalf("[1 x] = %s, want %s", got.String(), want.String())
	}
}

func TestEval_MapEvaluatesElements(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 99})
	got := evalStr(t, env, "{:a x}")
	want, _ := NewHashMap().Assoc(Keyword{V: "a"}, Int{V: 99})
	if !got.Equals(want) {
		t.Fatalf("{:a x} = %s, want %s", got.String(), want.String())
	}
}

func TestEval_QuasiquoteMap(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 99})
	got := evalStr(t, env, "`{:a ~x}")
	want, _ := NewHashMap().Assoc(Keyword{V: "a"}, Int{V: 99})
	if !got.Equals(want) {
		t.Fatalf("`{:a ~x} = %s, want %s", got.String(), want.String())
	}
}

func TestEval_ErrorIsTyped(t *testing.T) {
	t.Parallel()
	cases := []string{"(def)", "(if)", "(let* [a])", "foo"}
	for _, src := range cases {
		err := evalStrErr(newTestEnv(), src)
		if err == nil {
			t.Fatalf("%s: expected error", src)
		}
		var le *LispicoError
		if !errors.As(err, &le) {
			t.Fatalf("%s: error %v is not *LispicoError", src, err)
		}
		if le.Code == "" {
			t.Fatalf("%s: LispicoError has empty Code", src)
		}
	}
}

// readForm reads exactly one form for the concurrency tests.
func readForm(t *testing.T, src string) Value {
	t.Helper()
	forms, err := Read(src)
	if err != nil || len(forms) != 1 {
		t.Fatalf("Read(%q): %v (n=%d)", src, err, len(forms))
	}
	return forms[0]
}

func TestConcurrent_MacroExpansionNoRace(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	e := NewEvaluator()
	if _, err := e.Eval(context.Background(), readForm(t, "(defmacro m [] (quote 42))"), env); err != nil {
		t.Fatalf("defmacro: %v", err)
	}
	use := readForm(t, "(m)")

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				got, err := e.Eval(context.Background(), use, env)
				if err != nil || !got.Equals(Int{V: 42}) {
					t.Errorf("(m) = %v, err %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_RecurIsolation(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	e := NewEvaluator()

	// Recurs once (true → false) then returns, so loopDepth rises and falls
	// repeatedly on the shared engine. Uses only core forms.
	loopForm := readForm(t, "(loop [go true] (if go (recur false) :done))")
	strayRecur := readForm(t, "(recur 1)")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = e.Eval(context.Background(), loopForm, env)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 4000 {
			_, err := e.Eval(context.Background(), strayRecur, env)
			if err == nil {
				t.Errorf("bare (recur 1) succeeded; recur state leaked across evaluations")
				break
			}
		}
		close(stop)
	}()

	wg.Wait()
}

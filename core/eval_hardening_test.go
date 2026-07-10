package core

import (
	"context"
	"errors"
	"strings"
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

func TestDetachEvalState_AttachesFreshState(t *testing.T) {
	t.Parallel()

	type marker struct{}
	parent := context.WithValue(context.Background(), marker{}, "preserved")

	detached := DetachEvalState(parent)
	if v := detached.Value(marker{}); v != "preserved" {
		t.Fatalf("custom value not preserved: %v", v)
	}

	state, ok := detached.Value(evalStateKey{}).(*evalState)
	if !ok || state == nil {
		t.Fatalf("detached ctx missing evalState: ok=%v", ok)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := DetachEvalState(cancelled).Err(); err == nil {
		t.Fatalf("cancellation not preserved through detach")
	}

	sharedState := &evalState{}
	shared := context.WithValue(context.Background(), evalStateKey{}, sharedState)
	detachedFromShared := DetachEvalState(shared)
	if detachedFromShared.Value(evalStateKey{}) == sharedState {
		t.Fatalf("detached ctx reuses parent evalState")
	}
}

func TestDetachEvalState_ApplyIsolatesCallDepth(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	e := NewEvaluator()

	if _, err := e.Eval(context.Background(), readForm(t, "(defn deep [n] (if n (deep false) n))"), env); err != nil {
		t.Fatalf("defn deep: %v", err)
	}
	deepVal, ok := env.Get("deep")
	if !ok {
		t.Fatalf("deep not bound in env")
	}

	sharedState := &evalState{}
	parent := context.WithValue(context.Background(), evalStateKey{}, sharedState)
	detached := DetachEvalState(parent)

	var wg sync.WaitGroup
	for i, ctx := range []context.Context{parent, detached} {
		wg.Add(1)
		go func(i int, ctx context.Context) {
			defer wg.Done()
			for range 200 {
				got, err := e.Apply(ctx, deepVal, []Value{Bool{V: true}}, env)
				if err != nil {
					t.Errorf("goroutine %d Apply: %v", i, err)
					return
				}
				if !got.Equals(Bool{V: false}) {
					t.Errorf("goroutine %d got %s, want false", i, got)
					return
				}
			}
		}(i, ctx)
	}
	wg.Wait()
}

func TestEval_StructuralDepthVectorExceeded(t *testing.T) {
	t.Parallel()
	e := NewEvaluator()
	e.MaxStructuralDepth = 50

	n := 200
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	// Parse with reader ceiling ABOVE eval default so reader does not reject first
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("reader rejected depth %d with maxDepth=5000: %v", n, err)
	}
	env := newTestEnv()
	_, err = e.Eval(context.Background(), forms[0], env)
	if err == nil {
		t.Fatal("expected structural depth error")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestEval_StructuralDepthVectorUnderLimitOK(t *testing.T) {
	t.Parallel()
	e := NewEvaluator()
	e.MaxStructuralDepth = 2048

	n := 200
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("reader rejected depth %d: %v", n, err)
	}
	env := newTestEnv()
	_, err = e.Eval(context.Background(), forms[0], env)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestEval_StructuralDepthHashMapExceeded(t *testing.T) {
	t.Parallel()
	e := NewEvaluator()
	e.MaxStructuralDepth = 50

	var build func(depth int) Value
	build = func(depth int) Value {
		if depth == 0 {
			return Keyword{V: "done"}
		}
		m := NewHashMap()
		m.Set(Keyword{V: "k"}, build(depth-1))
		return m
	}
	deep := build(200)

	env := newTestEnv()
	_, err := e.Eval(context.Background(), deep, env)
	if err == nil {
		t.Fatal("expected structural depth error from deep HashMap literal")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestEval_StructuralDepthQuasiquoteExceeded(t *testing.T) {
	t.Parallel()
	e := NewEvaluator()
	e.MaxStructuralDepth = 50

	env := newTestEnv()

	// Build a quasiquoted form with 200 levels of list nesting.
	// (quasiquote ( ... (quasiquote (x)) ... ))
	var build func(depth int) string
	build = func(depth int) string {
		if depth == 0 {
			return "x"
		}
		return "(quasiquote " + build(depth-1) + ")"
	}
	src := build(100)
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("reader rejected deep quasiquote source: %v", err)
	}
	_, err = e.Eval(context.Background(), forms[0], env)
	if err == nil {
		t.Fatal("expected structural depth error from deep quasiquote")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestEval_StructuralDepthDirectEvaluatorEnforces(t *testing.T) {
	t.Parallel()
	// Parse with reader ceiling ABOVE eval default so reader does NOT reject first
	n := 2000
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("reader rejected depth %d with maxDepth=5000: %v", n, err)
	}

	e := NewEvaluator()
	// Default MaxStructuralDepth is 1024, so depth 2000 should exceed it
	env := newTestEnv()
	_, err = e.Eval(context.Background(), forms[0], env)
	if err == nil {
		t.Fatal("expected structural depth error: default MaxStructuralDepth=1024 < depth 2000")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestConcurrent_StructuralDepthIsolation(t *testing.T) {
	// DISCRIMINATING isolation proof: each goroutine evaluates a structure whose
	// depth (150) is UNDER the limit (200), so each succeeds on its own. If the
	// structDepth counter were a shared engine field instead of per-call evalState,
	// two concurrent evaluations would combine past 200 and fail. Asserting all
	// succeed proves per-call isolation; run under -race to catch any data race.
	const depth, limit, workers = 150, 200, 8
	src := strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth)
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}

	e := NewEvaluator()
	e.MaxStructuralDepth = limit

	env := newTestEnv()
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine uses its own context → its own evalState. Run several
			// iterations to maximize temporal overlap with the other goroutines.
			for range 20 {
				ctx := context.Background()
				if _, err := e.Eval(ctx, forms[0], env); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("under-limit concurrent evaluation must succeed in isolation; got shared-counter leak: %v", err)
		}
	}
}

func TestEval_QuasiquoteAgreementPin(t *testing.T) {
	t.Parallel()
	// With MaxStructuralDepth=1 in tree-walker:
	// - (quasiquote a) succeeds (atom → depth 0)
	// - (quasiquote ((a))) fails (list depth 2 > 1)
	e := NewEvaluator()
	e.MaxStructuralDepth = 1

	env := newTestEnv()

	// (quasiquote a) — atom, no struct depth increment
	val, err := e.Eval(context.Background(), List{Items: []Value{Symbol{V: "quasiquote"}, Symbol{V: "a"}}}, env)
	if err != nil {
		t.Fatalf("(quasiquote a) should succeed with MaxStructuralDepth=1: %v", err)
	}
	if !val.Equals(Symbol{V: "a"}) {
		t.Fatalf("(quasiquote a) = %s, want a", val.String())
	}

	// (quasiquote ((a))) — list/list/atom → depth 2
	inner := List{Items: []Value{Symbol{V: "a"}}}
	mid := List{Items: []Value{inner}}
	_, err = e.Eval(context.Background(), List{Items: []Value{Symbol{V: "quasiquote"}, mid}}, env)
	if err == nil {
		t.Fatal("(quasiquote ((a))) should error with MaxStructuralDepth=1")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

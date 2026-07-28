package cl_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

func newEngine(t *testing.T, opts ...runtime.EngineOption) runtime.Engine {
	t.Helper()
	options := append([]runtime.EngineOption{runtime.WithDialect(cl.Dialect())}, opts...)
	e, err := runtime.New(nil, options...)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })

	require.NoError(t, e.Use(stdlib.New()))
	return e
}

// TestCL_IsNotIdentity asserts that the CL dialect is non-identity because of
// its non-default axes (Lisp-2, CL reader flags).
func TestCL_IsNotIdentity(t *testing.T) {
	assert.False(t, cl.Dialect().IsIdentity(), "CL dialect must be non-identity")
}

// TestCL_Vocab_DrivesDialect exercises the spec scenarios from the dialect spec.
func TestCL_Vocab_DrivesDialect(t *testing.T) {
	e := newEngine(t)

	t.Run("defun defines a function", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(defun f (x) x)")
		require.NoError(t, err)
		require.True(t, core.Keyword{V: "fn"}.Equals(got.Type()), "defun must return :fn, got %s", got.Type())

		got, err = e.Eval(context.Background(), "cl", "(f 42)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 42}.Equals(got), "defun call should work, got %v", got)
	})

	t.Run("false is falsy", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(if false :y :n)")
		require.NoError(t, err)
		assert.True(t, core.Keyword{V: "n"}.Equals(got), "false is falsy, got %v", got)
	})

	t.Run("funcall applies a function", func(t *testing.T) {
		_, err := e.Eval(context.Background(), "cl", "(defun f (x) x)")
		require.NoError(t, err)
		got, err := e.Eval(context.Background(), "cl", "(progn (funcall #'f 42))")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 42}.Equals(got), "funcall #'f: got %v", got)
	})

	t.Run("#'f parses", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(quote #'f)")
		require.NoError(t, err)
		want := core.NewList([]core.Value{core.Symbol{V: "function"}, core.Symbol{V: "f"}})
		assert.True(t, want.Equals(got), "#'f must read as (function f), got %v", got)
	})

	t.Run("#(1 2) parses as a vector", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(quote #(1 2))")
		require.NoError(t, err)
		want := core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
		assert.True(t, want.Equals(got), "#(1 2) must read as a vector, got %v", got)
	})

	t.Run("[1 2] produces a parse error", func(t *testing.T) {
		_, err := e.Eval(context.Background(), "cl", "(quote [1 2])")
		require.Error(t, err, "[..] must fail to read as a vector literal under CL")
	})
}

func TestCL_ListBindingForms(t *testing.T) {
	modes := []struct {
		name string
		opts []runtime.EngineOption
	}{
		{name: "tree-walker", opts: []runtime.EngineOption{runtime.WithTreeWalker()}},
		{name: "vm", opts: []runtime.EngineOption{runtime.WithBytecode()}},
	}
	tests := []struct {
		name string
		src  string
		want core.Value
	}{
		{name: "let", src: "(let ((a 1) (b 2)) (+ a b))", want: core.Int{V: 3}},
		{name: "let*", src: "(let* ((a 1) (b (+ a 2))) b)", want: core.Int{V: 3}},
		{name: "loop recur", src: "(loop ((i 0)) (if (< i 3) (recur (+ i 1)) i))", want: core.Int{V: 3}},
		{name: "nested let", src: "(let ((a (let ((b 1)) b)) (c 2)) (+ a c))", want: core.Int{V: 3}},
		{name: "let* shadowing", src: "(let* ((x 1) (x (+ x 1))) x)", want: core.Int{V: 2}},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			e := newEngine(t, mode.opts...)
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					got, err := e.Eval(context.Background(), "cl-bindings", tc.src)
					require.NoError(t, err)
					require.True(t, tc.want.Equals(got), "%s => %v, want %v", tc.src, got, tc.want)
				})
			}
		})
	}
}

// TestCL_VocabMap asserts that the CL vocabulary renames work.
func TestCL_VocabMap(t *testing.T) {
	e := newEngine(t)

	t.Run("car reads first element", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(car '(1 2 3))")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 1}.Equals(got), "car: got %v", got)
	})

	t.Run("cdr reads rest", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(cdr '(1 2 3))")
		require.NoError(t, err)
		want := core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 3}})
		assert.True(t, want.Equals(got), "cdr: got %v", got)
	})

	t.Run("null checks nil", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(null nil)")
		require.NoError(t, err)
		assert.True(t, core.Bool{V: true}.Equals(got), "null of nil: got %v", got)
	})
}

// TestCL_SpecScenario_SurfaceForms evaluates the exact scenario from the spec.
func TestCL_SpecScenario_SurfaceForms(t *testing.T) {
	e := newEngine(t)

	// "defun SHALL define a function"
	got, err := e.Eval(context.Background(), "cl", "(defun square (x) (* x x))")
	require.NoError(t, err)
	t.Logf("defun square => %s", got)

	// "(if false :y :n)" evaluates to :n because false is falsy.
	got, err = e.Eval(context.Background(), "cl", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "n"}.Equals(got), "(if false :y :n) => %v", got)

	// "(funcall #'f args...) SHALL apply f"
	_, err = e.Eval(context.Background(), "cl", "(defun id (x) x)")
	require.NoError(t, err)
	got, err = e.Eval(context.Background(), "cl", "(funcall #'id 7)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "funcall #'id => %v", got)
}

func TestCL_ConditionalTruthiness_Evaluators(t *testing.T) {
	modes := []struct {
		name string
		opts []runtime.EngineOption
	}{
		{name: "tree-walker", opts: []runtime.EngineOption{runtime.WithTreeWalker()}},
		{name: "vm", opts: []runtime.EngineOption{runtime.WithBytecode()}},
	}
	cases := []struct {
		name string
		src  string
		want core.Value
	}{
		{name: "if predicate false", src: "(if (= 1 2) :a :b)", want: core.Keyword{V: "b"}},
		{name: "if false", src: "(if false :a :b)", want: core.Keyword{V: "b"}},
		{name: "when predicate false", src: "(when (= 1 2) :x)", want: core.Nil{}},
		{name: "cond predicate false", src: "(cond ((= 1 2) :a) (:else :b))", want: core.Keyword{V: "b"}},
		{name: "and predicate false", src: "(and (= 1 2) :x)", want: core.Bool{V: false}},
		{name: "or predicate false", src: "(or (= 1 2) :y)", want: core.Keyword{V: "y"}},
		{name: "not predicate false", src: "(not (= 1 2))", want: core.Bool{V: true}},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			e := newEngine(t, mode.opts...)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					got, err := e.Eval(context.Background(), "cl-truthiness", tc.src)
					require.NoError(t, err)
					require.True(t, tc.want.Equals(got), "%s => %v, want %v", tc.src, got, tc.want)
				})
			}
		})
	}
}

// TestCL_SpecScenario_ReaderAffordances exercises the reader spec scenario.
func TestCL_SpecScenario_ReaderAffordances(t *testing.T) {
	e := newEngine(t)

	// #'f SHALL parse
	got, err := e.Eval(context.Background(), "cl", "(quote #'f)")
	require.NoError(t, err)
	want := core.NewList([]core.Value{core.Symbol{V: "function"}, core.Symbol{V: "f"}})
	assert.True(t, want.Equals(got), "#'f => (function f)")

	// #(...) SHALL parse
	got, err = e.Eval(context.Background(), "cl", "(quote #(1 2 3))")
	require.NoError(t, err)
	wantVec := core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})
	assert.True(t, wantVec.Equals(got), "#(1 2 3) => vector")

	// [1 2] SHALL NOT read as a vector literal
	_, err = e.Eval(context.Background(), "cl", "(quote [1 2])")
	require.Error(t, err, "[1 2] must fail under CL")
}

// TestCL_Dialect_Memoized asserts that repeated cl.Dialect() calls are
// stable and that Fingerprint() on the memoized value skips the SHA-256 hash
// work an uncached Dialect repeats on every call. Allocation count, not
// wall-clock, is the observation mechanism: a cache hit returns the
// already-hashed string, while an uncached Fingerprint() allocates a new
// hash.Hash and formats its inputs (including sorting the vocabulary keys)
// every time.
func TestCL_Dialect_Memoized(t *testing.T) {
	memoized := cl.Dialect()
	uncached := core.FullDialect().
		Lisp2().
		WithoutBracketLiterals().
		WithFunctionRef().
		WithReaderVector().
		Add("defun", "defn").
		Rename("set!", "setq").
		Rename("do", "progn").
		Vocabulary(map[string]string{
			"car":     "first",
			"cdr":     "rest",
			"null":    "nil?",
			"cons":    "cons",
			"list":    "list",
			"append":  "concat",
			"length":  "count",
			"reverse": "reverse",
			"nth":     "nth",
			"sort":    "sort",
			"mapcar":  "map",
			"apply":   "apply",
			"type":    "type",
		})
	require.Equal(t, memoized.Fingerprint(), uncached.Fingerprint(), "memoized and uncached Fingerprint() must agree")
	assert.Equal(t, cl.Dialect().Fingerprint(), memoized.Fingerprint(), "repeated cl.Dialect() calls must produce the same fingerprint")

	memoizedAllocs := testing.AllocsPerRun(50, func() {
		_ = memoized.Fingerprint()
	})
	uncachedAllocs := testing.AllocsPerRun(50, func() {
		_ = uncached.Fingerprint()
	})

	t.Logf("memoized Fingerprint(): %.1f allocs/op, uncached Fingerprint(): %.1f allocs/op", memoizedAllocs, uncachedAllocs)
	assert.Less(t, memoizedAllocs, uncachedAllocs, "Fingerprint() on a memoized Dialect must not redo the SHA-256 hash work")
}

// TestCL_ConcurrentEnginesCorpusParity builds engines from cl.Dialect()
// concurrently and re-runs the existing vocab/surface-form assertions on
// each, proving the process-memoized dialect behaves identically to
// independent construction under concurrent use.
func TestCL_ConcurrentEnginesCorpusParity(t *testing.T) {
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := runtime.New(nil, runtime.WithDialect(cl.Dialect()))
			if err != nil {
				errs <- err
				return
			}
			defer e.Close()
			if err := e.Use(stdlib.New()); err != nil {
				errs <- err
				return
			}

			ctx := context.Background()

			got, err := e.Eval(ctx, "corpus", "(defun f (x) x)")
			if err != nil {
				errs <- err
				return
			}
			if !(core.Keyword{V: "fn"}).Equals(got.Type()) {
				errs <- fmt.Errorf("defun return type = %s", got.Type())
				return
			}

			got, err = e.Eval(ctx, "corpus", "(f 42)")
			if err != nil {
				errs <- err
				return
			}
			if !(core.Int{V: 42}).Equals(got) {
				errs <- fmt.Errorf("(f 42) = %v", got)
				return
			}

			got, err = e.Eval(ctx, "corpus", "(car '(1 2 3))")
			if err != nil {
				errs <- err
				return
			}
			if !(core.Int{V: 1}).Equals(got) {
				errs <- fmt.Errorf("car = %v", got)
				return
			}

			got, err = e.Eval(ctx, "corpus", "(if false :y :n)")
			if err != nil {
				errs <- err
				return
			}
			if !(core.Keyword{V: "n"}).Equals(got) {
				errs <- fmt.Errorf("(if false :y :n) = %v", got)
				return
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

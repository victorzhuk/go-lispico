package runtime

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// All tests pinned to Clojure dialect; the default flips to Common Lisp in shard-C.

func TestBytecodeRuntime_SequentialIsolation(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()

	one, err := eng.Eval(ctx, "one", "1")
	require.NoError(t, err)
	assert.True(t, one.Equals(core.Int{V: 1}), "first eval returns 1, got %v", one)

	two, err := eng.Eval(ctx, "two", "2")
	require.NoError(t, err)
	assert.True(t, two.Equals(core.Int{V: 2}), "second eval returns 2, got %v", two)
}

func TestBytecodeRuntime_Concurrent_Race(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	workers := runtime.NumCPU() * 2
	iterations := 50

	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				result, err := eng.Eval(ctx, "race", "42")
				if err != nil {
					t.Errorf("worker %d iteration %d: eval error: %v", id, j, err)
					return
				}
				if !result.Equals(core.Int{V: 42}) {
					t.Errorf("worker %d iteration %d: expected 42, got %v", id, j, result)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestBytecodeRuntime_EvalWithBindings(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	bindings := map[string]core.Value{"x": core.Int{V: 7}}
	result, err := eng.EvalWithBindings(ctx, "x", bindings)
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 7}), "binding visible under bytecode, got %v", result)
}

func TestBytecodeRuntime_Call(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()

	_, err = eng.Eval(ctx, "def", "(def add (fn [a b] a))")
	require.NoError(t, err)

	result, err := eng.Call(ctx, "add", core.Int{V: 2}, core.Int{V: 3})
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 2}), "call result, got %v", result)
}

func TestBytecodeRuntime_LoopRecur(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	bindBuiltin(t, eng, "+")
	bindBuiltin(t, eng, "=")

	ctx := context.Background()
	result, err := eng.Eval(ctx, "sum", "(loop [i 0 acc 0] (if (= i 10) acc (recur (+ i 1) (+ acc i))))")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 45}), "loop sum, got %v", result)
}

func TestBytecodeRuntime_TryCatch(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	result, err := eng.Eval(ctx, "try", "(try (throw \"boom\") (catch e e))")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.String{V: "boom"}), "catch value, got %v", result)
}

func TestBytecodeRuntime_Variadic(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	result, err := eng.Eval(ctx, "variadic", "((fn [a & rest] rest) 1 2 3)")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}}}
	assert.True(t, result.Equals(expected), "variadic rest, got %v", result)
}

func TestBytecodeRuntime_LetStar(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	bindBuiltin(t, eng, "+")

	ctx := context.Background()
	result, err := eng.Eval(ctx, "let*", "(let* [x 1 y (+ x 1)] y)")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 2}), "let* result, got %v", result)
}

func TestBytecodeRuntime_Macro(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	_, err = eng.Eval(ctx, "macro", "(defmacro unless [cond & body] `(if (not ~cond) (do ~@body) nil))")
	require.NoError(t, err)
	result, err := eng.Eval(ctx, "use", "(unless false 42)")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 42}), "macro expansion result, got %v", result)
}

func TestBytecodeRuntime_NestedDefmacroFallsBackToTreeWalker(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	result, err := eng.Eval(ctx, "nested-macro", "(do (defmacro id [x] x) (id 42))")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 42}), "nested defmacro deferred to tree-walker, got %v", result)
}

func TestBytecodeRuntime_ThrowNonString(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	result, err := eng.Eval(ctx, "throw-int", "(try (throw 42) (catch e e))")
	require.NoError(t, err)
	assert.True(t, result.Equals(core.String{V: "42"}), "non-string throw coerced to string, got %v", result)
}

func TestBytecodeRuntime_EmptyBodyFn(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	_, err = eng.Eval(ctx, "empty-fn", "((fn []))")
	require.Error(t, err, "empty-body fn should error, not panic")
}

func TestBytecodeRuntime_EmptyBodyDefn(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()
	_, err = eng.Eval(ctx, "empty-defn", "(defn f [])")
	require.Error(t, err, "empty-body defn should error, not panic")
}

func TestBytecodeRuntime_WhenUnlessValuePosition(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := context.Background()

	cases := []struct {
		name string
		src  string
		want core.Value
	}{
		{"when false in let", "(let [x (when false 1)] x)", core.Nil{}},
		{"when false in do", "(do (when false 1))", core.Nil{}},
		{"when false in fn body", "((fn [] (when false 1)))", core.Nil{}},
		{"unless true in let", "(let [x (unless true 1)] x)", core.Nil{}},
		{"unless true in do", "(do (unless true 1))", core.Nil{}},
		{"unless true in fn body", "((fn [] (unless true 1)))", core.Nil{}},
		{"when true yields body", "(let [x (when true 7)] x)", core.Int{V: 7}},
		{"unless false yields body", "(let [x (unless false 7)] x)", core.Int{V: 7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := eng.Eval(ctx, tc.name, tc.src)
			require.NoError(t, err)
			assert.True(t, got.Equals(tc.want), "got %v (%T), want %v (%T)", got, got, tc.want, tc.want)
		})
	}
}

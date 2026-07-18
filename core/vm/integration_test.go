package vm_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func newTestEnv() *core.Env {
	env := core.NewEnv(nil)
	env.Set("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			var sum int64
			for _, a := range args {
				sum += a.(core.Int).V
			}
			return core.Int{V: sum}, nil
		},
	})
	env.Set("-", core.GoFunc{
		Name: "-",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return core.Int{V: 0}, nil
			}
			result := args[0].(core.Int).V
			for _, a := range args[1:] {
				result -= a.(core.Int).V
			}
			return core.Int{V: result}, nil
		},
	})
	env.Set("*", core.GoFunc{
		Name: "*",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			result := int64(1)
			for _, a := range args {
				result *= a.(core.Int).V
			}
			return core.Int{V: result}, nil
		},
	})
	env.Set("<", core.GoFunc{
		Name: "<",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
		},
	})
	env.Set("=", core.GoFunc{
		Name: "=",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return core.Bool{V: true}, nil
			}
			for i := 1; i < len(args); i++ {
				if !args[i].Equals(args[0]) {
					return core.Bool{V: false}, nil
				}
			}
			return core.Bool{V: true}, nil
		},
	})
	return env
}

func compileAndRun(t *testing.T, env *core.Env, src string) core.Value {
	t.Helper()
	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(env)
	var result core.Value = core.Nil{}
	for _, chunk := range chunks {
		result, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "run")
	}
	return result
}

func TestCompileAndRunArithmetic(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	result := compileAndRun(t, env, "(+ 1 2)")
	assert.True(t, result.Equals(core.Int{V: 3}), "expected 3, got %v", result)
}

func TestCompileAndRunArithmetic_Nested(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	result := compileAndRun(t, env, "(+ (* 2 3) (- 10 5))")
	assert.True(t, result.Equals(core.Int{V: 11}), "expected 11, got %v", result)
}

func TestCompileAndRunDef(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	compileAndRun(t, env, "(def x 42)")
	val, ok := env.Get("x")
	require.True(t, ok, "x should be defined")
	assert.True(t, val.Equals(core.Int{V: 42}), "expected x=42, got %v", val)
}

func TestCompileAndRunIf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		expected core.Value
	}{
		{"true branch", "(if true 1 2)", core.Int{V: 1}},
		{"false branch", "(if false 1 2)", core.Int{V: 2}},
		{"nil branch", "(if nil 1 2)", core.Int{V: 2}},
		{"no else true", "(if true 42)", core.Int{V: 42}},
		{"no else false", "(if false 42)", core.Nil{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv()
			result := compileAndRun(t, env, tt.src)
			assert.True(t, result.Equals(tt.expected), "expected %v, got %v", tt.expected, result)
		})
	}
}

func TestCompileAndRunLet(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	result := compileAndRun(t, env, "(let [x 10 y 20] (+ x y))")
	assert.True(t, result.Equals(core.Int{V: 30}), "expected 30, got %v", result)
}

func TestCompileAndRunDo(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	result := compileAndRun(t, env, "(do 1 2 3)")
	assert.True(t, result.Equals(core.Int{V: 3}), "expected 3, got %v", result)
}

func TestTailRecursiveFib(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def fib-iter (fn [n a b]
  (if (= n 0)
    a
    (fib-iter (- n 1) b (+ a b)))))
(def fib (fn [n] (fib-iter n 0 1)))
(fib 10)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 55}), "expected fib(10)=55, got %v", result)
}

func TestTailRecursiveFactorial(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def factorial-iter (fn [n acc]
  (if (= n 0)
    acc
    (factorial-iter (- n 1) (* acc n)))))
(def factorial (fn [n] (factorial-iter n 1)))
(factorial 10)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 3628800}), "expected 10!=3628800, got %v", result)
}

func TestClosureCreationAndInvocation(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def adder (fn [x y] (+ x y)))
(adder 5 10)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 15}), "expected 15, got %v", result)
}

func TestClosureNested(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def inner (fn [x y] (+ x y)))
(def outer (fn [a] (inner a 10)))
(outer 5)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 15}), "expected 15, got %v", result)
}

func TestClosure_LexicalCapture(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def make-adder (fn [n] (fn [x] (+ x n))))
(def add5 (make-adder 5))
(add5 10)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 15}), "expected 15, got %v", result)
}

func TestClosure_LexicalCapture_Multiple(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def make-adder (fn [n] (fn [x] (+ x n))))
(def add5 (make-adder 5))
(def add10 (make-adder 10))
(+ (add5 3) (add10 7))`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 25}), "expected 25, got %v", result)
}

func TestClosure_LexicalCapture_Let(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(let [n 100]
  (def get-n (fn [] n)))
(get-n)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 100}), "expected 100, got %v", result)
}

func TestGoFuncCallback(t *testing.T) {
	t.Parallel()

	called := false
	env := core.NewEnv(nil)
	env.Set("callback", core.GoFunc{
		Name: "callback",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			called = true
			if len(args) > 0 {
				return args[0], nil
			}
			return core.Int{V: 42}, nil
		},
	})

	result := compileAndRun(t, env, "(callback 99)")
	assert.True(t, called, "callback should have been called")
	assert.True(t, result.Equals(core.Int{V: 99}), "expected 99, got %v", result)
}

func TestGoFuncCallback_WithEvaluator(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	env.Set("compute", core.GoFunc{
		Name: "compute",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			x := args[0].(core.Int).V
			y := args[1].(core.Int).V
			return core.Int{V: x + y}, nil
		},
	})

	src := `
(def result (compute 3 7))
result`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 10}), "expected 10, got %v", result)
}

func TestContextCancellationHalt(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	env.Set("spin", core.GoFunc{
		Name: "spin",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
			}
		},
	})

	src := `(spin)`

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(env)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = v.Run(ctx, chunks[0])
	assert.Error(t, err, "should error on context cancellation")
}

func TestContextCancellation_BeforeRun(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	chunk := &vm.Chunk{
		Name: "test",
		Code: []vm.Instruction{
			vm.Encode(vm.OpTrue, 0),
			vm.Encode(vm.OpReturn, 0),
		},
	}

	v := vm.New(env)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := v.Run(ctx, chunk)
	assert.Error(t, err, "should error on pre-cancelled context")
}

func TestCompileAndRun_DataStructures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(core.Value) bool
	}{
		{
			name: "list",
			src:  "'(1 2 3)",
			check: func(v core.Value) bool {
				list, ok := v.(core.List)
				return ok && len(list.Items) == 3
			},
		},
		{
			name: "vector",
			src:  "[1 2 3]",
			check: func(v core.Value) bool {
				vec, ok := v.(core.Vector)
				return ok && len(vec.Items) == 3
			},
		},
		{
			name: "hashmap",
			src:  "{:a 1 :b 2}",
			check: func(v core.Value) bool {
				hm, ok := v.(*core.HashMap)
				return ok && hm.Len() == 2
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv()
			result := compileAndRun(t, env, tt.src)
			assert.True(t, tt.check(result), "check failed for %v", result)
		})
	}
}

func TestCompileAndRun_WhenUnless(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		expected core.Value
	}{
		{"when true", "(when true 1 2)", core.Int{V: 2}},
		{"unless false", "(unless false 1 2)", core.Int{V: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv()
			result := compileAndRun(t, env, tt.src)
			assert.True(t, result.Equals(tt.expected), "expected %v, got %v", tt.expected, result)
		})
	}
}

func TestCompileAndRun_SetBang(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	compileAndRun(t, env, "(def x 1)")
	result := compileAndRun(t, env, "(set! x 42)")
	assert.True(t, result.Equals(core.Int{V: 42}), "expected 42, got %v", result)

	val, ok := env.Get("x")
	require.True(t, ok)
	assert.True(t, val.Equals(core.Int{V: 42}), "x should be updated to 42")
}

func TestMultipleRuns_Reset(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	for i := range 5 {
		result := compileAndRun(t, env, "(+ 1 2)")
		assert.True(t, result.Equals(core.Int{V: 3}), "run %d: expected 3, got %v", i, result)
	}
}

func TestTopLevelCapturedLet(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `(do (def get-x (let [x 42] (fn [] x))) (get-x))`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 42}), "expected 42, got %v", result)
}

func TestSetBangAfterClosure(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(let [x 10]
  (def get-x (fn [] x))
  (set! x 20)
  (get-x))`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 20}), "expected 20, got %v", result)
}

func TestCapturedParam(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `((fn [a] (let [f (fn [] a)] (f))) 42)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 42}), "expected 42, got %v", result)
}

func TestCapturedClosureCall(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `((fn [x] (let [f (fn [y] (+ x y))] (f 10))) 32)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 42}), "expected 42, got %v", result)
}

// A large iteration count exercises OpLoop's back-edge across many passes
// through Run's per-frame dispatch locals, confirming reload after a
// preceding frame mutation never leaves stale state behind.
func TestLoopRecur_LargeIterationCount(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def sum-to (fn [n]
  (loop [i n acc 0]
    (if (= i 0)
      acc
      (recur (- i 1) (+ acc i))))))
(sum-to 200000)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 20000100000}), "expected 20000100000, got %v", result)
}

// Deep, non-tail self-recursion via OpCall grows the frame stack across many
// vm.call invocations, each re-growing the operand stack for its own chunk —
// confirms growStack's per-frame capacity hint survives repeated frame churn.
func TestDeepRecursion_ManyFrames(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(def count-down (fn [n]
  (if (= n 0)
    0
    (+ 1 (count-down (- n 1))))))
(count-down 5000)`

	result := compileAndRun(t, env, src)
	assert.True(t, result.Equals(core.Int{V: 5000}), "expected 5000, got %v", result)
}

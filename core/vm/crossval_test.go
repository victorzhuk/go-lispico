package vm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func newCrossValEnv() *core.Env {
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
	env.Set(">", core.GoFunc{
		Name: ">",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V > args[1].(core.Int).V}, nil
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

func TestVMVsTreeWalker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{"integer literal", "42"},
		{"string literal", `"hello"`},
		{"keyword", ":foo"},
		{"nil", "nil"},
		{"true", "true"},
		{"false", "false"},
		{"simple addition", "(+ 1 2)"},
		{"nested arithmetic", "(+ (* 2 3) (- 10 5))"},
		{"if true", "(if true 1 2)"},
		{"if false", "(if false 1 2)"},
		{"if nil", "(if nil 1 2)"},
		{"let binding", "(let [x 10 y 20] (+ x y))"},
		{"do block", "(do 1 2 3)"},
		{"vector literal", "[1 2 3]"},
		{"list literal", "'(1 2 3)"},
		{"hashmap literal", "{:a 1 :b 2}"},
		{"fn invocation", "((fn [x] (+ x 1)) 5)"},
		{"fn def and call", "(def add (fn [a b] (+ a b))) (add 3 4)"},
		{"def", "(def x 42) x"},
		{"when true", "(when true 1 2)"},
		{"unless false", "(unless false 1 2)"},
		{"quote", "'(a b c)"},
		{"set!", "(def x 1) (set! x 42) x"},
		{"comparison", "(< 1 2)"},
		{"equality", "(= 5 5)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tt.src)
			require.NoError(t, err, "read source")

			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			var treeResult core.Value = core.Nil{}
			for _, form := range forms {
				treeResult, err = treeEval.Eval(context.Background(), form, treeEnv)
				require.NoError(t, err, "tree-walker eval")
			}

			vmEnv := newCrossValEnv()
			chunks, err := compiler.CompileAll(forms)
			require.NoError(t, err, "compile")

			v := vm.New(vmEnv)
			var vmResult core.Value = core.Nil{}
			for _, chunk := range chunks {
				vmResult, err = v.Run(context.Background(), chunk)
				require.NoError(t, err, "vm run")
			}

			assert.True(t, vmResult.Equals(treeResult),
				"VM result %v (%T) != tree-walker result %v (%T)",
				vmResult, vmResult, treeResult, treeResult)
		})
	}
}

func TestVMVsTreeWalker_RecursiveFunctions(t *testing.T) {
	t.Parallel()

	src := `
(def fib (fn [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2))))))
(fib 10)`

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEnv := newCrossValEnv()
	treeEval := core.NewEvaluator()
	var treeResult core.Value = core.Nil{}
	for _, form := range forms {
		treeResult, err = treeEval.Eval(context.Background(), form, treeEnv)
		require.NoError(t, err, "tree-walker eval")
	}

	vmEnv := newCrossValEnv()
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(vmEnv)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v != tree-walker result %v", vmResult, treeResult)
	assert.True(t, vmResult.Equals(core.Int{V: 55}), "fib(10) should be 55")
}

func TestVMVsTreeWalker_Loop(t *testing.T) {
	t.Parallel()

	src := `
(def factorial (fn [n]
  (if (= n 0)
    1
    (* n (factorial (- n 1))))))
(factorial 10)`

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEnv := newCrossValEnv()
	treeEval := core.NewEvaluator()
	var treeResult core.Value = core.Nil{}
	for _, form := range forms {
		treeResult, err = treeEval.Eval(context.Background(), form, treeEnv)
		require.NoError(t, err, "tree-walker eval")
	}

	vmEnv := newCrossValEnv()
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(vmEnv)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v != tree-walker result %v", vmResult, treeResult)
	assert.True(t, vmResult.Equals(core.Int{V: 3628800}), "factorial(10) should be 3628800")
}

func TestVMVsTreeWalker_NestedClosures(t *testing.T) {
	t.Parallel()

	src := `
(def inner (fn [x y] (+ x y)))
(def outer (fn [a] (inner a 10)))
(outer 5)`

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEnv := newCrossValEnv()
	treeEval := core.NewEvaluator()
	var treeResult core.Value = core.Nil{}
	for _, form := range forms {
		treeResult, err = treeEval.Eval(context.Background(), form, treeEnv)
		require.NoError(t, err, "tree-walker eval")
	}

	vmEnv := newCrossValEnv()
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(vmEnv)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v != tree-walker result %v", vmResult, treeResult)
	assert.True(t, vmResult.Equals(core.Int{V: 15}), "result should be 15")
}

func TestVMVsTreeWalker_HigherOrder(t *testing.T) {
	t.Parallel()

	src := `
(def apply-twice (fn [f x] (f (f x))))
(def inc (fn [x] (+ x 1)))
(def result (apply-twice inc 5))
result`

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	env := newCrossValEnv()
	treeEval := core.NewEvaluator()
	var treeResult core.Value = core.Nil{}
	for _, form := range forms {
		treeResult, err = treeEval.Eval(context.Background(), form, env)
		require.NoError(t, err, "tree-walker eval")
	}

	vmEnv := newCrossValEnv()
	for _, form := range forms {
		// Need to set up the same env for tree-walker
		treeEval.Eval(context.Background(), form, vmEnv)
	}

	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(vmEnv)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v != tree-walker result %v", vmResult, treeResult)
}

func TestVMVsTreeWalker_AllTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		expected core.Value
	}{
		{"Int", "42", core.Int{V: 42}},
		{"Float", "3.14", core.Float{V: 3.14}},
		{"String", `"hello"`, core.String{V: "hello"}},
		{"Keyword", ":foo", core.Keyword{V: "foo"}},
		{"Symbol", "'sym", core.Symbol{V: "sym"}},
		{"Bool true", "true", core.Bool{V: true}},
		{"Bool false", "false", core.Bool{V: false}},
		{"Nil", "nil", core.Nil{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tt.src)
			require.NoError(t, err, "read source")

			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			treeResult, err := treeEval.Eval(context.Background(), forms[0], treeEnv)
			require.NoError(t, err, "tree-walker eval")

			vmEnv := newCrossValEnv()
			chunks, err := compiler.CompileAll(forms)
			require.NoError(t, err, "compile")

			v := vm.New(vmEnv)
			vmResult, err := v.Run(context.Background(), chunks[0])
			require.NoError(t, err, "vm run")

			assert.True(t, vmResult.Equals(treeResult),
				"VM result %v != tree-walker result %v", vmResult, treeResult)
			assert.True(t, vmResult.Equals(tt.expected),
				"result %v != expected %v", vmResult, tt.expected)
		})
	}
}

func compare(t *testing.T, env *core.Env, src string) {
	t.Helper()

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEval := core.NewEvaluator()
	var treeResult core.Value = core.Nil{}
	for _, form := range forms {
		treeResult, err = treeEval.Eval(context.Background(), form, env)
		require.NoError(t, err, "tree-walker eval")
	}

	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(env)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v (%T) != tree-walker result %v (%T)",
		vmResult, vmResult, treeResult, treeResult)
}

func TestVMVsTreeWalker_CondAndOrNot(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"cond else", "(cond (false 1) (:else 2))"},
		{"cond keyword else", "(cond (false 1) (:else 2))"},
		{"cond no match", "(cond (false 1) (false 2))"},
		{"and empty", "(and)"},
		{"and short", "(and 1 false 3)"},
		{"and last", "(and 1 2 3)"},
		{"or empty", "(or)"},
		{"or short", "(or false 2 3)"},
		{"or last false", "(or false false false)"},
		{"not true", "(not true)"},
		{"not nil", "(not nil)"},
		{"not zero", "(not 0)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_LoopRecur(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"sum to 10", "(loop [i 0 acc 0] (if (< i 10) (recur (+ i 1) (+ acc i)) acc))"},
		{"factorial", "(loop [n 5 acc 1] (if (= n 0) acc (recur (- n 1) (* acc n))))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_TryCatch(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"catch throw", "(try (throw \"boom\") (catch e e))"},
		{"no throw", "(try 42 (catch e e))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_Variadic(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"fixed plus rest", "((fn [a & rest] a) 1 2 3)"},
		{"rest only", "((fn [& rest] rest) 1 2 3)"},
		{"zero rest", "((fn [a & rest] a) 1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_LetStar(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"sequential", "(let* [x 1 y (+ x 1)] y)"},
		{"shadow", "(let* [x 1 x (* x 2)] x)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_Macro(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	forms, err := core.Read("(defmacro unless [cond body] `(if (not ~cond) ~body nil)) (unless false 42)")
	require.NoError(t, err, "read source")
	require.Len(t, forms, 2)

	treeEval := core.NewEvaluator()
	_, err = treeEval.Eval(context.Background(), forms[0], env)
	require.NoError(t, err, "define macro")

	expanded, err := treeEval.MacroExpand(context.Background(), forms[1], env)
	require.NoError(t, err, "macro expand")

	treeResult, err := treeEval.Eval(context.Background(), forms[1], env)
	require.NoError(t, err, "tree-walker eval")

	chunks, err := compiler.CompileAll([]core.Value{expanded})
	require.NoError(t, err, "compile")

	v := vm.New(env)
	var vmResult core.Value = core.Nil{}
	for _, chunk := range chunks {
		vmResult, err = v.Run(context.Background(), chunk)
		require.NoError(t, err, "vm run")
	}

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v (%T) != tree-walker result %v (%T)",
		vmResult, vmResult, treeResult, treeResult)
}

func TestVMVsTreeWalker_NonStringThrow(t *testing.T) {
	t.Parallel()

	src := "(try (throw 42) (catch e e))"
	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEnv := newCrossValEnv()
	treeEval := core.NewEvaluator()
	treeResult, err := treeEval.Eval(context.Background(), forms[0], treeEnv)
	require.NoError(t, err, "tree-walker eval")
	assert.True(t, treeResult.Equals(core.String{V: "42"}), "tree-walker: expected String(42), got %v (%T)", treeResult, treeResult)

	vmEnv := newCrossValEnv()
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err, "compile")

	v := vm.New(vmEnv)
	vmResult, err := v.Run(context.Background(), chunks[0])
	require.NoError(t, err, "vm run")
	assert.True(t, vmResult.Equals(core.String{V: "42"}), "vm: expected String(42), got %v (%T)", vmResult, vmResult)

	assert.True(t, vmResult.Equals(treeResult),
		"VM result %v (%T) != tree-walker result %v (%T)",
		vmResult, vmResult, treeResult, treeResult)
}

func TestVMVsTreeWalker_NestedDefmacro(t *testing.T) {
	t.Parallel()

	src := "(do (defmacro id [x] x) (id 42))"
	forms, err := core.Read(src)
	require.NoError(t, err, "read source")

	treeEnv := newCrossValEnv()
	treeEval := core.NewEvaluator()
	treeResult, err := treeEval.Eval(context.Background(), forms[0], treeEnv)
	require.NoError(t, err, "tree-walker should evaluate a defmacro nested in a do body")
	assert.True(t, treeResult.Equals(core.Int{V: 42}), "expected 42, got %v", treeResult)

	_, compileErr := compiler.CompileAll(forms)
	require.Error(t, compileErr, "bytecode compiler should reject a nested defmacro, not miscompile it")

	var lispErr *core.LispicoError
	require.True(t, errors.As(compileErr, &lispErr), "expected *core.LispicoError, got %T", compileErr)
	assert.Equal(t, compiler.CodeUnsupported, lispErr.Code, "nested defmacro should be classified as unsupported-in-bytecode")
}

func TestVMVsTreeWalker_EmptyBodyFnDefn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{"empty-body fn", "((fn []))"},
		{"empty-body defn", "(defn f [])"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tt.src)
			require.NoError(t, err, "read source")

			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			_, treeErr := treeEval.Eval(context.Background(), forms[0], treeEnv)
			assert.Error(t, treeErr, "tree-walker should reject an empty-body fn/defn, not panic")

			_, compileErr := compiler.CompileAll(forms)
			assert.Error(t, compileErr, "bytecode compiler should reject an empty-body fn/defn, not panic")
		})
	}
}

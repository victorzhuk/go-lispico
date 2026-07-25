package vm_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// buildLetSource returns `(let [v0 0 v1 1 ... v(n-1) (n-1)] (+ v0 v1 ...))` —
// a let binding n symbols, straddling the persistent vector's future
// promotion boundary at n=32.
func buildLetSource(n int) string {
	var pairs, sum strings.Builder
	for i := range n {
		if i > 0 {
			sum.WriteByte(' ')
		}
		fmt.Fprintf(&pairs, "v%d %d ", i, i)
		fmt.Fprintf(&sum, "v%d", i)
	}
	return fmt.Sprintf("(let [%s] (+ %s))", strings.TrimSpace(pairs.String()), sum.String())
}

// buildFnSource returns a def-then-call program for a fn taking n params.
func buildFnSource(n int) string {
	var params, sum, args strings.Builder
	for i := range n {
		if i > 0 {
			sum.WriteByte(' ')
			args.WriteByte(' ')
		}
		fmt.Fprintf(&params, "p%d ", i)
		fmt.Fprintf(&sum, "p%d", i)
		fmt.Fprintf(&args, "%d", i)
	}
	return fmt.Sprintf("(def addN (fn [%s] (+ %s)))\n(addN %s)",
		strings.TrimSpace(params.String()), sum.String(), args.String())
}

func newBenchEnv() *core.Env {
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			var sum int64
			for _, a := range args {
				sum += a.(core.Int).V
			}
			return core.Int{V: sum}, nil
		},
	})
	env.SetCanonical("-", core.GoFunc{
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
	env.SetCanonical("*", core.GoFunc{
		Name: "*",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			result := int64(1)
			for _, a := range args {
				result *= a.(core.Int).V
			}
			return core.Int{V: result}, nil
		},
	})
	env.SetCanonical("<", core.GoFunc{
		Name: "<",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
		},
	})
	env.SetCanonical("=", core.GoFunc{
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
	env.Set("cons", core.GoFunc{
		Name: "cons",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			c := args[1].(core.List)
			res, _ := c.Cons(args[0])
			return res, nil
		},
	})
	env.Set("count", core.GoFunc{
		Name: "count",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Int{V: int64(args[0].(core.List).Len())}, nil
		},
	})
	env.Set("map", core.GoFunc{
		Name: "map",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			fn := args[0]
			list := args[1].(core.List)
			n := list.Len()
			result := make([]core.Value, n)
			for i := 0; i < n; i++ {
				var err error
				result[i], err = eval.Apply(ctx, fn, []core.Value{list.At(i)}, env)
				if err != nil {
					return nil, err
				}
			}
			return core.NewList(result), nil
		},
	})
	return env
}

func BenchmarkArithmeticLoop_VM(b *testing.B) {
	src := `
(def sum-to (fn [n]
  (loop [i n acc 0]
    (if (= i 0)
      acc
      (recur (- i 1) (+ acc i))))))
(sum-to 10000)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkArithmeticLoop_TreeWalker(b *testing.B) {
	src := `
(def sum-to (fn [n]
  (loop [i n acc 0]
    (if (= i 0)
      acc
      (recur (- i 1) (+ acc i))))))
(sum-to 10000)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkFunctionCall_VM(b *testing.B) {
	src := `
(def add (fn [a b] (+ a b)))
(def nested (fn [x] (add x 1)))
(nested 42)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkUncapturedFunctionCall_VM(b *testing.B) {
	src := `
(def add-one (fn [x] (+ x 1)))
(add-one 41)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkUncapturedFunctionCall_TreeWalker(b *testing.B) {
	src := `
(def add-one (fn [x] (+ x 1)))
(add-one 41)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkFunctionCall_TreeWalker(b *testing.B) {
	src := `
(def add (fn [a b] (+ a b)))
(def nested (fn [x] (add x 1)))
(nested 42)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkMapOverList_VM(b *testing.B) {
	src := `
(def double (fn [x] (* x 2)))
(map double '(1 2 3 4 5 6 7 8 9 10))`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkMapOverList_TreeWalker(b *testing.B) {
	src := `
(def double (fn [x] (* x 2)))
(map double '(1 2 3 4 5 6 7 8 9 10))`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkFileLoad_Uncached(b *testing.B) {
	var lines []string
	for i := range 1000 {
		lines = append(lines, fmt.Sprintf("(def x%d %d)", i, i))
	}
	content := fmt.Sprintf("%s\n(def result (+ x0 x1))", lines)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		forms, _ := core.Read(content)
		chunks, _ := compiler.CompileAll(forms)
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkFileLoad_TreeWalker(b *testing.B) {
	var lines []string
	for i := range 1000 {
		lines = append(lines, fmt.Sprintf("(def x%d %d)", i, i))
	}
	content := fmt.Sprintf("%s\n(def result (+ x0 x1))", lines)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		forms, _ := core.Read(content)
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkFibonacci_VM(b *testing.B) {
	src := `
(def fib (fn [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2))))))
(fib 15)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkFibonacci_TreeWalker(b *testing.B) {
	src := `
(def fib (fn [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2))))))
(fib 15)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkClosure_VM(b *testing.B) {
	src := `
(def adder (fn [x y] (+ x y)))
(adder 10 5)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkClosure_TreeWalker(b *testing.B) {
	src := `
(def adder (fn [x y] (+ x y)))
(adder 10 5)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkSimpleArithmetic_VM(b *testing.B) {
	src := "(+ 1 2 3 4 5)"
	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkSimpleArithmetic_TreeWalker(b *testing.B) {
	src := "(+ 1 2 3 4 5)"
	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkLet_VM(b *testing.B) {
	src := "(let [a 1 b 2 c 3 d 4 e 5] (+ a b c d e))"
	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkLet_TreeWalker(b *testing.B) {
	src := "(let [a 1 b 2 c 3 d 4 e 5] (+ a b c d e))"
	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

// BenchmarkLetWide16_VM/_TreeWalker and BenchmarkLetWide40_VM/_TreeWalker
// cover the binding-carrier regression risk at the VM's execution path:
// `let` binds through a Vector, and a wide binding vector straddling the
// future promotion threshold at n=32 must not regress against BenchmarkLet's
// small (5-binding) shape above.
func BenchmarkLetWide16_VM(b *testing.B) {
	forms, _ := core.Read(buildLetSource(16))
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkLetWide16_TreeWalker(b *testing.B) {
	forms, _ := core.Read(buildLetSource(16))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkLetWide40_VM(b *testing.B) {
	forms, _ := core.Read(buildLetSource(40))
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkLetWide40_TreeWalker(b *testing.B) {
	forms, _ := core.Read(buildLetSource(40))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

// BenchmarkFnCallWide16_VM/_TreeWalker and BenchmarkFnCallWide40_VM/_TreeWalker
// cover the same carrier risk for fn parameter lists, complementing the
// fixed 2-param BenchmarkFunctionCall_VM/_TreeWalker above.
func BenchmarkFnCallWide16_VM(b *testing.B) {
	forms, _ := core.Read(buildFnSource(16))
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkFnCallWide16_TreeWalker(b *testing.B) {
	forms, _ := core.Read(buildFnSource(16))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkFnCallWide40_VM(b *testing.B) {
	forms, _ := core.Read(buildFnSource(40))
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkFnCallWide40_TreeWalker(b *testing.B) {
	forms, _ := core.Read(buildFnSource(40))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkTailCall_VM(b *testing.B) {
	src := `
(def sum-to (fn [n acc]
  (if (= n 0)
    acc
    (sum-to (- n 1) (+ acc n)))))
(sum-to 1000 0)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkTailCall_TreeWalker(b *testing.B) {
	src := `
(def sum-to (fn [n acc]
  (if (= n 0)
    acc
    (sum-to (- n 1) (+ acc n)))))
(sum-to 1000 0)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkAccumulate100_VM(b *testing.B) {
	src := `
(def build (fn [n]
  (loop [i 0 acc '()]
    (if (= i n)
      acc
      (recur (+ i 1) (cons i acc))))))
(build 100)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkAccumulate100_TreeWalker(b *testing.B) {
	src := `
(def build (fn [n]
  (loop [i 0 acc '()]
    (if (= i n)
      acc
      (recur (+ i 1) (cons i acc))))))
(build 100)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

func BenchmarkAccumulate1000_VM(b *testing.B) {
	src := `
(def build (fn [n]
  (loop [i 0 acc '()]
    (if (= i n)
      acc
      (recur (+ i 1) (cons i acc))))))
(build 1000)`

	forms, _ := core.Read(src)
	chunks, _ := compiler.CompileAll(forms)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		v := vm.New(env)
		for _, chunk := range chunks {
			v.Run(context.Background(), chunk)
		}
	}
}

func BenchmarkAccumulate1000_TreeWalker(b *testing.B) {
	src := `
(def build (fn [n]
  (loop [i 0 acc '()]
    (if (= i n)
      acc
      (recur (+ i 1) (cons i acc))))))
(build 1000)`

	forms, _ := core.Read(src)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env := newBenchEnv()
		e := core.NewEvaluator()
		for _, form := range forms {
			e.Eval(context.Background(), form, env)
		}
	}
}

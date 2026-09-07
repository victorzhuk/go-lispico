package vm_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// runTreeWalker evaluates src form-by-form in the tree-walking evaluator as
// the reference control for the VM under test.
func runTreeWalker(t *testing.T, env *core.Env, src string) (core.Value, error) {
	t.Helper()
	forms, err := core.Read(src)
	require.NoError(t, err, "read source")
	eval := core.NewEvaluator()
	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = eval.Eval(context.Background(), form, env)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// runVM compiles src and runs it on a fresh VM with the given env under a
// deadline so a divergent (infinite) loop still terminates with an error.
func runVM(t *testing.T, env *core.Env, src string, timeout time.Duration) (core.Value, error) {
	t.Helper()
	chunks := compileSrc(t, src)
	v := vm.New(env)
	v.SetTimeout(timeout)
	var result core.Value = core.Nil{}
	var err error
	for _, chunk := range chunks {
		result, err = v.Run(context.Background(), chunk)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// TestVMLoopScopeParity pins that the loop compiler restores the enclosing
// scope after compiling the body, so loop initializers resolve names against
// the scope visible at the loop form, and that a `recur`-driven loop
// terminates once its condition is met. The tree-walker is the control.
func TestVMLoopScopeParity(t *testing.T) {
	t.Parallel()

	t.Run("let initializer in loop bindings terminates and yields its value", func(t *testing.T) {
		t.Parallel()
		src := `(loop [i 0 acc (let [x 5] x)] (if (= i 3) acc (recur (+ i 1) acc)))`

		treeResult, treeErr := runTreeWalker(t, newTestEnv(), src)
		require.NoError(t, treeErr, "tree-walker control")
		require.True(t, treeResult.Equals(core.Int{V: 5}),
			"tree-walker control must return 5, got %v", treeResult)

		vmResult, vmErr := runVM(t, newTestEnv(), src, 2*time.Second)
		if vmErr != nil {
			require.False(t, errors.Is(vmErr, context.DeadlineExceeded),
				"VM loop with a `let` initializer diverges (initializer scope lost); tree-walker returned 5: %v", vmErr)
			require.NoError(t, vmErr, "vm run")
		}
		require.True(t, vmResult.Equals(core.Int{V: 5}),
			"TestVMLoopScopeParity/let-initializer: VM result %v, want 5 (tree-walker control 5)", vmResult)
	})

	t.Run("loop initializer sees the enclosing binding when shadowed", func(t *testing.T) {
		t.Parallel()
		src := `(let [a 10] (loop [a 1 b a] b))`

		treeResult, treeErr := runTreeWalker(t, newTestEnv(), src)
		require.NoError(t, treeErr, "tree-walker control")
		require.True(t, treeResult.Equals(core.Int{V: 10}),
			"tree-walker control must return 10, got %v", treeResult)

		vmResult, vmErr := runVM(t, newTestEnv(), src, 2*time.Second)
		require.NoError(t, vmErr, "vm run")
		require.True(t, vmResult.Equals(core.Int{V: 10}),
			"TestVMLoopScopeParity/shadowed-initializer: VM result %v, want 10 (tree-walker control 10)", vmResult)
	})

	t.Run("sequential let and let* controls unchanged", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			`(let [a 1 b (+ a 1)] b)`,
			`(let* [a 1 b (+ a 1)] b)`,
		} {
			treeResult, treeErr := runTreeWalker(t, newTestEnv(), src)
			require.NoError(t, treeErr, "tree-walker control: %s", src)
			require.True(t, treeResult.Equals(core.Int{V: 2}),
				"tree-walker control must return 2 for %s, got %v", src, treeResult)

			vmResult, vmErr := runVM(t, newTestEnv(), src, 2*time.Second)
			require.NoError(t, vmErr, "vm run: %s", src)
			assert.True(t, vmResult.Equals(core.Int{V: 2}),
				"TestVMLoopScopeParity/sequential-controls: VM result %v, want 2 for %s", vmResult, src)
		}
	})
}

// vmStackHeight reports the live operand-stack length of v via a read-only
// reflect view of the unexported stack slice. The external test package
// cannot call vm.stackSize() directly.
func vmStackHeight(v *vm.VM) int {
	rv := reflect.ValueOf(v).Elem().FieldByName("stack")
	if !rv.IsValid() {
		return -1
	}
	return rv.Len()
}

// TestVMLoopOperandBound pins that recur restores the operand-stack height it
// saved before evaluating replacement arguments, so live operand height does
// not grow with iteration count. A GoFunc probe snapshots the stack length
// every iteration; height at 1000 iterations must stay within a small
// constant of height at 10.
func TestVMLoopOperandBound(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		src  func(n int) string
	}
	rows := []row{
		{
			name: "nested let and vector allocation per iteration",
			src: func(n int) string {
				return `(loop [i 0 acc 0] (if (= i ` + strconv.Itoa(n) + `) acc (recur (probe (+ i 1)) (let [x [i i]] x))))`
			},
		},
		{
			name: "nested loop recur targets inner loop",
			src: func(n int) string {
				return `(loop [i 0] (if (= i ` + strconv.Itoa(n) + `) i (do (loop [j 0] (if (= j 2) j (recur (probe (+ j 1))))) (recur (probe (+ i 1))))))`
			},
		},
		{
			name: "simultaneous replacement of all bindings",
			src: func(n int) string {
				return `(loop [i 0 acc 0] (if (= i ` + strconv.Itoa(n) + `) acc (recur (probe (+ i 1)) (+ acc i))))`
			},
		},
		{
			name: "escaping closure captures loop binding",
			src: func(n int) string {
				return `(loop [i 0 acc 0] (if (= i ` + strconv.Itoa(n) + `) acc (recur (probe (+ i 1)) ((fn [] (+ acc i))))))`
			},
		},
	}

	runWithProbe := func(t *testing.T, src string) int {
		t.Helper()
		env := newTestEnv()
		var v *vm.VM
		var max int
		env.Set("probe", core.GoFunc{
			Name: "probe",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				if h := vmStackHeight(v); h > max {
					max = h
				}
				return args[0], nil
			},
		})
		chunks := compileSrc(t, src)
		v = vm.New(env)
		v.SetTimeout(10 * time.Second)
		var err error
		for _, chunk := range chunks {
			if _, err = v.Run(context.Background(), chunk); err != nil {
				break
			}
		}
		require.NoError(t, err, "vm run")
		return max
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			observe := func(n int) int {
				h := runWithProbe(t, r.src(n))
				return h
			}
			h10 := observe(10)
			h100 := observe(100)
			h1000 := observe(1000)

			require.Greater(t, h10, 0, "TestVMLoopOperandBound: probe must observe a positive stack height")
			assert.LessOrEqual(t, h100, h10+8,
				"TestVMLoopOperandBound: height(100)=%d must be within 8 of height(10)=%d", h100, h10)
			assert.LessOrEqual(t, h1000, h10+8,
				"TestVMLoopOperandBound: height(1000)=%d must be within 8 of height(10)=%d — discarded recur operands accumulate", h1000, h10)
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

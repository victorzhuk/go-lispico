package vm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// TestVMLocalOperandParity pins the vm-local-stack-scope delta repros: lexical
// bindings inside operand positions must not corrupt earlier operands, and
// loop bindings must stay inside the loop's lexical scope. Expected values are
// pinned from the spec delta, and the tree-walker runs as the control on the
// same source.
func TestVMLocalOperandParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		expected core.Value // nil: both evaluators must reject with UndefinedError
	}{
		{
			name:     "vector preserves an earlier element",
			src:      `[10 (let [x 2] x)]`,
			expected: core.NewVector([]core.Value{core.Int{V: 10}, core.Int{V: 2}}),
		},
		{
			name:     "native call preserves an earlier argument",
			src:      `(+ 10 (let [x 2] x))`,
			expected: core.Int{V: 12},
		},
		{
			name:     "ordinary call preserves target and arguments",
			src:      `((fn [a b] a) 10 (let [x 2] x))`,
			expected: core.Int{V: 10},
		},
		{
			name:     "loop exit restores a shadowed outer binding",
			src:      `(let [x 10] (loop [x 1] x) x)`,
			expected: core.Int{V: 10},
		},
		{
			name:     "loop initializer sees the enclosing binding",
			src:      `(let [a 10] (loop [a 1 b a] b))`,
			expected: core.Int{V: 10},
		},
		{
			name: "loop binding is unavailable after exit",
			src:  `(do (loop [x 1] x) x)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tt.src)
			require.NoError(t, err, "read source")

			// Tree-walker control: the reference behavior must already hold.
			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			var treeResult core.Value = core.Nil{}
			var treeErr error
			for _, form := range forms {
				treeResult, treeErr = treeEval.Eval(context.Background(), form, treeEnv)
				if treeErr != nil {
					break
				}
			}

			// VM under test.
			vmEnv := newCrossValEnv()
			chunks, err := compiler.CompileAll(forms)
			require.NoError(t, err, "compile")
			v := vm.New(vmEnv)
			var vmResult core.Value = core.Nil{}
			var vmErr error
			for _, chunk := range chunks {
				vmResult, vmErr = v.Run(context.Background(), chunk)
				if vmErr != nil {
					break
				}
			}

			if tt.expected == nil {
				require.Error(t, treeErr, "tree-walker control must reject the escaped loop binding")
				var tle *core.LispicoError
				require.ErrorAs(t, treeErr, &tle, "tree-walker error must be *core.LispicoError")
				assert.Equal(t, "UndefinedError", tle.Code, "tree-walker error code")

				assert.Error(t, vmErr, "VM must reject the escaped loop binding with an error")
				var vle *core.LispicoError
				if assert.ErrorAs(t, vmErr, &vle, "VM error must be *core.LispicoError") {
					assert.Equal(t, "UndefinedError", vle.Code, "VM error code")
				}
				return
			}

			require.NoError(t, treeErr, "tree-walker control")
			require.NotNil(t, treeResult, "tree-walker control")
			require.True(t, treeResult.Equals(tt.expected),
				"tree-walker control must match pinned result %v, got %v", tt.expected, treeResult)

			require.NoError(t, vmErr, "vm run")
			require.NotNil(t, vmResult, "vm run")
			assert.True(t, vmResult.Equals(tt.expected),
				"VM result %v (%T) != pinned result %v (%T)", vmResult, vmResult, tt.expected, tt.expected)
			assert.Equal(t, treeResult.Type(), vmResult.Type(),
				"VM result type must equal tree-walker result type")
		})
	}
}

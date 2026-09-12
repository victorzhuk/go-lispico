package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// The evaluator rejects excess if arguments and bodyless when before touching
// any branch (evalIf/evalWhen, pinned by TestEval_IfErrors and
// TestEval_WhenErrors). The compiler must refuse the same arities with the
// same error type and wording, or a form one mode rejects would run partially
// on the other.

func TestCompilerIfArityParity(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`(if true 1 2 3)`, `(if false 1 2 3)`} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			forms, err := core.Read(src)
			require.NoError(t, err)

			_, treeErr := core.NewEvaluator().Eval(t.Context(), forms[0], core.NewEnv(nil))
			require.Error(t, treeErr, "evaluator must reject excess if args")
			var treeLe *core.LispicoError
			require.ErrorAs(t, treeErr, &treeLe)
			assert.Equal(t, "EvalError", treeLe.Code)
			assert.Equal(t, "if requires 2 or 3 arguments", treeLe.Message)

			var le *core.LispicoError
			require.ErrorAs(t, NewCompiler("test").Compile(forms[0]), &le, "compiler must reject excess if args")
			assert.Equal(t, CodeCompileError, le.Code)
			assert.Equal(t, treeLe.Message, le.Message)
		})
	}
}

func TestCompilerWhenArityParity(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`(when true)`, `(when false)`} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			forms, err := core.Read(src)
			require.NoError(t, err)

			_, treeErr := core.NewEvaluator().Eval(t.Context(), forms[0], core.NewEnv(nil))
			require.Error(t, treeErr, "evaluator must reject bodyless when")
			var treeLe *core.LispicoError
			require.ErrorAs(t, treeErr, &treeLe)
			assert.Equal(t, "EvalError", treeLe.Code)
			assert.Equal(t, "when requires at least 2 arguments", treeLe.Message)

			var le *core.LispicoError
			require.ErrorAs(t, NewCompiler("test").Compile(forms[0]), &le, "compiler must reject bodyless when")
			assert.Equal(t, CodeCompileError, le.Code)
			assert.Equal(t, treeLe.Message, le.Message)
		})
	}
}

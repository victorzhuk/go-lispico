package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/runtime"
)

// TestCL_SurfaceNameDispatchParity pins that a surface (hidden) name under the
// CL dialect dispatches like any ordinary symbol: a user binding of do is a
// user function/macro, never the kernel form, on both evaluators.
//
// do and set! are hidden (renamed away to progn/setq), so at the base revision
// the bytecode VM refuses them with a CompileError and the tree-walker's
// expandDeepList early-returns on removed heads — each case below fails on
// that behavior until the dispatch fix lands.
func TestCL_SurfaceNameDispatchParity(t *testing.T) {
	modes := []struct {
		name string
		opt  runtime.EngineOption
	}{
		{name: "tree-walker", opt: runtime.WithTreeWalker()},
		{name: "vm", opt: runtime.WithBytecode()},
	}

	eval := func(t *testing.T, mode runtime.EngineOption, srcs ...string) (core.Value, error) {
		t.Helper()
		e := newEngine(t, mode)
		var got core.Value
		for _, src := range srcs {
			var err error
			got, err = e.Eval(context.Background(), "surface-name-dispatch", src)
			if err != nil {
				return got, err
			}
		}
		return got, nil
	}

	t.Run("user-bound hidden name is a user function", func(t *testing.T) {
		srcs := []string{"(defun do (x) (* x 2))", "(do 5)"}
		results := make([]core.Value, len(modes))
		errs := make([]error, len(modes))
		for i, mode := range modes {
			results[i], errs[i] = eval(t, mode.opt, srcs...)
			require.NoErrorf(t, errs[i], "%s: %s must evaluate", mode.name, srcs[1])
			assert.Truef(t, (core.Int{V: 10}).Equals(results[i]),
				"%s: (do 5) after (defun do (x) (* x 2)) = %v, want 10", mode.name, results[i])
		}
		assert.True(t, results[0].Equals(results[1]),
			"evaluators disagree: tree-walker %v, vm %v", results[0], results[1])
	})

	t.Run("defmacro on hidden name dispatches to the user macro", func(t *testing.T) {
		srcs := []string{"(defmacro do (& body) (cons 'progn body))", "(do 1 2)"}
		results := make([]core.Value, len(modes))
		errs := make([]error, len(modes))
		for i, mode := range modes {
			results[i], errs[i] = eval(t, mode.opt, srcs...)
			require.NoErrorf(t, errs[i], "%s: %s must evaluate", mode.name, srcs[1])
			assert.Truef(t, (core.Int{V: 2}).Equals(results[i]),
				"%s: (do 1 2) after (defmacro do (& body) (cons 'progn body)) = %v, want 2", mode.name, results[i])
		}
		assert.True(t, results[0].Equals(results[1]),
			"evaluators disagree: tree-walker %v, vm %v", results[0], results[1])
	})

	t.Run("unbound hidden name is an UndefinedError on both evaluators", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			src  string
		}{
			{name: "unbound do", src: "(do 1)"},
			{name: "unbound set!", src: "(set! x 1)"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				typed := make([]*core.LispicoError, len(modes))
				for i, mode := range modes {
					_, err := eval(t, mode.opt, tc.src)
					require.Errorf(t, err, "%s: unbound %s must error", mode.name, tc.src)
					var le *core.LispicoError
					require.ErrorAsf(t, err, &le, "%s: error must be a typed *core.LispicoError, got %v", mode.name, err)
					assert.Equalf(t, "UndefinedError", le.Code,
						"%s: unbound hidden name %s must refuse at runtime with Code UndefinedError, got %q (%s)",
						mode.name, tc.src, le.Code, le.Message)
					typed[i] = le
				}
				assert.Equal(t, typed[0].Code, typed[1].Code,
					"evaluators disagree on Code: tree-walker %q, vm %q", typed[0].Code, typed[1].Code)
				assert.Equal(t, typed[0].Message, typed[1].Message,
					"evaluators disagree on Message: tree-walker %q, vm %q", typed[0].Message, typed[1].Message)
			})
		}
	})
}

// TestCL_SurfaceNameShapeErrorParity pins that the CL function/funcall shape
// errors are typed *core.LispicoError with Code EvalError and the
// tree-walker's message verbatim on both evaluators — the bytecode compiler
// must refuse the same shapes with the same typed code and message, not its
// own untyped compile error.
func TestCL_SurfaceNameShapeErrorParity(t *testing.T) {
	modes := []struct {
		name string
		opt  runtime.EngineOption
	}{
		{name: "tree-walker", opt: runtime.WithTreeWalker()},
		{name: "vm", opt: runtime.WithBytecode()},
	}

	eval := func(t *testing.T, mode runtime.EngineOption, src string) (core.Value, error) {
		t.Helper()
		e := newEngine(t, mode)
		return e.Eval(context.Background(), "surface-name-shape-error", src)
	}

	for _, tc := range []struct {
		name     string
		src      string
		wantMsg  string
		wantRe   string
		wantCode string
	}{
		{name: "function with 2 arguments", src: "(function a b)", wantMsg: "function requires exactly 1 argument", wantCode: "EvalError"},
		{name: "function with non-symbol argument", src: "(function 1)", wantRe: `^function: argument must be a symbol, got .+`, wantCode: "EvalError"},
		{name: "funcall with no arguments", src: "(funcall)", wantMsg: "funcall requires at least 1 argument", wantCode: "EvalError"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typed := make([]*core.LispicoError, len(modes))
			for i, mode := range modes {
				_, err := eval(t, mode.opt, tc.src)
				require.Errorf(t, err, "%s: %s must error", mode.name, tc.src)
				var le *core.LispicoError
				require.ErrorAsf(t, err, &le, "%s: error must be a typed *core.LispicoError, got %v", mode.name, err)
				assert.Equalf(t, tc.wantCode, le.Code,
					"%s: %s must refuse with Code %s, got %q (%s)", mode.name, tc.src, tc.wantCode, le.Code, le.Message)
				if tc.wantMsg != "" {
					assert.Equalf(t, tc.wantMsg, le.Message,
						"%s: %s must refuse with message %q, got %q", mode.name, tc.src, tc.wantMsg, le.Message)
				} else {
					assert.Regexpf(t, tc.wantRe, le.Message,
						"%s: %s must refuse with a message matching %q, got %q", mode.name, tc.src, tc.wantRe, le.Message)
				}
				typed[i] = le
			}
			assert.Equal(t, typed[0].Code, typed[1].Code,
				"evaluators disagree on Code: tree-walker %q, vm %q", typed[0].Code, typed[1].Code)
			assert.Equal(t, typed[0].Message, typed[1].Message,
				"evaluators disagree on Message: tree-walker %q, vm %q", typed[0].Message, typed[1].Message)
		})
	}
}

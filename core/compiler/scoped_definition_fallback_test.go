package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

// TestCompilerScopedDefinitionFallback pins the compiler's boundary for
// definitions inside lexical-definition scopes: fn bodies, let/let* and loop
// bindings, and catch handlers. The tree-walker binds a scoped def/defn into
// the enclosing scope, but the VM's only definition opcodes are OpSetGlobal
// and OpSetFunc, so a scoped definition would silently land in the global
// namespace; it must compile to CodeUnsupported so the runtime can fall back
// to the tree-walker, exactly as a nested defmacro does. Recognition follows
// lexical-definition scope, not binding count (empty binding lists count) or
// syntax depth (definitions behind do or nested several scopes deep count),
// and shape validation still runs first: a malformed scoped def keeps its
// CompileError. Genuine top-level definitions stay supported.
func TestCompilerScopedDefinitionFallback(t *testing.T) {
	mustUnsupported := func(t *testing.T, src string) {
		t.Helper()
		forms, err := core.Read(src)
		require.NoError(t, err)
		compileErr := NewCompiler("test").Compile(forms[0])
		var le *core.LispicoError
		require.ErrorAs(t, compileErr, &le, "%s: want code %s, got %v", src, CodeUnsupported, compileErr)
		assert.Equal(t, CodeUnsupported, le.Code, "%s: wrong error code", src)
	}

	t.Run("scoped def refused", func(t *testing.T) {
		for _, tc := range []struct{ name, src string }{
			{"def in fn body", `(fn (x) (def y 1))`},
			{"def in fn body without params", `(fn [] (def y 1))`},
			{"def in let", `(let [x 1] (def y 2))`},
			{"def in let with empty bindings", `(let [] (def y 1))`},
			{"def in let*", `(let* [x 1] (def y 2))`},
			{"def in let* with empty bindings", `(let* [] (def y 1))`},
			{"def in loop", `(loop [i 0] (def y 1))`},
			{"def in loop with empty bindings", `(loop [] (def y 1))`},
			{"def in catch handler", `(try 1 (catch e (def y 2)))`},
			{"def nested across scopes", `(fn [] (let [] (loop [] (def y 1))))`},
			{"def behind do inside let", `(let [] (do (def y 1)))`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				mustUnsupported(t, tc.src)
			})
		}
	})

	t.Run("scoped defn refused", func(t *testing.T) {
		for _, tc := range []struct{ name, src string }{
			{"defn in let", `(let [] (defn f (x) x))`},
			{"defn in fn body", `(fn [] (defn f (x) x))`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				mustUnsupported(t, tc.src)
			})
		}
	})

	// Under CL (Lisp-2), compileDefn takes the direct OpSetFunc branch
	// instead of rewriting to def; a scoped definition must be refused on
	// that route too.
	t.Run("scoped defn refused under Lisp-2 dialect", func(t *testing.T) {
		clDialect := cl.Dialect()
		forms, err := core.Read(`(let [] (defn f (x) x))`)
		require.NoError(t, err)
		compileErr := NewCompilerWithDialect("test", &clDialect).Compile(forms[0])
		var le *core.LispicoError
		require.ErrorAs(t, compileErr, &le, "%s under CL: want code %s, got %v", `(let [] (defn f (x) x))`, CodeUnsupported, compileErr)
		assert.Equal(t, CodeUnsupported, le.Code)
	})

	// Refusal applies after shape validation: a malformed scoped def keeps
	// its CompileError instead of gaining CodeUnsupported.
	t.Run("shape validation precedes refusal", func(t *testing.T) {
		forms, err := core.Read(`(let [] (def))`)
		require.NoError(t, err)
		compileErr := NewCompiler("test").Compile(forms[0])
		var le *core.LispicoError
		require.ErrorAs(t, compileErr, &le, "%s: want code %s, got %v", `(let [] (def))`, CodeCompileError, compileErr)
		assert.Equal(t, CodeCompileError, le.Code)
	})

	t.Run("top-level definitions remain supported", func(t *testing.T) {
		for _, tc := range []struct{ name, src string }{
			{"top-level def", `(def y 1)`},
			{"top-level defn", `(defn f (x) x)`},
			{"def inside top-level do", `(do (def y 1))`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				forms, err := core.Read(tc.src)
				require.NoError(t, err)
				require.NoError(t, NewCompiler("test").Compile(forms[0]), "%s must compile", tc.src)
			})
		}

		t.Run("top-level defn under Lisp-2 dialect", func(t *testing.T) {
			clDialect := cl.Dialect()
			forms, err := core.Read(`(defn f (x) x)`)
			require.NoError(t, err)
			require.NoError(t, NewCompilerWithDialect("test", &clDialect).Compile(forms[0]))
		})
	})
}

// TestCompilerDefunScopedDefinitionFallback covers the Lisp-2 defun route:
// its fn sub-compiler must open the same lexical-definition scope compileFn
// does, or a def in a defun body emits a global store instead of falling
// back to the tree-walker, which binds it into the enclosing scope.
func TestCompilerDefunScopedDefinitionFallback(t *testing.T) {
	t.Parallel()
	clDialect := cl.Dialect()
	forms, err := core.Read(`(defun outer () (def leaked 1))`)
	require.NoError(t, err)
	compileErr := NewCompilerWithDialect("test", &clDialect).Compile(forms[0])
	var le *core.LispicoError
	require.ErrorAs(t, compileErr, &le, "want code %s, got %v", CodeUnsupported, compileErr)
	assert.Equal(t, CodeUnsupported, le.Code)

	eval, err := core.NewEvaluatorWithDialect(clDialect)
	require.NoError(t, err)
	env := core.NewEnv(nil)
	_, err = eval.Eval(t.Context(), forms[0], env)
	require.NoError(t, err, "define defun")
	_, err = eval.Eval(t.Context(), core.NewList([]core.Value{core.Symbol{V: "outer"}}), env)
	require.NoError(t, err, "run defun body")
	_, leaked := env.Get("leaked")
	assert.False(t, leaked, "def in a defun body must not reach the root env")
}

// TestCompilerLetInitializerScopedDefFallback covers definitions in let/let*
// binding initializers: an initializer evaluates inside the scope it opens,
// so its def binds the enclosing scope under the tree-walker and the
// compiler must refuse the form, not emit a global store.
func TestCompilerLetInitializerScopedDefFallback(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`(let [x (def leaked 1)] x)`, `(let* [x (def leaked 1)] x)`} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			forms, err := core.Read(src)
			require.NoError(t, err)
			compileErr := NewCompiler("test").Compile(forms[0])
			var le *core.LispicoError
			require.ErrorAs(t, compileErr, &le, "%s: want code %s, got %v", src, CodeUnsupported, compileErr)
			assert.Equal(t, CodeUnsupported, le.Code)
		})
	}

	forms, err := core.Read(`(let [x (def leaked 1)] x)`)
	require.NoError(t, err)
	env := core.NewEnv(nil)
	got, err := core.NewEvaluator().Eval(t.Context(), forms[0], env)
	require.NoError(t, err)
	require.True(t, got.Equals(core.Int{V: 1}), "tree-walker result = %v", got)
	_, leaked := env.Get("leaked")
	assert.False(t, leaked, "let-initializer def must not reach the root env")
}

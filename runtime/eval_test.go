// Tests using bracket literals are pinned to Clojure; the default flips to Common Lisp in shard-C.

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestEval_SimpleExpressions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"integer", "42", core.Int{V: 42}},
		{"float", "3.14", core.Float{V: 3.14}},
		{"string", `"hello"`, core.String{V: "hello"}},
		{"nil", "nil", core.Nil{}},
		{"true", "true", core.Bool{V: true}},
		{"false", "false", core.Bool{V: false}},
		{"keyword", ":foo", core.Keyword{V: "foo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := New(nil)
			require.NoError(t, err)
			defer e.Close()

			result, err := e.Eval(context.Background(), "test", tt.input)
			require.NoError(t, err)
			assert.True(t, tt.expected.Equals(result), "expected %v, got %v", tt.expected, result)
		})
	}
}

func TestEval_Arithmetic(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	t.Run("addition", func(t *testing.T) {
		bindBuiltin(e, "+")
		result, err := e.Eval(context.Background(), "test", "(+ 1 2)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 3}.Equals(result))
	})

	t.Run("nested", func(t *testing.T) {
		bindBuiltin(e, "+")
		bindBuiltin(e, "*")
		result, err := e.Eval(context.Background(), "test", "(+ (* 2 3) 4)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 10}.Equals(result))
	})
}

func TestEval_UndefinedVariable(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "test", "undefined-var")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")
}

func TestEvalFile_ValidFile(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.lisp")
	content := `(def x 42)`
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	result, err := e.EvalFile(filePath)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(result))

	val, ok := e.RootEnv().Get("x")
	require.True(t, ok)
	assert.True(t, core.Int{V: 42}.Equals(val))
}

func TestEvalFile_NonExistent(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	_, err = e.EvalFile("/nonexistent/path/file.lisp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestLoadDir_Alphabetical(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	tmpDir := t.TempDir()

	files := map[string]string{
		"a.lisp": `(def order [])`,
		"b.lisp": `(def order (conj order "b"))`,
		"c.lisp": `(def order (conj order "c"))`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644))
	}

	bindBuiltin(e, "conj")
	require.NoError(t, e.LoadDir(tmpDir))

	val, ok := e.RootEnv().Get("order")
	require.True(t, ok)
	vec, ok := val.(core.Vector)
	require.True(t, ok)
	require.Len(t, vec.Items, 2)
	assert.True(t, core.String{V: "b"}.Equals(vec.Items[0]))
	assert.True(t, core.String{V: "c"}.Equals(vec.Items[1]))
}

func TestLoadDir_SkipsNonLisp(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("not lisp"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "valid.lisp"), []byte("(def x 1)"), 0644))

	require.NoError(t, e.LoadDir(tmpDir))

	_, ok := e.RootEnv().Get("x")
	assert.True(t, ok)
}

func TestCall_DefinedFunction(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "test", "(defn add [a b] (+ a b))")
	require.NoError(t, err)

	bindBuiltin(e, "+")
	result, err := e.Call(context.Background(), "add", core.Int{V: 2}, core.Int{V: 3})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 5}.Equals(result))
}

func TestCall_UndefinedFunction(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Call(context.Background(), "nonexistent", core.Int{V: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined function")
}

func TestCall_ContextCancellation(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "test", "(defn slow [] (loop [n 1000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)

	bindBuiltin(e, "=")
	bindBuiltin(e, "-")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = e.Call(ctx, "slow")
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestCall_Timeout(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "test", "(defn slow [] (loop [n 1000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)

	bindBuiltin(e, "=")
	bindBuiltin(e, "-")

	ctx := context.Background()
	_, err = e.Call(ctx, "slow")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestEval_Timeout(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(e, "=")
	bindBuiltin(e, "-")

	_, err = e.Eval(context.Background(), "test", "(loop [n 1000000] (if (= n 0) n (recur (- n 1))))")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestBind_CreatesBinding(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Bind("my-var", core.Int{V: 42}))

	val, ok := e.RootEnv().Get("my-var")
	require.True(t, ok)
	assert.True(t, core.Int{V: 42}.Equals(val))
}

func TestBind_NamespaceConflict(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	mockPlugin := &testPlugin{name: "llm"}
	require.NoError(t, e.Registry().Register(mockPlugin))

	err = e.Bind("llm/complete", core.String{V: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts with registered plugin namespace")

	err = e.Bind("llm", core.String{V: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts with registered plugin namespace")
}

func TestEvalWithBindings_Isolation(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	bindings := map[string]core.Value{
		"x": core.Int{V: 100},
	}

	result, err := e.EvalWithBindings(context.Background(), "x", bindings)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 100}.Equals(result))

	_, ok := e.RootEnv().Get("x")
	assert.False(t, ok, "binding should not leak to root env")
}

func TestEvalWithBindings_DoesNotAffectRoot(t *testing.T) {
	e, err := New(nil)
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Bind("root-var", core.Int{V: 1}))

	bindings := map[string]core.Value{
		"local-var": core.Int{V: 2},
	}

	result, err := e.EvalWithBindings(context.Background(), "(do root-var local-var)", bindings)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(result))

	_, ok := e.RootEnv().Get("local-var")
	assert.False(t, ok)
}

type testPlugin struct {
	name string
}

func (p *testPlugin) Name() string           { return p.name }
func (p *testPlugin) Init(_ *core.Env) error { return nil }
func (p *testPlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{Description: "test plugin"}
}

func bindBuiltin(e Engine, name string) {
	switch name {
	case "+":
		e.RootEnv().Set("+", core.GoFunc{
			Name: "+",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				sum := int64(0)
				for _, arg := range args {
					if i, ok := arg.(core.Int); ok {
						sum += i.V
					}
				}
				return core.Int{V: sum}, nil
			},
		})
	case "*":
		e.RootEnv().Set("*", core.GoFunc{
			Name: "*",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				prod := int64(1)
				for _, arg := range args {
					if i, ok := arg.(core.Int); ok {
						prod *= i.V
					}
				}
				return core.Int{V: prod}, nil
			},
		})
	case "-":
		e.RootEnv().Set("-", core.GoFunc{
			Name: "-",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				if len(args) == 0 {
					return core.Int{V: 0}, nil
				}
				result := args[0].(core.Int).V
				for _, arg := range args[1:] {
					result -= arg.(core.Int).V
				}
				return core.Int{V: result}, nil
			},
		})
	case "=":
		e.RootEnv().Set("=", core.GoFunc{
			Name: "=",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				if len(args) < 2 {
					return core.Bool{V: true}, nil
				}
				first := args[0]
				for _, arg := range args[1:] {
					if !first.Equals(arg) {
						return core.Bool{V: false}, nil
					}
				}
				return core.Bool{V: true}, nil
			},
		})
	case "conj":
		e.RootEnv().Set("conj", core.GoFunc{
			Name: "conj",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				if len(args) < 2 {
					return args[0], nil
				}
				coll := args[0]
				if vec, ok := coll.(core.Vector); ok {
					items := make([]core.Value, len(vec.Items))
					copy(items, vec.Items)
					items = append(items, args[1:]...)
					return core.Vector{Items: items}, nil
				}
				return coll, nil
			},
		})
	}
}

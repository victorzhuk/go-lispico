// Tests using bracket literals are pinned to Clojure; the default flips to Common Lisp in shard-C.

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
		bindBuiltin(t, e, "+")
		result, err := e.Eval(context.Background(), "test", "(+ 1 2)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 3}.Equals(result))
	})

	t.Run("nested", func(t *testing.T) {
		bindBuiltin(t, e, "+")
		bindBuiltin(t, e, "*")
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
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

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
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644))
	}

	bindBuiltin(t, e, "conj")
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

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("not lisp"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "valid.lisp"), []byte("(def x 1)"), 0o644))

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

	bindBuiltin(t, e, "+")
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

	bindBuiltin(t, e, "=")
	bindBuiltin(t, e, "-")

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

	bindBuiltin(t, e, "=")
	bindBuiltin(t, e, "-")

	ctx := context.Background()
	_, err = e.Call(ctx, "slow")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestEval_Timeout(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "=")
	bindBuiltin(t, e, "-")

	_, err = e.Eval(context.Background(), "test", "(loop [n 1000000] (if (= n 0) n (recur (- n 1))))")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// deadlineCapture records the deadline observed on the context passed into a
// GoFunc call, so tests can assert which deadline actually governs evaluation
// without waiting it out.
type deadlineCapture struct {
	mu       sync.Mutex
	deadline time.Time
	ok       bool
}

func (c *deadlineCapture) record(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline, c.ok = ctx.Deadline()
}

func (c *deadlineCapture) result() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadline, c.ok
}

func bindDeadlineCapture(t testing.TB, e Engine, name string) *deadlineCapture {
	t.Helper()
	c := &deadlineCapture{}
	err := e.Bind(name, core.GoFunc{
		Name: name,
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			c.record(ctx)
			return core.Nil{}, nil
		},
	})
	require.NoError(t, err)
	return c
}

type invokeFn func(ctx context.Context, e Engine) error

func evalInvoke(source string) invokeFn {
	return func(ctx context.Context, e Engine) error {
		_, err := e.Eval(ctx, "test", source)
		return err
	}
}

func callInvoke(name string) invokeFn {
	return func(ctx context.Context, e Engine) error {
		_, err := e.Call(ctx, name)
		return err
	}
}

func evalWithBindingsInvoke(source string) invokeFn {
	return func(ctx context.Context, e Engine) error {
		_, err := e.EvalWithBindings(ctx, source, nil)
		return err
	}
}

func deadlineInvocations() map[string]invokeFn {
	return map[string]invokeFn{
		"Eval":             evalInvoke("(capture)"),
		"Call":             callInvoke("capture"),
		"EvalWithBindings": evalWithBindingsInvoke("(capture)"),
	}
}

func TestEngineDeadline_CallerEarlierGovernsAlone(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			callerDeadline := time.Now().Add(2 * time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
			defer cancel()

			require.NoError(t, invoke(ctx, e))

			got, ok := capture.result()
			require.True(t, ok, "expected a deadline on the evaluation context")
			assert.WithinDuration(t, callerDeadline, got, 20*time.Millisecond)
		})
	}
}

func TestEngineDeadline_CallerLaterEngineBoundApplies(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(50*time.Millisecond))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			start := time.Now()
			require.NoError(t, invoke(ctx, e))

			got, ok := capture.result()
			require.True(t, ok, "expected a deadline on the evaluation context")
			assert.WithinDuration(t, start.Add(50*time.Millisecond), got, 30*time.Millisecond)
		})
	}
}

func TestEngineDeadline_ZeroTimeoutNoCallerDeadlineUnbounded(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(0))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			require.NoError(t, invoke(context.Background(), e))

			_, ok := capture.result()
			assert.False(t, ok, "expected no deadline when Engine timeout is disabled and caller has none")
		})
	}
}

func TestEngineDeadline_ZeroTimeoutCallerDeadlineUnchanged(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(0))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			callerDeadline := time.Now().Add(3 * time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
			defer cancel()

			require.NoError(t, invoke(ctx, e))

			got, ok := capture.result()
			require.True(t, ok, "expected the caller's deadline to survive unchanged")
			assert.WithinDuration(t, callerDeadline, got, 20*time.Millisecond)
		})
	}
}

func TestEngineDeadline_DefaultConstructionKeeps30sBound(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			start := time.Now()
			require.NoError(t, invoke(context.Background(), e))

			got, ok := capture.result()
			require.True(t, ok, "expected the Engine's default deadline")
			assert.WithinDuration(t, start.Add(30*time.Second), got, 500*time.Millisecond)
		})
	}
}

func TestEngineImpl_WithEvalTimeout(t *testing.T) {
	t.Run("caller deadline earlier returns ctx unchanged, no timer", func(t *testing.T) {
		e, err := New(nil)
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		got, gotCancel := impl.withEvalTimeout(ctx)
		defer gotCancel()
		assert.True(t, got == ctx, "expected the caller's context unchanged")
	})

	t.Run("caller deadline later wraps with Engine's tighter timeout", func(t *testing.T) {
		e, err := New(nil, WithTimeout(50*time.Millisecond))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		got, gotCancel := impl.withEvalTimeout(ctx)
		defer gotCancel()
		assert.False(t, got == ctx, "expected a new context wrapping the Engine's tighter deadline")
		deadline, ok := got.Deadline()
		require.True(t, ok)
		assert.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 20*time.Millisecond)
	})

	t.Run("no caller deadline wraps with Engine's timeout", func(t *testing.T) {
		e, err := New(nil, WithTimeout(50*time.Millisecond))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		got, gotCancel := impl.withEvalTimeout(context.Background())
		defer gotCancel()
		assert.False(t, got == context.Background())
		_, ok := got.Deadline()
		assert.True(t, ok)
	})

	t.Run("zero timeout returns ctx unchanged regardless of caller deadline", func(t *testing.T) {
		e, err := New(nil, WithTimeout(0))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		got, gotCancel := impl.withEvalTimeout(ctx)
		defer gotCancel()
		assert.True(t, got == ctx, "expected the Engine deadline to stay disabled")
	})
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
	// Pinned to Clojure dialect: source uses (do ...) which CL renames to (progn).
	e, err := New(nil, WithDialect(clojure.Dialect()))
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

func bindBuiltin(t testing.TB, e Engine, name string) {
	t.Helper()
	switch name {
	case "+":
		if err := e.Bind("+", core.GoFunc{
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
		}); err != nil {
			t.Fatalf("bindBuiltin %s: %v", name, err)
		}
	case "*":
		if err := e.Bind("*", core.GoFunc{
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
		}); err != nil {
			t.Fatalf("bindBuiltin %s: %v", name, err)
		}
	case "-":
		if err := e.Bind("-", core.GoFunc{
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
		}); err != nil {
			t.Fatalf("bindBuiltin %s: %v", name, err)
		}
	case "=":
		if err := e.Bind("=", core.GoFunc{
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
		}); err != nil {
			t.Fatalf("bindBuiltin %s: %v", name, err)
		}
	case "conj":
		if err := e.Bind("conj", core.GoFunc{
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
		}); err != nil {
			t.Fatalf("bindBuiltin %s: %v", name, err)
		}
	}
}

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

// TestEngineDeadline_CallerLaterEngineBoundApplies confirms the GoFunc now
// sees the caller's own (later) ctx deadline unwrapped — the Engine no longer
// layers a second ctx bound on top of it (that bound is enforced off-context,
// see TestEngineDeadline_CallerLaterEngineBoundStillGovernsEvaluation below).
func TestEngineDeadline_CallerLaterEngineBoundApplies(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(50*time.Millisecond))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			callerDeadline := time.Now().Add(5 * time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
			defer cancel()

			require.NoError(t, invoke(ctx, e))

			got, ok := capture.result()
			require.True(t, ok, "expected the caller's own deadline on the evaluation context")
			assert.WithinDuration(t, callerDeadline, got, 20*time.Millisecond)
		})
	}
}

// TestEngineDeadline_CallerLaterEngineBoundStillGovernsEvaluation confirms
// that even though a GoFunc no longer observes the Engine's tighter deadline
// as a context deadline, it still governs evaluation via the evaluators'
// batched cancellation checks — a long loop still terminates with
// context.DeadlineExceeded at the Engine's bound, not the caller's later one.
func TestEngineDeadline_CallerLaterEngineBoundStillGovernsEvaluation(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithTimeout(20*time.Millisecond))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "=")
	bindBuiltin(t, e, "-")

	_, err = e.Eval(context.Background(), "test", "(defn slow [] (loop [n 1000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = e.Call(ctx, "slow")
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
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

// TestEngineDeadline_DefaultConstructionKeeps30sBound confirms that under
// default construction the GoFunc's ctx carries no deadline at all — the
// Engine's 30s default is enforced off-context by the evaluators' batched
// checks now, not by wrapping ctx in a timer. The 30s bound itself is
// verified directly against evalDeadline in TestEngineImpl_EvalDeadline.
func TestEngineDeadline_DefaultConstructionKeeps30sBound(t *testing.T) {
	for name, invoke := range deadlineInvocations() {
		t.Run(name, func(t *testing.T) {
			e, err := New(nil, WithDialect(clojure.Dialect()))
			require.NoError(t, err)
			defer e.Close()

			capture := bindDeadlineCapture(t, e, "capture")

			require.NoError(t, invoke(context.Background(), e))

			_, ok := capture.result()
			assert.False(t, ok, "expected no context deadline on the GoFunc ctx under the Engine's default timeout")
		})
	}
}

// TestEngineImpl_EvalDeadline unit-tests evalDeadline's instant computation
// directly, now that the Engine deadline is no longer observable as a ctx
// wrapper (see the deadline-invocation tests above).
func TestEngineImpl_EvalDeadline(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("timeout disabled returns zero", func(t *testing.T) {
		e, err := New(nil, WithTimeout(0))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		got := impl.evalDeadline(context.Background(), start)
		assert.True(t, got.IsZero())
	})

	t.Run("no caller deadline returns start plus timeout", func(t *testing.T) {
		e, err := New(nil, WithTimeout(50*time.Millisecond))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		got := impl.evalDeadline(context.Background(), start)
		assert.Equal(t, start.Add(50*time.Millisecond), got)
	})

	t.Run("caller deadline earlier or equal returns zero", func(t *testing.T) {
		e, err := New(nil, WithTimeout(50*time.Millisecond))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		ctx, cancel := context.WithDeadline(context.Background(), start.Add(50*time.Millisecond))
		defer cancel()

		got := impl.evalDeadline(ctx, start)
		assert.True(t, got.IsZero())
	})

	t.Run("caller deadline later returns start plus timeout", func(t *testing.T) {
		e, err := New(nil, WithTimeout(50*time.Millisecond))
		require.NoError(t, err)
		defer e.Close()
		impl := e.(*engineImpl)

		ctx, cancel := context.WithDeadline(context.Background(), start.Add(time.Hour))
		defer cancel()

		got := impl.evalDeadline(ctx, start)
		assert.Equal(t, start.Add(50*time.Millisecond), got)
	})
}

// TestEngineDeadline_NoPerCallTimerAllocation confirms Call and Eval no
// longer pay for a per-call context.WithTimeout timer (timerCtx, runtime
// timer, Done channel, cleanup goroutine) when the Engine's default timeout
// applies and the caller holds no deadline of its own. Thresholds are the
// measured allocs/op on this change (Call: 2, Eval: 7, both with
// testing.AllocsPerRun(1000, ...)) plus one alloc of headroom for noise; a
// regression back to a per-call timer would push both well past these
// ceilings.
func TestEngineDeadline_NoPerCallTimerAllocation(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Bind("noop", core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}))

	ctx := context.Background()

	callAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.Call(ctx, "noop")
	})
	assert.LessOrEqual(t, callAllocs, float64(3))

	evalAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.Eval(ctx, "test", "42")
	})
	assert.LessOrEqual(t, evalAllocs, float64(8))
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

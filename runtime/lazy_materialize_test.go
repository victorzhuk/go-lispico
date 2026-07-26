package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestLazyMaterialize_DefersUntilFirstUse(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	impl := eng.(*engineImpl)
	require.NoError(t, eng.Use(stdlib.New()))
	afterUse := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, 0, afterUse, "Use must not force materialization")

	v, err := eng.Eval(context.Background(), "first-use", "(str \"a\" \"b\")")
	require.NoError(t, err)
	require.Equal(t, "\"ab\"", v.String())
	afterFirstEval := impl.lazyMaterializer.MaterializeCount()
	assert.Greater(t, afterFirstEval, afterUse, "first use must materialize")

	_, err = eng.Eval(context.Background(), "second-use", "(str \"a\" \"b\")")
	require.NoError(t, err)
	afterSecondEval := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, afterFirstEval, afterSecondEval,
		"second use must not re-materialize")
}

func TestLazyMaterialize_ConcurrentFirstTouch(t *testing.T) {
	t.Parallel()

	const goroutines = 64

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	deadline := time.Now().Add(10 * time.Second)
	for range goroutines {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			v, err := eng.Eval(ctx, "race", "(str \"a\" \"b\")")
			assert.NoError(t, err)
			assert.Equal(t, "\"ab\"", v.String())
		}()
	}
	wg.Wait()

	after := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, 1, after,
		"materialize count must be exactly 1 across concurrent first-touches")
}

func TestLazyMaterialize_DisjointNamesInParallel(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	// Each goroutine first-touches a different deferred stdlib name.
	work := []struct{ src, want string }{
		{"(+ 1 2)", "3"},
		{"(str \"a\" \"b\")", "\"ab\""},
		{"(count [1 2 3])", "3"},
		{"(reverse [1 2 3])", "(3 2 1)"},
		{"(nth [10 20] 1)", "20"},
		{"(sort [2 1])", "(1 2)"},
		{"(concat [1] [2])", "(1 2)"},
		{"(merge {:a 1} {:b 2})", "{:a 1 :b 2}"},
	}
	var wg sync.WaitGroup
	for _, w := range work {
		wg.Add(1)
		go func(src, want string) {
			defer wg.Done()
			v, err := eng.Eval(context.Background(), "disjoint", src)
			assert.NoError(t, err)
			assert.Equal(t, want, v.String(), "src=%s", src)
		}(w.src, w.want)
	}
	wg.Wait()

	impl := eng.(*engineImpl)
	assert.Equal(t, len(work), impl.lazyMaterializer.MaterializeCount(),
		"each disjoint name must materialize exactly once")
}

func TestLazyMaterialize_RaceSafeNoDeadlock(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := eng.Eval(ctx, "stress", "(str (count [1 2]) (reverse [3 4]))")
			cancel()
			if err != nil {
				t.Errorf("eval error under contention: %v", err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deadlock or hang detected under concurrent lazy materialization")
	}
}

func TestLazyMaterialize_ToggleDisableFallsBackToEager(t *testing.T) {
	restore := setStdlibLazyDisabledForTesting(true)
	defer restore()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)

	v, err := eng.Eval(context.Background(), "warm", "(str \"a\" \"b\")")
	require.NoError(t, err)
	require.Equal(t, "\"ab\"", v.String())

	after := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, 0, after,
		"disabled mode should fall back to eager and not record template materializations")
}

func TestLazyMaterialize_DeleteAfterShadowTombstones(t *testing.T) {
	// Serial: builds the eager reference under the process-global toggle.

	// Two-engine parity: lazy-on vs lazy-off (eager) must agree at every
	// step of shadow &rarr; delete &rarr; resolve.
	restore := setStdlibLazyDisabledForTesting(true)
	eager, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eager.Close() })
	require.NoError(t, eager.Use(stdlib.New()))
	restore()

	lazy, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lazy.Close() })
	require.NoError(t, lazy.Use(stdlib.New()))

	shadow := core.GoFunc{
		Name: "+",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 999}, nil
		},
	}
	require.NoError(t, eager.Bind("+", shadow))
	require.NoError(t, lazy.Bind("+", shadow))

	for name, eng := range map[string]Engine{"eager": eager, "lazy": lazy} {
		v, err := eng.Eval(context.Background(), "shadowed", "(+ 1 2)")
		assert.NoError(t, err, name)
		assert.True(t, core.Int{V: 999}.Equals(v), "%s: shadow must win", name)

		eng.RootEnv().Delete("+")
		_, err = eng.Eval(context.Background(), "after-delete", "(+ 1 2)")
		assert.Error(t, err, "%s: delete must not be undone by deferral", name)
		assert.Contains(t, err.Error(), "undefined", name)
	}
}

func TestLazyMaterialize_MacroFirstTouchFromVM(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Use(stdlib.New()))

	impl := eng.(*engineImpl)
	before := impl.lazyMaterializer.MaterializeCount()

	v, err := eng.Eval(context.Background(), "thread", "(-> 1 (+ 2) (* 3))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 9}.Equals(v))

	after := impl.lazyMaterializer.MaterializeCount()
	assert.Greater(t, after, before,
		"thread-first macro (and its dependencies) must materialize through MacroExpand")

	v2, err := eng.Eval(context.Background(), "thread-again", "(-> 1 (+ 2) (* 3))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 9}.Equals(v2))

	after2 := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, after, after2,
		"second macro expansion must not re-materialize")
}

// TestLazyMaterialize_PropagatesToSiblingEngine verifies the process-level
// template is shared (both engines resolve the deferred name) while
// materialization stays per-engine (each engine counts its own).
func TestLazyMaterialize_PropagatesToSiblingEngine(t *testing.T) {
	t.Parallel()

	eng1, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng1.Close() })
	eng2, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng2.Close() })

	require.NoError(t, eng1.Use(stdlib.New()))
	require.NoError(t, eng2.Use(stdlib.New()))

	v, err := eng1.Eval(context.Background(), "e1", "(str \"a\" \"b\")")
	require.NoError(t, err)
	require.Equal(t, "\"ab\"", v.String())
	assert.Equal(t, 1, eng1.(*engineImpl).lazyMaterializer.MaterializeCount())

	v, err = eng2.Eval(context.Background(), "e2", "(str \"a\" \"b\")")
	require.NoError(t, err)
	require.Equal(t, "\"ab\"", v.String())
	assert.Equal(t, 1, eng2.(*engineImpl).lazyMaterializer.MaterializeCount(),
		"sibling engine materializes into its own root env")
}

func TestLazyMaterialize_EquivalenceOnVsOff(t *testing.T) {
	// No t.Parallel: the disabled toggle is process-global and would leak
	// into parallel tests that depend on lazy materialization.

	corpus := []string{
		`(+ 1 2 3)`,
		`(- 10 (* 2 3))`,
		`(str "hello, " "world")`,
		`(let [x 10 y 20] (+ x y))`,
		`(map (fn [n] (* n 2)) [1 2 3 4])`,
		`(reduce + 0 [1 2 3 4 5])`,
		`(-> 1 (+ 2) (* 3))`,
		`(->> 5 (- 1) (/ 2))`,
		`(if-let [x (get {:a 1} :a)] (str "got: " x) "missing")`,
		`(when-let [x (get {:a 42} :a)] (* x 2))`,
		`(get-in {:a {:b {:c 7}}} [:a :b :c])`,
	}

	run := func(eng Engine) []string {
		results := make([]string, 0, len(corpus))
		for _, src := range corpus {
			v, err := eng.Eval(context.Background(), "src", src)
			require.NoError(t, err, "src=%s", src)
			results = append(results, v.String())
		}
		return results
	}

	// Lazy ON (default after Use).
	engOn, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engOn.Close() })
	require.NoError(t, engOn.Use(stdlib.New()))
	onResults := run(engOn)

	// Lazy OFF (toggle for the second engine only).
	restore := setStdlibLazyDisabledForTesting(true)
	defer restore()

	engOff, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engOff.Close() })
	require.NoError(t, engOff.Use(stdlib.New()))
	offResults := run(engOff)

	assert.Equal(t, onResults, offResults,
		"lazy on vs off must produce byte-identical results across the corpus")
}

// TestLazyMaterialize_EnumerationSeesFullSurface pins the spec scenario: an
// enumeration surface (RootEnv().VarNames/FuncNames) on an engine where
// nothing has been resolved yet reports the same bindings an eagerly loaded
// engine reports, forcing materialization as needed.
func TestLazyMaterialize_EnumerationSeesFullSurface(t *testing.T) {
	// Serial: uses the process-global disabled toggle for the eager reference.
	lazy, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lazy.Close() })
	require.NoError(t, lazy.Use(stdlib.New()))

	impl := lazy.(*engineImpl)
	before := impl.lazyMaterializer.MaterializeCount()

	lazyNames := lazy.RootEnv().VarNames()
	assert.Greater(t, impl.lazyMaterializer.MaterializeCount(), before,
		"enumeration must force the deferred template")

	restore := setStdlibLazyDisabledForTesting(true)
	defer restore()
	eager, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eager.Close() })
	require.NoError(t, eager.Use(stdlib.New()))

	assert.ElementsMatch(t, eager.RootEnv().VarNames(), lazyNames)
	assert.ElementsMatch(t, eager.RootEnv().FuncNames(), lazy.RootEnv().FuncNames())
}

// TestLazyMaterialize_EngineFuncOnStdlibNames pins Engine.Func resolving
// stdlib names right after Use — exercising the contract that the lazy
// layer covers CellLocal, Cell, FuncCell paths used by Engine.Func.
func TestLazyMaterialize_EngineFuncOnStdlibNames(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	for _, name := range []string{"+", "map", "reduce", "str", "get"} {
		fn, err := eng.Func(name)
		require.NoError(t, err, "Func(%q)", name)
		require.NotNil(t, fn, "Func(%q) returned nil handle", name)
	}
}

// TestLazyMaterialize_UnloadRemovesDeferredAndMaterialized pins the spec
// scenario: UnloadPlugin after SOME deferred names were materialized and
// others were not removes both — neither resolves afterwards.
func TestLazyMaterialize_UnloadRemovesDeferredAndMaterialized(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	// Materialize one name; leave the rest deferred.
	v, err := eng.Eval(context.Background(), "touch", "(map (fn [x] (+ x 1)) [1 2])")
	require.NoError(t, err)
	require.Equal(t, "(2 3)", v.String())

	require.NoError(t, eng.UnloadPlugin(""))

	for _, src := range []string{"(map (fn [x] x) [1])", "(reduce + 0 [1])"} {
		_, err = eng.Eval(context.Background(), "after-unload", src)
		assert.Error(t, err, "src=%s", src)
		assert.Contains(t, err.Error(), "undefined", "src=%s", src)
	}

	// Re-loading the plugin revives the full surface.
	require.NoError(t, eng.Use(stdlib.New()))
	v, err = eng.Eval(context.Background(), "revived", "(reduce + 0 [1 2 3])")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 6}.Equals(v))
}

// TestLazyMaterialize_ConcurrentDependentNames exercises first-touch of a
// deferred pure-Lisp definition whose body resolves other deferred names
// (get-in -> reduce/get) from many goroutines: every name materializes at
// most once, nobody deadlocks, and results stay correct. Run with -race.
func TestLazyMaterialize_ConcurrentDependentNames(t *testing.T) {
	t.Parallel()

	const goroutines = 32
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	deadline := time.Now().Add(15 * time.Second)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			v, err := eng.Eval(ctx, "dep", "(get-in {:a {:b {:c 7}}} [:a :b :c])")
			assert.NoError(t, err)
			assert.True(t, core.Int{V: 7}.Equals(v), "got %v", v)
		}()
	}
	wg.Wait()

	impl := eng.(*engineImpl)
	// get-in + its transitive first-touches (reduce, get) materialize once each.
	assert.LessOrEqual(t, impl.lazyMaterializer.MaterializeCount(), 4,
		"dependent names must materialize at most once each")
}

// TestLazyMaterialize_EquivalenceOnVsOff_CL runs the on/off equivalence
// under the CL dialect (Lisp-2, vocabulary renames, reader flags) so the
// func-cell mirroring and vocab alias paths are defended in both modes.
// List-only syntax: the CL reader disables bracket literals.
func TestLazyMaterialize_EquivalenceOnVsOff_CL(t *testing.T) {
	// Serial: process-global disabled toggle.
	corpus := []string{
		`(+ 1 2 3)`,
		`(car (list 1 2 3))`,
		`(cdr (list 1 2 3))`,
		`(mapcar (fn (x) (* x 2)) (list 1 2 3))`,
		`(defun double (x) (* x 2))`,
		`(double 21)`,
		`(if nil :yes :no)`,
		`(get-in (hash-map :a (hash-map :b 5)) (list :a :b))`,
	}

	run := func(eng Engine) []string {
		results := make([]string, 0, len(corpus))
		for _, src := range corpus {
			v, err := eng.Eval(context.Background(), "src", src)
			require.NoError(t, err, "src=%s", src)
			results = append(results, v.String())
		}
		return results
	}

	lazyOn, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lazyOn.Close() })
	require.NoError(t, lazyOn.Use(stdlib.New()))
	onResults := run(lazyOn)

	restore := setStdlibLazyDisabledForTesting(true)
	defer restore()
	lazyOff, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lazyOff.Close() })
	require.NoError(t, lazyOff.Use(stdlib.New()))
	offResults := run(lazyOff)

	assert.Equal(t, onResults, offResults,
		"CL lazy on vs off must produce byte-identical results across the corpus")
}

// TestLazyMaterialize_NativeOpParityOnOff pins the canonical-flag contract
// under both modes: a defun rebind of a canonical operator must clear it for
// the VM exactly as under eager load (the tree-walker is the reference).
func TestLazyMaterialize_NativeOpParityOnOff(t *testing.T) {
	// Serial: process-global disabled toggle.
	cases := []struct {
		redef, call string
		want        core.Value
	}{
		{"(defun + (a b) (- a b))", "(+ 5 3)", core.Int{V: 2}},
		{"(defun < (a b) true)", "(< 5 3)", core.Bool{V: true}},
	}
	for _, lazyDisabled := range []bool{false, true} {
		restore := setStdlibLazyDisabledForTesting(lazyDisabled)
		eng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
		require.NoError(t, err)
		require.NoError(t, eng.Use(stdlib.New()))
		for _, tc := range cases {
			_, err = eng.Eval(context.Background(), "redef", tc.redef)
			require.NoError(t, err)
			got, err := eng.Eval(context.Background(), "call", tc.call)
			require.NoError(t, err)
			assert.True(t, got.Equals(tc.want), "lazyDisabled=%v redef=%s got %v", lazyDisabled, tc.redef, got)
		}
		// A canonical op never rebound must still take the fused path with
		// identical results in both modes.
		got, err := eng.Eval(context.Background(), "plain", "(* 6 7)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 42}.Equals(got))
		require.NoError(t, eng.Close())
		restore()
	}
}

// errSharedTemplateInit is returned by sharedTemplatePlugin when constructed
// with fail: true, to exercise the failed-Init retry path without any real
// dependency failure.
var errSharedTemplateInit = errors.New("shared template init failed")

// sharedTemplatePlugin mirrors stdlib's namespace shape (Name() == "") so its
// registration routes through the process-level template registry exactly
// like plugins/stdlib does — the structural condition RegisterValue checks
// before deferring. inits counts real Init executions (shared across
// instances that must resolve to one process-level layer); fail forces this
// instance's Init to error before it ever registers anything.
type sharedTemplatePlugin struct {
	version string
	inits   *int64
	fail    bool
}

func (p *sharedTemplatePlugin) Name() string { return "" }

func (p *sharedTemplatePlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{Version: p.version}
}

func (p *sharedTemplatePlugin) Init(env *core.Env) error {
	atomic.AddInt64(p.inits, 1)
	if p.fail {
		return errSharedTemplateInit
	}
	return env.RegisterValue("shared-template-fn", core.GoFunc{
		Name: "shared-template-fn",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 42}, nil
		},
	}, false)
}

// TestLazyMaterialize_SecondEngineSkipsInit pins the counter proof: Init runs
// exactly once across N engines that share one dialect fingerprint and
// plugin identity, even though each engine calls Use independently.
func TestLazyMaterialize_SecondEngineSkipsInit(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-skip-init", "if")
	var inits int64

	const engines = 4
	for range engines {
		eng, err := New(nil, WithBytecode(), WithDialect(dialect))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		require.NoError(t, eng.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits}))
	}

	assert.Equal(t, int64(1), atomic.LoadInt64(&inits),
		"Init must run exactly once across engines sharing one dialect fingerprint and plugin identity")
}

// TestLazyMaterialize_SecondEngineSharesClosurePointers pins the
// pointer-identity proof: the materialized core.GoFunc on a second engine is
// the exact same underlying func as on the first, not a re-built duplicate.
func TestLazyMaterialize_SecondEngineSharesClosurePointers(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-shared-ptr", "if")
	var inits int64

	engA, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engA.Close() })
	require.NoError(t, engA.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits}))

	engB, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engB.Close() })
	require.NoError(t, engB.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits}))

	_, err = engA.Eval(context.Background(), "a", "(shared-template-fn)")
	require.NoError(t, err)
	_, err = engB.Eval(context.Background(), "b", "(shared-template-fn)")
	require.NoError(t, err)

	vA, ok := engA.RootEnv().Get("shared-template-fn")
	require.True(t, ok)
	vB, ok := engB.RootEnv().Get("shared-template-fn")
	require.True(t, ok)

	fnA, ok := vA.(core.GoFunc)
	require.True(t, ok)
	fnB, ok := vB.(core.GoFunc)
	require.True(t, ok)

	assert.Equal(t, reflect.ValueOf(fnA.Fn).Pointer(), reflect.ValueOf(fnB.Fn).Pointer(),
		"materialized closures on both engines must be the same underlying func")
	assert.Equal(t, int64(1), atomic.LoadInt64(&inits))
}

// TestLazyMaterialize_FailedInitLeavesLayerIncomplete pins retry-after-failure:
// a failing first Init never completes the layer, and the next Use (now
// succeeding) builds it cleanly with no residue from the failed attempt.
func TestLazyMaterialize_FailedInitLeavesLayerIncomplete(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-failed-init", "if")
	var inits int64

	eng1, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng1.Close() })
	err = eng1.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits, fail: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, errSharedTemplateInit)

	eng2, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng2.Close() })
	require.NoError(t, eng2.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits}))

	v, err := eng2.Eval(context.Background(), "ok", "(shared-template-fn)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(v))

	assert.Equal(t, int64(2), atomic.LoadInt64(&inits),
		"the failed attempt and the successful retry both ran Init")
}

// TestLazyMaterialize_DifferentVersionsGetDistinctLayers pins task 2.3: two
// versions of the same (empty) plugin name never share a layer, each builds
// independently, and a later engine at a version already built attaches
// that version's layer specifically, not the other one's.
func TestLazyMaterialize_DifferentVersionsGetDistinctLayers(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-distinct-versions", "if")
	var initsV1, initsV2 int64

	eng1, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng1.Close() })
	require.NoError(t, eng1.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &initsV1}))

	eng2, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng2.Close() })
	require.NoError(t, eng2.Use(&sharedTemplatePlugin{version: "2.0.0", inits: &initsV2}))

	v1, err := eng1.Eval(context.Background(), "v1", "(shared-template-fn)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(v1))
	v2, err := eng2.Eval(context.Background(), "v2", "(shared-template-fn)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(v2))

	assert.Equal(t, int64(1), atomic.LoadInt64(&initsV1))
	assert.Equal(t, int64(1), atomic.LoadInt64(&initsV2),
		"v2's build must not be skipped by v1's completion")

	eng3, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng3.Close() })
	require.NoError(t, eng3.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &initsV1}))
	v3, err := eng3.Eval(context.Background(), "v3", "(shared-template-fn)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(v3))
	assert.Equal(t, int64(1), atomic.LoadInt64(&initsV1),
		"a third engine at v1 attaches v1's existing layer, not v2's, with no re-Init")
}

// TestLazyMaterialize_ConcurrentFirstUseBuildsOnce pins the spec scenario
// "Concurrent first loads build one layer": many engines with one dialect
// fingerprint racing their first Use of the same plugin identity build the
// layer exactly once, and every engine still evaluates correctly. Run with
// -race.
func TestLazyMaterialize_ConcurrentFirstUseBuildsOnce(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-concurrent-use", "if")
	var inits int64

	const engines = 32
	made := make([]Engine, engines)
	var wg sync.WaitGroup
	wg.Add(engines)
	for i := range engines {
		go func(i int) {
			defer wg.Done()
			eng, err := New(nil, WithBytecode(), WithDialect(dialect))
			assert.NoError(t, err)
			assert.NoError(t, eng.Use(&sharedTemplatePlugin{version: "1.0.0", inits: &inits}))
			made[i] = eng
		}(i)
	}
	wg.Wait()

	for i, eng := range made {
		t.Cleanup(func() { _ = eng.Close() })
		v, err := eng.Eval(context.Background(), "concurrent", "(shared-template-fn)")
		require.NoError(t, err, "engine %d", i)
		assert.True(t, core.Int{V: 42}.Equals(v), "engine %d", i)
	}

	assert.Equal(t, int64(1), atomic.LoadInt64(&inits),
		"concurrent first Use of one key must build the layer exactly once")
}

// TestLazyMaterialize_EnumerationIdenticalFirstAndSecondEngine pins the
// spec's enumeration scenario across roles: a second engine that only
// attached the layer must enumerate the same surface as the first engine
// that built it.
func TestLazyMaterialize_EnumerationIdenticalFirstAndSecondEngine(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-enum-parity", "if")

	first, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.Use(stdlib.New()))
	firstVars := first.RootEnv().VarNames()
	firstFuncs := first.RootEnv().FuncNames()

	second, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.Use(stdlib.New()))
	secondVars := second.RootEnv().VarNames()
	secondFuncs := second.RootEnv().FuncNames()

	assert.ElementsMatch(t, firstVars, secondVars,
		"second engine (attached layer) must enumerate the same surface as the first (built it)")
	assert.ElementsMatch(t, firstFuncs, secondFuncs)
}

// TestLazyMaterialize_ShadowAndDeleteIdenticalFirstAndSecondEngine pins
// shadow-then-delete parity across roles instead of across the eager/lazy
// toggle: a second (attaching) engine must behave exactly like the first
// (building) engine when a deferred name is shadowed and then deleted.
func TestLazyMaterialize_ShadowAndDeleteIdenticalFirstAndSecondEngine(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-shadow-delete-parity", "if")

	first, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.Use(stdlib.New()))

	second, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.Use(stdlib.New()))

	shadow := core.GoFunc{
		Name: "+",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 999}, nil
		},
	}

	for name, eng := range map[string]Engine{"first": first, "second": second} {
		require.NoError(t, eng.Bind("+", shadow), name)
		v, err := eng.Eval(context.Background(), "shadowed", "(+ 1 2)")
		require.NoError(t, err, name)
		assert.True(t, core.Int{V: 999}.Equals(v), "%s: shadow must win", name)

		eng.RootEnv().Delete("+")
		_, err = eng.Eval(context.Background(), "after-delete", "(+ 1 2)")
		assert.Error(t, err, "%s: delete must not be undone by deferral", name)
		assert.Contains(t, err.Error(), "undefined", name)
	}
}

// TestLazyMaterialize_UnloadIdenticalFirstAndSecondEngine pins the spec
// scenario "Unload removes deferred and materialized bindings": unloading
// the layer-building first engine removes its own bindings only, a sibling
// second engine sharing the layer stays unaffected, and a third engine can
// still attach the layer afterward without rebuilding it.
func TestLazyMaterialize_UnloadIdenticalFirstAndSecondEngine(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-unload-parity", "if")

	first, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.Use(stdlib.New()))

	second, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.Use(stdlib.New()))

	for _, eng := range []Engine{first, second} {
		v, err := eng.Eval(context.Background(), "touch", "(+ 1 2)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 3}.Equals(v))
	}

	require.NoError(t, first.UnloadPlugin(""))
	for _, src := range []string{"(+ 1 2)", "(reduce + 0 [1])"} {
		_, err := first.Eval(context.Background(), "after-unload", src)
		assert.Error(t, err, "src=%s", src)
		assert.Contains(t, err.Error(), "undefined", "src=%s", src)
	}

	v, err := second.Eval(context.Background(), "sibling-untouched", "(reduce + 0 [1 2 3])")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 6}.Equals(v),
		"second engine must be unaffected by unloading the layer-building first engine")

	third, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = third.Close() })
	require.NoError(t, third.Use(stdlib.New()))
	v, err = third.Eval(context.Background(), "third", "(str \"a\" \"b\")")
	require.NoError(t, err)
	assert.Equal(t, "\"ab\"", v.String(),
		"a third engine still attaches the shared layer after a sibling's unload")
}

// TestLazyMaterialize_ReloadIdenticalFirstAndSecondEngine pins hot-reload
// parity: reloading the same plugin identity attaches the already-complete
// layer on both a first (builder) and second (attacher) engine, and
// evaluation keeps working identically on either side afterward.
func TestLazyMaterialize_ReloadIdenticalFirstAndSecondEngine(t *testing.T) {
	t.Parallel()

	dialect := clojure.Dialect().Add("lazy-template-reload-parity", "if")

	first, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.Use(stdlib.New()))

	second, err := New(nil, WithBytecode(), WithDialect(dialect))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.Use(stdlib.New()))

	require.NoError(t, first.ReloadPlugin(stdlib.New()))
	require.NoError(t, second.ReloadPlugin(stdlib.New()))

	for name, eng := range map[string]Engine{"first": first, "second": second} {
		v, err := eng.Eval(context.Background(), "after-reload", "(reduce + 0 [1 2 3 4])")
		require.NoError(t, err, name)
		assert.True(t, core.Int{V: 10}.Equals(v), name)
	}
}

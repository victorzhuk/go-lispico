package runtime

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"strings"
	"sync"
	"testing"
)

// ownerProbe is trusted host Go code wrapping the engine's real evaluator:
// Eval/Apply and DefineBootstrap delegate unchanged to the wrapped evaluator,
// while DefineBootstrap calls are counted, so tests can prove lazy bootstrap
// materialization routes through the env's installed owner.
type ownerProbe struct {
	mu     sync.Mutex
	inner  core.Evaluator
	count  int
	source []string
}

func newOwnerProbe(inner core.Evaluator) *ownerProbe {
	return &ownerProbe{inner: inner}
}

func (p *ownerProbe) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Eval(ctx, form, env)
}

func (p *ownerProbe) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Apply(ctx, fn, args, env)
}

func (p *ownerProbe) DefineBootstrap(ctx context.Context, source string, env *core.Env) (core.Value, error) {
	bd, ok := p.inner.(core.BootstrapDefiner)
	if !ok {
		return nil, fmt.Errorf("installed evaluator %T lacks core.BootstrapDefiner", p.inner)
	}
	p.mu.Lock()
	p.count++
	p.source = append(p.source, source)
	p.mu.Unlock()
	return bd.DefineBootstrap(ctx, source, env)
}

func (p *ownerProbe) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

func (p *ownerProbe) Sources() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.source...)
}

// TestStdlibLazyBootstrap_UsesInstalledOwner proves the lazy first touch of a
// deferred bootstrap name defines it through the env's installed owner —
// exactly one DefineBootstrap for the touched name — and publishes the cell
// the owner bound, never via a fresh identity Evaluator.
func TestStdlibLazyBootstrap_UsesInstalledOwner(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	impl := eng.(*engineImpl)
	require.NotNil(t, impl.bytecodeEvaluator, "engine must carry a bytecode evaluator")

	root := eng.RootEnv()
	probe := newOwnerProbe(root.Evaluator())
	root.SetEvaluator(probe)

	require.NoError(t, eng.Use(stdlib.New()))
	before := probe.Count()

	v, err := eng.Eval(context.Background(), "first-touch", "(-> 1 (+ 2))")
	require.NoError(t, err)
	require.Equal(t, "3", v.String())

	if got := probe.Count() - before; got != 1 {
		t.Fatalf("lazy first touch triggered %d DefineBootstrap calls, want exactly 1; materialization must route through the installed owner, not a fresh identity Evaluator", got)
	}

	found := false
	for _, src := range probe.Sources() {
		if strings.Contains(src, "(defmacro -> ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the touched name -> was not among the DefineBootstrap sources: %v", probe.Sources())
	}

	if root.Evaluator() != core.Evaluator(probe) {
		t.Fatalf("installed owner replaced during materialization: %T", root.Evaluator())
	}
	// The owner is the Lisp-1 (Clojure-identity) dialect: the value cell is
	// the single namespace, so first touch must publish there and must not
	// mirror the binding into the function cell.
	if _, ok := root.Get("->"); !ok {
		t.Fatalf("-> not published in the value cell after first touch under the Lisp-1 owner")
	}
	if _, ok := root.GetFunc("->"); ok {
		t.Fatalf("-> mirrored into the function cell after first touch under the Lisp-1 owner; the value cell is the single namespace")
	}
}


// TestStdlibLazyBootstrap_UsesInstalledOwnerCellFirstTouch pins the cell a
// first touch publishes for a deferred bootstrap name under each owner axis:
// the evaluator's lisp2 axis solely owns publication — Lisp-1 publishes the
// value cell only, Lisp-2 the function cell only — and the opposite cell
// stays untouched whichever position (value or function) triggered the miss.
func TestStdlibLazyBootstrap_UsesInstalledOwnerCellFirstTouch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		axis   string
		d      core.Dialect
		funcNS bool
		lookupFound bool
	}{
		{axis: "lisp1", d: clojure.Dialect(), funcNS: false, lookupFound: true},
		{axis: "lisp1", d: clojure.Dialect(), funcNS: true, lookupFound: false},
		{axis: "lisp2", d: core.FullDialect().Lisp2(), funcNS: false, lookupFound: false},
		{axis: "lisp2", d: core.FullDialect().Lisp2(), funcNS: true, lookupFound: true},
	}
	for _, tc := range cases {
		t.Run(tc.axis+"_first_touch_"+map[bool]string{false: "value", true: "func"}[tc.funcNS]+"_position", func(t *testing.T) {
			t.Parallel()

			eng, err := New(nil, WithBytecode(), WithDialect(tc.d))
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })
			require.NoError(t, eng.Use(stdlib.New()))
			root := eng.RootEnv()

			if tc.funcNS {
				if _, ok := root.GetFunc("->"); ok != tc.lookupFound {
					t.Fatalf("function-position first touch under %s owner: GetFunc(\"->\") found=%v, want %v", tc.axis, ok, tc.lookupFound)
				}
			} else {
				if _, ok := root.Get("->"); ok != tc.lookupFound {
					t.Fatalf("value-position first touch under %s owner: Get(\"->\") found=%v, want %v", tc.axis, ok, tc.lookupFound)
				}
			}

			if tc.d.IsLisp2() {
				if _, ok := root.GetFunc("->"); !ok {
					t.Fatalf("-> not published in the function cell after first touch under the Lisp-2 owner")
				}
				if _, ok := root.Get("->"); ok {
					t.Fatalf("-> leaked into the value cell under the Lisp-2 owner; the function cell is the single publication cell")
				}
			} else {
				if _, ok := root.Get("->"); !ok {
					t.Fatalf("-> not published in the value cell after first touch under the Lisp-1 owner")
				}
				if _, ok := root.GetFunc("->"); ok {
					t.Fatalf("-> mirrored into the function cell under the Lisp-1 owner; the value cell is the single namespace")
				}
			}
		})
	}
}

// TestStdlibLazyBootstrap_DivergentOwnerAxis pins that lazy first-touch
// publication follows the installed evaluator's lisp2 axis, not the engine
// config dialect: a Lisp-2-configured runtime with a Lisp-1 installed owner
// publishes the value cell only, and a Lisp-1-configured runtime with a
// Lisp-2 owner the function cell only.
func TestStdlibLazyBootstrap_DivergentOwnerAxis(t *testing.T) {
	t.Parallel()

	t.Run("lisp2_engine_lisp1_owner_value_cell_only", func(t *testing.T) {
		t.Parallel()

		eng, err := New(nil, WithBytecode(), WithDialect(core.FullDialect().Lisp2()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ownerEng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = ownerEng.Close() })

		root := eng.RootEnv()
		root.SetEvaluator(ownerEng.RootEnv().Evaluator())

		require.NoError(t, eng.Use(stdlib.New()))

		if _, ok := root.Get("->"); !ok {
			t.Fatalf("-> not published in the value cell after first touch under the Lisp-1 owner")
		}
		if _, ok := root.GetFunc("->"); ok {
			t.Fatalf("-> mirrored into the function cell under the Lisp-1 owner in a Lisp-2-configured runtime; the installed owner's axis, not the engine config dialect, owns publication")
		}
	})

	t.Run("lisp1_engine_lisp2_owner_function_cell_only", func(t *testing.T) {
		t.Parallel()

		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ownerEng, err := New(nil, WithBytecode(), WithDialect(core.FullDialect().Lisp2()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = ownerEng.Close() })

		root := eng.RootEnv()
		root.SetEvaluator(ownerEng.RootEnv().Evaluator())

		require.NoError(t, eng.Use(stdlib.New()))

		if _, ok := root.GetFunc("->"); !ok {
			t.Fatalf("-> not published in the function cell after first touch under the Lisp-2 owner")
		}
		if _, ok := root.Get("->"); ok {
			t.Fatalf("-> leaked into the value cell under the Lisp-2 owner in a Lisp-1-configured runtime; the installed owner's axis, not the engine config dialect, owns publication")
		}
	})
}

// TestBootstrapLazyConcurrentFirstTouch_PublishesOnce proves concurrent first
// touch of one deferred bootstrap name publishes exactly once (per-name
// MaterializeCount delta == 1) with the correct result, race-clean.
// Macro-expanding (-> ...) pulls other deferred names in, so the concurrency
// test touches exactly one bootstrap name via lookup misses — the layer's own
// first-touch path — and checks the published form works afterwards.
func TestBootstrapLazyConcurrentFirstTouch_PublishesOnce(t *testing.T) {
	t.Parallel()

	const goroutines = 64

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	impl := eng.(*engineImpl)
	root := eng.RootEnv()
	before := impl.lazyMaterializer.MaterializeCount()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if _, ok := root.Get("->"); !ok {
				t.Error("-> reported missing on concurrent first touch")
			}
		}()
	}
	wg.Wait()

	after := impl.lazyMaterializer.MaterializeCount()
	assert.Equal(t, 1, after-before,
		"concurrent first touch of one bootstrap name must materialize exactly once")

	if _, ok := root.Get("->"); !ok {
		t.Fatalf("-> not published in the value cell after concurrent first touch under the Lisp-1 owner")
	}
	if _, ok := root.GetFunc("->"); ok {
		t.Fatalf("-> mirrored into the function cell after concurrent first touch under the Lisp-1 owner; the value cell is the single namespace")
	}
	v, err := eng.Eval(context.Background(), "use", "(-> 1 (+ 2))")
	require.NoError(t, err)
	require.Equal(t, "3", v.String())
}

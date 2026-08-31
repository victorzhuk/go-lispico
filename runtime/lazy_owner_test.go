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

	if _, ok := root.GetFunc("->"); !ok {
		t.Fatalf("-> not published in function cell after materialization")
	}
	v, err := eng.Eval(context.Background(), "use", "(-> 1 (+ 2))")
	require.NoError(t, err)
	require.Equal(t, "3", v.String())
}

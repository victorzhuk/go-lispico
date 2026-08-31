package stdlib_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

// bootstrapProbe is trusted host Go code: it wraps a real evaluator and
// forwards everything, counting DefineBootstrap calls so tests can prove
// bootstrap definitions are routed through the env's installed owner.
type bootstrapProbe struct {
	mu     sync.Mutex
	owner  core.BootstrapDefiner
	inner  core.Evaluator
	count  int
	source []string
}

func newBootstrapProbe(inner core.Evaluator, owner core.BootstrapDefiner) *bootstrapProbe {
	return &bootstrapProbe{inner: inner, owner: owner}
}

func (p *bootstrapProbe) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Eval(ctx, form, env)
}

func (p *bootstrapProbe) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	return p.inner.Apply(ctx, fn, args, env)
}

func (p *bootstrapProbe) DefineBootstrap(ctx context.Context, source string, env *core.Env) (core.Value, error) {
	p.mu.Lock()
	p.count++
	p.source = append(p.source, source)
	p.mu.Unlock()
	return p.owner.DefineBootstrap(ctx, source, env)
}

func (p *bootstrapProbe) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

func (p *bootstrapProbe) Sources() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.source...)
}

// incapableEvaluator implements core.Evaluator but not core.BootstrapDefiner.
type incapableEvaluator struct {
	inner core.Evaluator
}

func (e *incapableEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	return e.inner.Eval(ctx, form, env)
}

func (e *incapableEvaluator) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	return e.inner.Apply(ctx, fn, args, env)
}

// eagerBootstrapNames are the bootstrap definitions stdlib publishes eagerly
// when the lazy layer is disabled (bootstrap.stdlibBootstrapEntries).
var eagerBootstrapNames = []string{"->", "->>", "as->", "if-let", "when-let", "get-in"}

func TestStdlibEagerBootstrap_UsesInstalledOwner(t *testing.T) {
	restore := runtime.SetStdlibLazyDisabledForTesting(true)
	defer restore()

	env := core.NewEnv(nil)
	engine := core.NewEvaluator()
	probe := newBootstrapProbe(engine, engine)
	env.SetEvaluator(probe)

	if err := stdlib.New().Init(env); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got := probe.Count(); got != len(eagerBootstrapNames) {
		t.Fatalf("DefineBootstrap called %d times, want %d (one per eager bootstrap entry); the loader must route every bootstrap definition through the installed owner, not a fresh identity Evaluator", got, len(eagerBootstrapNames))
	}

	sources := probe.Sources()
	defined := make(map[string]bool)
	for _, src := range sources {
		for _, name := range eagerBootstrapNames {
			if strings.Contains(src, "(defmacro "+name+" ") || strings.Contains(src, "(defn "+name+" ") {
				defined[name] = true
			}
		}
	}
	for _, name := range eagerBootstrapNames {
		if !defined[name] {
			t.Errorf("bootstrap entry %q was not defined through the installed owner", name)
		}
	}

	// The installed owner must survive; no caller-side substitution.
	if env.Evaluator() != core.Evaluator(probe) {
		t.Fatalf("env evaluator replaced during Init: %T", env.Evaluator())
	}

	// Publication: the default evaluator is Lisp-1, so bootstrap names
	// publish into the value cell only — the function cell stays empty.
	for _, name := range eagerBootstrapNames {
		if _, ok := env.Get(name); !ok {
			t.Errorf("bootstrap name %q not published in the value cell", name)
		}
		if _, ok := env.GetFunc(name); ok {
			t.Errorf("bootstrap name %q mirrored into the function cell under the Lisp-1 default evaluator", name)
		}
	}
}

func TestStandaloneEnv_AdoptsDefaultEvaluator(t *testing.T) {
	restore := runtime.SetStdlibLazyDisabledForTesting(true)
	defer restore()

	env := core.NewEnv(nil)
	if env.Evaluator() != nil {
		t.Fatalf("precondition: fresh env already has an evaluator %T", env.Evaluator())
	}

	if err := stdlib.New().Init(env); err != nil {
		t.Fatalf("Init on env with nil evaluator: %v", err)
	}

	if env.Evaluator() == nil {
		t.Fatalf("Init did not adopt a default evaluator into the environment; env.Evaluator() is nil after Init")
	}

	for _, name := range eagerBootstrapNames {
		if _, ok := env.Get(name); !ok {
			t.Errorf("bootstrap name %q not published in the value cell after standalone Init", name)
		}
		if _, ok := env.GetFunc(name); ok {
			t.Errorf("bootstrap name %q mirrored into the function cell under the Lisp-1 default evaluator", name)
		}
	}
}

func TestStandaloneEnv_NeverReplacesEvaluator(t *testing.T) {
	restore := runtime.SetStdlibLazyDisabledForTesting(true)
	defer restore()

	env := core.NewEnv(nil)
	engine := core.NewEvaluator()
	probe := newBootstrapProbe(engine, engine)
	env.SetEvaluator(probe)

	if err := stdlib.New().Init(env); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if env.Evaluator() != core.Evaluator(probe) {
		t.Fatalf("Init replaced the installed evaluator: got %T, want the pre-installed probe", env.Evaluator())
	}
}

func TestStandaloneEnv_IncapableEvaluatorFailsInit(t *testing.T) {
	restore := runtime.SetStdlibLazyDisabledForTesting(true)
	defer restore()

	env := core.NewEnv(nil)
	bad := &incapableEvaluator{inner: core.NewEvaluator()}
	env.SetEvaluator(bad)

	err := stdlib.New().Init(env)
	if err == nil {
		t.Fatalf("Init with an evaluator lacking core.BootstrapDefiner succeeded; want a typed init error")
	}

	if env.Evaluator() != core.Evaluator(bad) {
		t.Fatalf("failed Init left a different evaluator installed: %T", env.Evaluator())
	}

	for _, name := range eagerBootstrapNames {
		if _, ok := env.Get(name); ok {
			t.Errorf("bootstrap name %q bound despite failed Init", name)
		}
		if _, ok := env.GetFunc(name); ok {
			t.Errorf("bootstrap name %q bound in function cell despite failed Init", name)
		}
	}
}

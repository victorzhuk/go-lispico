package core

import (
	"context"
	"errors"
	"sort"
	"testing"
)

// envForDefiner builds a fresh root environment bound to eng, mirroring how
// the stdlib bootstrap targets a scope.
func envForDefiner(t *testing.T, eng *engine) *Env {
	t.Helper()
	env := NewEnv(nil)
	env.SetEvaluator(eng)
	return env
}

// envNames snapshots every local binding (value and function cells) so tests
// can prove DefineBootstrap wrote nothing.
func envNames(env *Env) []string {
	names := append([]string{}, env.LocalNames()...)
	names = append(names, env.LocalFuncNames()...)
	sort.Strings(names)
	return names
}

func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// evalCountingEvaluator wraps a real engine and counts Eval calls so tests can
// prove the grammar decision happens before any evaluation.
type evalCountingEvaluator struct {
	inner *engine
	count int
}

func (p *evalCountingEvaluator) Eval(ctx context.Context, form Value, env *Env) (Value, error) {
	p.count++
	return p.inner.Eval(ctx, form, env)
}

func (p *evalCountingEvaluator) Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
	return p.inner.Apply(ctx, fn, args, env)
}

// bootstrapCorpus mirrors the shipped stdlib corpus at
// plugins/stdlib/bootstrap.go: exactly 6 entries.
var bootstrapCorpus = []struct {
	name   string
	source string
}{
	{name: "->", source: `(defmacro -> [x & forms]
  (reduce (fn [acc form] (if (list? form) (cons (first form) (cons acc (rest form))) (list form acc))) x forms))`},
	{name: "->>", source: `(defmacro ->> [x & forms]
  (reduce (fn [acc form] (if (list? form) (concat form (list acc)) (list form acc))) x forms))`},
	{name: "as->", source: `(defmacro as-> [expr name & forms]
  (let* [bindings (reduce (fn [acc form] (conj acc name form)) [name expr] forms)]
    (list (quote let*) bindings name)))`},
	{name: "if-let", source: `(defmacro if-let [bindings then else]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))`},
	{name: "when-let", source: `(defmacro when-let [bindings & body]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))`},
	{name: "get-in", source: `(defn get-in [m ks]
  (reduce (fn [acc k] (get acc k)) m ks))`},
}

func TestDefineBootstrap_AcceptsSingleDefnOrDefmacro(t *testing.T) {
	ctx := context.Background()
	eng, err := NewEvaluatorWithDialect(FullDialect())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)

	v, err := eng.DefineBootstrap(ctx, `(defn f [x] x)`, env)
	if err != nil {
		t.Fatalf("defn: %v", err)
	}
	lam, ok := v.(Lambda)
	if !ok || lam.Name != "f" {
		t.Fatalf("defn result = %#v, want Lambda named f", v)
	}
	if got, ok := env.Get("f"); !ok {
		t.Fatalf("f not bound in value cell")
	} else if l, ok := got.(Lambda); !ok || l.Name != "f" {
		t.Fatalf("f = %#v, want Lambda named f", got)
	}

	v, err = eng.DefineBootstrap(ctx, `(defmacro m [x] x)`, env)
	if err != nil {
		t.Fatalf("defmacro: %v", err)
	}
	mac, ok := v.(Macro)
	if !ok || mac.Name != "m" {
		t.Fatalf("defmacro result = %#v, want Macro named m", v)
	}
	if got, ok := env.Get("m"); !ok {
		t.Fatalf("m not bound in value cell")
	} else if mm, ok := got.(Macro); !ok || mm.Name != "m" {
		t.Fatalf("m = %#v, want Macro named m", got)
	}
}

func TestDefineBootstrap_RejectsNonDefinitionForms(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		source string
	}{
		{"zero forms", ""},
		{"two forms", `(defn f [x] x) (defn g [y] y)`},
		{"atom", "42"},
		{"empty list", "()"},
		{"def form", `(def x 1)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := NewEvaluatorWithDialect(FullDialect())
			if err != nil {
				t.Fatalf("engine: %v", err)
			}
			env := envForDefiner(t, eng)
			before := envNames(env)

			_, err = eng.DefineBootstrap(ctx, tc.source, env)
			if err == nil {
				t.Fatalf("DefineBootstrap(%q) = nil error, want typed bootstrap grammar error", tc.source)
			}
			var lerr *LispicoError
			if !errors.As(err, &lerr) {
				t.Fatalf("DefineBootstrap(%q) error = %T (%v), want *LispicoError via errors.As", tc.source, err, err)
			}
			if lerr.Code == "" {
				t.Fatalf("DefineBootstrap(%q) error has empty Code", tc.source)
			}
			if after := envNames(env); !sameNames(before, after) {
				t.Fatalf("env changed on rejection: before %v, after %v", before, after)
			}
		})
	}
}

func TestDefineBootstrap_RejectsBeforeEvaluation(t *testing.T) {
	ctx := context.Background()
	rejected := []string{
		"",
		`(defn f [x] x) (defn g [y] y)`,
		"42",
		"()",
		`(def x 1)`,
	}
	eng, err := NewEvaluatorWithDialect(FullDialect())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)
	probe := &evalCountingEvaluator{inner: eng}
	env.SetEvaluator(probe)

	for _, src := range rejected {
		before := envNames(env)
		countBefore := probe.count
		if _, err := eng.DefineBootstrap(ctx, src, env); err == nil {
			t.Fatalf("DefineBootstrap(%q) = nil error, want rejection", src)
		}
		if probe.count != countBefore {
			t.Fatalf("DefineBootstrap(%q) made %d Eval calls, want 0", src, probe.count-countBefore)
		}
		if after := envNames(env); !sameNames(before, after) {
			t.Fatalf("DefineBootstrap(%q) wrote env: before %v, after %v", src, before, after)
		}
	}
}

func TestDefineBootstrap_FullKernelDispatchUnderEmptyDialect(t *testing.T) {
	ctx := context.Background()
	for _, entry := range bootstrapCorpus {
		t.Run(entry.name, func(t *testing.T) {
			eng, err := NewEvaluatorWithDialect(EmptyDialect())
			if err != nil {
				t.Fatalf("engine: %v", err)
			}
			env := envForDefiner(t, eng)

			// Sanity: the owner dialect really lacks defn/defmacro.
			sanity, err := Read(`(defmacro dialectProbe [] 1)`)
			if err != nil {
				t.Fatalf("read sanity form: %v", err)
			}
			if _, err := eng.Eval(ctx, sanity[0], env); err == nil {
				t.Fatalf("EmptyDialect engine evaluated defmacro; owner limits are not in force")
			}

			if _, err := eng.DefineBootstrap(ctx, entry.source, env); err != nil {
				t.Fatalf("DefineBootstrap(%s): %v", entry.name, err)
			}
			got, ok := env.Get(entry.name)
			if !ok {
				t.Fatalf("%s not bound after DefineBootstrap", entry.name)
			}
			if entry.name == "get-in" {
				lam, ok := got.(Lambda)
				if !ok || lam.Name != "get-in" {
					t.Fatalf("get-in = %#v, want Lambda named get-in", got)
				}
				return
			}
			mac, ok := got.(Macro)
			if !ok || mac.Name != entry.name {
				t.Fatalf("%s = %#v, want Macro named %s", entry.name, got, entry.name)
			}
		})
	}
}

func TestDefineBootstrap_CLBracketSyntaxLoads(t *testing.T) {
	ctx := context.Background()
	d := FullDialect().WithoutBracketLiterals()
	eng, err := NewEvaluatorWithDialect(d)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)
	src := `(defmacro w [x & forms] x)`

	// Sanity: the owner's own reader flags reject bracket syntax.
	if _, err := d.Read(src); err == nil {
		t.Fatalf("owner dialect read bracket source without error; WithoutBracketLiterals is not in force")
	}

	if _, err := eng.DefineBootstrap(ctx, src, env); err != nil {
		t.Fatalf("DefineBootstrap with bracket params under WithoutBracketLiterals owner: %v", err)
	}
	got, ok := env.Get("w")
	if !ok {
		t.Fatalf("w not bound after DefineBootstrap")
	}
	mac, ok := got.(Macro)
	if !ok || mac.Name != "w" {
		t.Fatalf("w = %#v, want Macro named w", got)
	}
}

func TestDefineBootstrap_Lisp2BindsFunctionCellOnly(t *testing.T) {
	ctx := context.Background()
	eng, err := NewEvaluatorWithDialect(FullDialect().Lisp2())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)

	if _, err := eng.DefineBootstrap(ctx, `(defn f [x] x)`, env); err != nil {
		t.Fatalf("defn: %v", err)
	}
	if got, ok := env.GetFunc("f"); !ok {
		t.Fatalf("f not bound in function cell under Lisp-2")
	} else if lam, ok := got.(Lambda); !ok || lam.Name != "f" {
		t.Fatalf("f = %#v, want Lambda named f", got)
	}
	if _, ok := env.Get("f"); ok {
		t.Fatalf("f leaked into value cell under Lisp-2")
	}

	if _, err := eng.DefineBootstrap(ctx, `(defmacro m [x] x)`, env); err != nil {
		t.Fatalf("defmacro: %v", err)
	}
	if got, ok := env.GetFunc("m"); !ok {
		t.Fatalf("m not bound in function cell under Lisp-2")
	} else if mac, ok := got.(Macro); !ok || mac.Name != "m" {
		t.Fatalf("m = %#v, want Macro named m", got)
	}
	if _, ok := env.Get("m"); ok {
		t.Fatalf("m leaked into value cell under Lisp-2")
	}
}

func TestDefineBootstrap_Lisp1BindsValueCellOnly(t *testing.T) {
	ctx := context.Background()
	eng, err := NewEvaluatorWithDialect(FullDialect())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)

	if _, err := eng.DefineBootstrap(ctx, `(defn f [x] x)`, env); err != nil {
		t.Fatalf("defn: %v", err)
	}
	if got, ok := env.Get("f"); !ok {
		t.Fatalf("f not bound in value cell under Lisp-1")
	} else if lam, ok := got.(Lambda); !ok || lam.Name != "f" {
		t.Fatalf("f = %#v, want Lambda named f", got)
	}
	if _, ok := env.GetFunc("f"); ok {
		t.Fatalf("f leaked into function cell under Lisp-1")
	}

	if _, err := eng.DefineBootstrap(ctx, `(defmacro m [x] x)`, env); err != nil {
		t.Fatalf("defmacro: %v", err)
	}
	if got, ok := env.Get("m"); !ok {
		t.Fatalf("m not bound in value cell under Lisp-1")
	} else if mac, ok := got.(Macro); !ok || mac.Name != "m" {
		t.Fatalf("m = %#v, want Macro named m", got)
	}
	if _, ok := env.GetFunc("m"); ok {
		t.Fatalf("m leaked into function cell under Lisp-1")
	}
}

func TestDefineBootstrap_DoesNotWidenDialectForms(t *testing.T) {
	ctx := context.Background()
	eng, err := NewEvaluatorWithDialect(EmptyDialect())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	env := envForDefiner(t, eng)

	for _, entry := range bootstrapCorpus {
		if _, err := eng.DefineBootstrap(ctx, entry.source, env); err != nil {
			t.Fatalf("DefineBootstrap(%s): %v", entry.name, err)
		}
	}

	// The owner's resolved form table must be untouched: user defmacro still
	// fails after the trusted definitions landed.
	user, err := Read(`(defmacro user-m [] 1)`)
	if err != nil {
		t.Fatalf("read user form: %v", err)
	}
	if _, err := eng.Eval(ctx, user[0], env); err == nil {
		t.Fatalf("user defmacro evaluated after DefineBootstrap widened the dialect forms")
	}
}

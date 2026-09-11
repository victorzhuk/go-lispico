package core

import (
	"errors"
	"testing"
	"time"
)

func aliasTry(t *testing.T, what string, write func() error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("%s panicked instead of forwarding the write to the root", what)
		}
	}()
	if err := write(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func aliasNS(fn bool) string {
	if fn {
		return "function"
	}
	return "value"
}

func aliasEntry(t *testing.T, reg *Registration, name string, fn bool, what string) *registrationEntry {
	t.Helper()
	ent, ok := reg.entries[registrationKey{name: name, fn: fn}]
	if !ok || ent == nil {
		t.Fatalf("%s left no journal entry for %q in the %s namespace; want the write attributed to the registration", what, name, aliasNS(fn))
	}
	return ent
}

func aliasNoEntry(t *testing.T, reg *Registration, name, what string) {
	t.Helper()
	for _, fn := range []bool{false, true} {
		if _, ok := reg.entries[registrationKey{name: name, fn: fn}]; ok {
			t.Errorf("%s left a journal entry for %q in the %s namespace; want none", what, name, aliasNS(fn))
		}
	}
}

func aliasBegin(t *testing.T, root *Env) *Registration {
	t.Helper()
	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration on an idle root: %v", err)
	}
	return reg
}

func aliasSeed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func aliasForm(t *testing.T, src string) Value {
	t.Helper()
	form, err := ReadOne(src)
	if err != nil {
		t.Fatalf("ReadOne(%q): %v", src, err)
	}
	return form
}

func aliasEval(t *testing.T, ev Evaluator, env *Env, src string) Value {
	t.Helper()
	var out Value
	aliasTry(t, "eval "+src, func() error {
		v, err := ev.Eval(t.Context(), aliasForm(t, src), env)
		out = v
		return err
	})
	return out
}

func aliasWantValue(t *testing.T, root *Env, name string, want Value, what string) {
	t.Helper()
	if got, ok := root.Get(name); !ok || got != want {
		t.Errorf("%s: root %s = %v (bound %v); want %v", what, name, got, ok, want)
	}
}

func aliasEvalRoot(t *testing.T) *Env {
	t.Helper()
	root := NewEnv(nil)
	root.SetEvaluator(NewEvaluator())
	aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
	return root
}

func TestRegistration_ViewHoldsNoBindingsAfterMerge(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
	src := NewEnv(nil)
	aliasSeed(t, "seed source a", src.Set("a", Int{V: 2}))
	aliasSeed(t, "seed source function fa", src.SetFunc("fa", Int{V: 3}))
	canon := NewEnv(nil)
	aliasSeed(t, "seed canonical source b", canon.Set("b", Int{V: 4}))
	epoch := root.MacroEpoch()

	reg := aliasBegin(t, root)
	view := reg.Env()
	aliasTry(t, "src.MergeInto(view)", func() error { return src.MergeInto(view) })
	aliasTry(t, "src.MergeIntoCanonical(view)", func() error { return canon.MergeIntoCanonical(view) })
	aliasTry(t, "view.Rebuild()", func() error { view.Rebuild(); return nil })
	aliasTry(t, "view.BumpMacroEpoch()", func() error { view.BumpMacroEpoch(); return nil })

	if view.vars != nil || view.funcs != nil {
		t.Errorf("after merges, Rebuild and BumpMacroEpoch through the view it holds %d value and %d function cells; want both maps nil with every binding on the root", len(view.vars), len(view.funcs))
	}
	aliasWantValue(t, root, "a", Int{V: 2}, "a merged into the view")
	if got, ok := root.GetFunc("fa"); !ok || got != (Int{V: 3}) {
		t.Errorf("function fa merged into the view: root fa = %v (bound %v); want 3", got, ok)
	}
	if got, ok, c := root.GetCanonical("b"); !ok || !c || got != (Int{V: 4}) {
		t.Errorf("b canonically merged into the view: root b = %v, bound %v, canonical %v; want canonical 4", got, ok, c)
	}
	if got := root.MacroEpoch(); got != epoch+1 {
		t.Errorf("after view.BumpMacroEpoch() root MacroEpoch = %d; want %d, one bump forwarded to the root", got, epoch+1)
	}
}

func TestRegistration_FindOwnerWriteIsAttributed(t *testing.T) {
	t.Parallel()
	parent := NewEnv(nil)
	aliasSeed(t, "seed g above the root", parent.Set("g", Int{V: 7}))
	root := NewEnv(parent)
	aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
	prior := root.vars["x"]

	reg := aliasBegin(t, root)
	view := reg.Env()
	owner, ok := view.Find("x")
	if !ok {
		t.Fatalf("view.Find(x) found nothing; want the owner of the root binding x")
	}
	if owner == root {
		t.Errorf("view.Find(x) returned the raw root; want the view so writes through the owner stay attributed")
	} else if owner != view {
		t.Errorf("view.Find(x) returned %p; want the view %p", owner, view)
	}
	if up, ok := view.Find("g"); !ok || up != parent {
		t.Errorf("view.Find(g) = %p (found %v); want the raw ancestor %p that owns g above the root", up, ok, parent)
	}

	aliasTry(t, "owner.Set(x)", func() error { return owner.Set("x", Int{V: 2}) })
	aliasWantValue(t, root, "x", Int{V: 2}, "x written through the Find owner")
	ent := aliasEntry(t, reg, "x", false, "owner.Set(x) on the Find result")
	if ent.prior != prior {
		t.Errorf("journal entry for x records prior cell %p; want the pre-op cell %p", ent.prior, prior)
	}

	reg.Complete()
	if after, ok := view.Find("x"); !ok || after != view {
		t.Errorf("after completion view.Find(x) = %p (found %v); want the view %p, never the raw root", after, ok, view)
	}
}

func TestRegistration_ChildScopeWriteIsAttributed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		child func(view *Env) (*Env, error)
	}{
		{
			name: "Child",
			child: func(v *Env) (*Env, error) {
				c := v.Child()
				return c, c.Set("local", Int{V: 9})
			},
		},
		{
			name: "ChildVariadic",
			child: func(v *Env) (*Env, error) {
				return v.ChildVariadic([]Symbol{{V: "local"}}, []Value{Int{V: 9}}, Symbol{})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := aliasEvalRoot(t)
			prior := root.vars["x"]
			reg := aliasBegin(t, root)
			view := reg.Env()

			var child *Env
			aliasTry(t, "view."+tt.name+"()", func() error {
				c, err := tt.child(view)
				child = c
				return err
			})
			if child.parent != view {
				t.Errorf("view.%s() parent is %p; want the view %p so parent traversal keeps attribution", tt.name, child.parent, view)
			}
			aliasEval(t, view.Evaluator(), child, "(set! x 2)")

			aliasNoEntry(t, reg, "local", "a child-local binding")
			aliasWantValue(t, root, "x", Int{V: 2}, "x set! from the child scope")
			ent := aliasEntry(t, reg, "x", false, "(set! x 2) in a "+tt.name+" scope of the view")
			if ent.prior != prior {
				t.Errorf("journal entry for x records prior cell %p; want the pre-op cell %p", ent.prior, prior)
			}
		})
	}
}

func TestRegistration_EvaluatorReentryWriteIsAttributed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		src       string
		bound     string
		want      Value
		seeded    bool
		bumpEpoch bool
	}{
		{name: "def", src: "(def y 5)", bound: "y", want: Int{V: 5}},
		{name: "set!", src: "(set! x 5)", bound: "x", want: Int{V: 5}, seeded: true},
		{name: "defmacro", src: "(defmacro m [a] a)", bound: "m", bumpEpoch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := aliasEvalRoot(t)
			var prior *Cell
			if tt.seeded {
				prior = root.vars[tt.bound]
			}
			epoch := root.MacroEpoch()
			reg := aliasBegin(t, root)
			view := reg.Env()

			aliasEval(t, view.Evaluator(), view, tt.src)

			if tt.want != nil {
				aliasWantValue(t, root, tt.bound, tt.want, tt.src+" through the view")
			} else if !root.HasLive(tt.bound) {
				t.Errorf("%s through the view left %s unbound on the root; want it bound there", tt.src, tt.bound)
			}
			if tt.bumpEpoch {
				if got := root.MacroEpoch(); got != epoch+1 {
					t.Errorf("%s through the view left root MacroEpoch at %d; want %d", tt.src, got, epoch+1)
				}
			}
			ent := aliasEntry(t, reg, tt.bound, false, tt.src+" evaluated with the view")
			if ent.prior != prior {
				t.Errorf("journal entry for %s records prior cell %p; want %p", tt.bound, ent.prior, prior)
			}
			if cur := root.vars[tt.bound]; ent.last != cur {
				t.Errorf("journal entry for %s records last cell %p; want the root map cell %p", tt.bound, ent.last, cur)
			}
		})
	}
}

func TestRegistration_CapturedClosureWriteIsAttributed(t *testing.T) {
	t.Parallel()
	root := aliasEvalRoot(t)
	prior := root.vars["x"]
	reg := aliasBegin(t, root)
	view := reg.Env()
	ev := view.Evaluator()
	lam := aliasEval(t, ev, view, "(fn [v] (set! x v))")

	aliasTry(t, "applying the closure captured under the view", func() error {
		_, err := ev.Apply(t.Context(), lam, []Value{Int{V: 3}}, view)
		return err
	})
	aliasWantValue(t, root, "x", Int{V: 3}, "x set! by the captured closure")
	ent := aliasEntry(t, reg, "x", false, "the closure captured under the view")
	if ent.prior != prior {
		t.Errorf("journal entry for x records prior cell %p; want the pre-op cell %p", ent.prior, prior)
	}
	reg.Complete()

	reg2 := aliasBegin(t, root)
	aliasTry(t, "applying the closure after its registration completed", func() error {
		_, err := ev.Apply(t.Context(), lam, []Value{Int{V: 4}}, view)
		return err
	})
	aliasWantValue(t, root, "x", Int{V: 4}, "x set! by the closure after completion")
	aliasNoEntry(t, reg2, "x", "the closure of a completed registration")
	reg2.Complete()
}

func TestRegistration_MergeIntoViewIsAttributed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		canonical bool
		merge     func(src, view *Env) error
	}{
		{name: "MergeInto", merge: func(s, v *Env) error { return s.MergeInto(v) }},
		{name: "MergeIntoCanonical", canonical: true, merge: func(s, v *Env) error { return s.MergeIntoCanonical(v) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
			prior := root.vars["x"]
			src := NewEnv(nil)
			aliasSeed(t, "seed source x", src.Set("x", Int{V: 2}))
			aliasSeed(t, "seed source n", src.Set("n", Int{V: 3}))
			aliasSeed(t, "seed source function f", src.SetFunc("f", Int{V: 4}))

			reg := aliasBegin(t, root)
			aliasTry(t, "src."+tt.name+"(view)", func() error { return tt.merge(src, reg.Env()) })

			if got, ok, c := root.GetCanonical("x"); !ok || c != tt.canonical || got != (Int{V: 2}) {
				t.Errorf("after %s root x = %v, bound %v, canonical %v; want 2 with canonical %v", tt.name, got, ok, c, tt.canonical)
			}
			aliasWantValue(t, root, "n", Int{V: 3}, "n merged into the view")
			if got, ok := root.GetFunc("f"); !ok || got != (Int{V: 4}) {
				t.Errorf("after %s root function f = %v (bound %v); want 4", tt.name, got, ok)
			}
			for _, w := range []struct {
				name  string
				fn    bool
				prior *Cell
			}{{"x", false, prior}, {"n", false, nil}, {"f", true, nil}} {
				ent := aliasEntry(t, reg, w.name, w.fn, "src."+tt.name+"(view)")
				if ent.prior != w.prior {
					t.Errorf("journal entry for %s (%s) records prior cell %p; want %p", w.name, aliasNS(w.fn), ent.prior, w.prior)
				}
				cur := root.vars[w.name]
				if w.fn {
					cur = root.funcs[w.name]
				}
				if ent.last != cur {
					t.Errorf("journal entry for %s (%s) records last cell %p; want the root map cell %p", w.name, aliasNS(w.fn), ent.last, cur)
				}
			}
		})
	}
}

func TestRegistration_MergeFromViewReadsRoot(t *testing.T) {
	t.Parallel()
	for _, finished := range []bool{false, true} {
		name := "active"
		if finished {
			name = "finished"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
			aliasSeed(t, "seed function f", root.SetFunc("f", Int{V: 2}))
			reg := aliasBegin(t, root)
			view := reg.Env()
			if finished {
				reg.Complete()
			}
			other := NewEnv(nil)
			aliasTry(t, "view.MergeInto(other)", func() error { return view.MergeInto(other) })

			if got, ok := other.Get("x"); !ok || got != (Int{V: 1}) {
				t.Errorf("view.MergeInto(other) left other x = %v (bound %v); want the root's 1 copied", got, ok)
			}
			if got, ok := other.GetFunc("f"); !ok || got != (Int{V: 2}) {
				t.Errorf("view.MergeInto(other) left other function f = %v (bound %v); want the root's 2 copied", got, ok)
			}
			if n := len(reg.entries); n != 0 {
				t.Errorf("view.MergeInto(other) left %d journal entries; want none, the target is not the root", n)
			}
		})
	}
}

type aliasMergeResult struct {
	err      error
	panicked bool
}

func TestRegistration_MergeIntoSelfRefused(t *testing.T) {
	t.Parallel()
	const want = "merge source and target are the same environment"
	pairs := []struct {
		name  string
		merge func(root, view *Env) error
	}{
		{name: "view into root", merge: func(r, v *Env) error { return v.MergeInto(r) }},
		{name: "root into view", merge: func(r, v *Env) error { return r.MergeInto(v) }},
		{name: "view into view", merge: func(_, v *Env) error { return v.MergeInto(v) }},
		{name: "root into root", merge: func(r, _ *Env) error { return r.MergeInto(r) }},
	}
	for _, finished := range []bool{false, true} {
		state := "active"
		if finished {
			state = "finished"
		}
		for _, p := range pairs {
			t.Run(state+"/"+p.name, func(t *testing.T) {
				t.Parallel()
				root := NewEnv(nil)
				aliasSeed(t, "seed x", root.Set("x", Int{V: 1}))
				reg := aliasBegin(t, root)
				view := reg.Env()
				if finished {
					reg.Complete()
				}

				done := make(chan aliasMergeResult, 1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							done <- aliasMergeResult{panicked: true}
						}
					}()
					done <- aliasMergeResult{err: p.merge(root, view)}
				}()
				var res aliasMergeResult
				select {
				case res = <-done:
				case <-time.After(2 * time.Second):
					t.Fatalf("%s merge did not return within 2s; want an immediate refusal taking no lock", p.name)
				}
				if res.panicked {
					t.Fatalf("%s merge panicked; want it refused with %q", p.name, want)
				}
				var lerr *LispicoError
				if !errors.As(res.err, &lerr) || lerr.Code != "EvalError" || lerr.Message != want {
					t.Errorf("%s merge returned %v; want an EvalError %q", p.name, res.err, want)
				}
			})
		}
	}
}

type aliasLazyLayer struct{ id int }

func (*aliasLazyLayer) LookupAndMaterialize(*Env, string, bool) (Value, bool, bool) {
	return nil, false, false
}

func (*aliasLazyLayer) TombstoneForDelete(*Env, string) {}

func (*aliasLazyLayer) RegisterValue(*Env, string, Value, bool) error { return nil }

func (*aliasLazyLayer) RegisterSource(*Env, string, string) bool { return false }

func (*aliasLazyLayer) ForceAll(*Env) {}

type aliasConfigField struct {
	name               string
	set                func(e *Env, v any)
	get                func(e *Env) any
	prior, op, foreign any
}

func aliasConfigFields() []aliasConfigField {
	return []aliasConfigField{
		{
			name:    "evaluator",
			set:     func(e *Env, v any) { e.SetEvaluator(v.(Evaluator)) },
			get:     func(e *Env) any { return e.Evaluator() },
			prior:   depthLimitEvaluator{limit: 1},
			op:      depthLimitEvaluator{limit: 2},
			foreign: depthLimitEvaluator{limit: 3},
		},
		{
			name:    "retained meter",
			set:     func(e *Env, v any) { e.SetRetainedMeter(v) },
			get:     func(e *Env) any { return e.RetainedMeter() },
			prior:   &testEvalMeter{leaseCalls: 1},
			op:      &testEvalMeter{leaseCalls: 2},
			foreign: &testEvalMeter{leaseCalls: 3},
		},
		{
			name:    "lazy layer",
			set:     func(e *Env, v any) { e.SetLazyLayer(v.(LazyLayer)) },
			get:     func(e *Env) any { return e.LazyLayer() },
			prior:   &aliasLazyLayer{id: 1},
			op:      &aliasLazyLayer{id: 2},
			foreign: &aliasLazyLayer{id: 3},
		},
	}
}

func aliasConfigureThroughView(t *testing.T, root *Env, fields []aliasConfigField) *Registration {
	t.Helper()
	for _, f := range fields {
		f.set(root, f.prior)
	}
	reg := aliasBegin(t, root)
	view := reg.Env()
	for _, f := range fields {
		aliasTry(t, "view "+f.name+" setter", func() error { f.set(view, f.op); return nil })
	}
	for _, f := range fields {
		if got := f.get(root); got != f.op {
			t.Errorf("during the op root %s = %v; want %v, the value set through the view", f.name, got, f.op)
		}
	}
	return reg
}

func TestRegistration_AbortRestoresConfiguration(t *testing.T) {
	t.Parallel()
	fields := aliasConfigFields()
	root := NewEnv(nil)
	reg := aliasConfigureThroughView(t, root, fields)
	reg.Abort()

	for _, f := range fields {
		if got := f.get(root); got != f.prior {
			t.Errorf("after abort root %s = %v; want the prior %v", f.name, got, f.prior)
		}
	}
}

func TestRegistration_AbortKeepsHostConfiguration(t *testing.T) {
	t.Parallel()
	for i, host := range aliasConfigFields() {
		t.Run(host.name, func(t *testing.T) {
			t.Parallel()
			fields := aliasConfigFields()
			root := NewEnv(nil)
			reg := aliasConfigureThroughView(t, root, fields)
			fields[i].set(root, fields[i].foreign)
			reg.Abort()

			for j, f := range fields {
				want, why := f.prior, "the prior restored for an op-owned field"
				if j == i {
					want, why = f.foreign, "the raw root write made after the view setter"
				}
				if got := f.get(root); got != want {
					t.Errorf("after abort root %s = %v; want %v, %s", f.name, got, want, why)
				}
			}
		})
	}
}

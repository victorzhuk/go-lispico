package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestBytecodeEvaluator_ImplementsBootstrapDefiner proves the bytecode
// evaluator exposes the trusted bootstrap capability and delegates to its tree
// owner: definitions land through the tree's dialect rules (Lisp-2 function
// cell for the stock CL dialect), bracket syntax loads via the trusted full
// reader flags despite the owner's CL reader, and non-definitions fail typed
// without touching the environment.
func TestBytecodeEvaluator_ImplementsBootstrapDefiner(t *testing.T) {
	ctx := context.Background()
	eng, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	impl, ok := eng.(*engineImpl)
	if !ok {
		t.Fatalf("New returned %T, want *engineImpl", eng)
	}
	be := impl.bytecodeEvaluator
	if be == nil {
		t.Fatalf("engine has no bytecode evaluator")
	}

	env := core.NewEnv(nil)

	// Delegation: the tree owner is the CL dialect (Lisp-2), so a defn binds
	// the function cell — a fresh identity evaluator would bind the value
	// cell instead.
	if _, err := be.DefineBootstrap(ctx, `(defn f [x] x)`, env); err != nil {
		t.Fatalf("DefineBootstrap(defn): %v", err)
	}
	got, ok := env.GetFunc("f")
	if !ok {
		t.Fatalf("f not bound in function cell; DefineBootstrap did not delegate to the tree owner")
	}
	if lam, ok := got.(core.Lambda); !ok || lam.Name != "f" {
		t.Fatalf("f = %#v, want core.Lambda named f", got)
	}
	if _, ok := env.Get("f"); ok {
		t.Fatalf("f leaked into value cell; delegation bypassed the tree owner's Lisp-2 axis")
	}

	// Trusted reader: bracket parameter syntax loads even though the owner
	// dialect (CL) disables bracket literals.
	if _, err := be.DefineBootstrap(ctx, `(defmacro m [x & forms] x)`, env); err != nil {
		t.Fatalf("DefineBootstrap(defmacro with brackets): %v", err)
	}
	got, ok = env.GetFunc("m")
	if !ok {
		t.Fatalf("m not bound in function cell")
	}
	if mac, ok := got.(core.Macro); !ok || mac.Name != "m" {
		t.Fatalf("m = %#v, want core.Macro named m", got)
	}

	// Typed failure, no env writes.
	namesBefore := append(env.LocalNames(), env.LocalFuncNames()...)
	if _, err := be.DefineBootstrap(ctx, `(def x 1)`, env); err == nil {
		t.Fatalf("DefineBootstrap((def x 1)) = nil error, want typed bootstrap grammar error")
	} else {
		var lerr *core.LispicoError
		if !errors.As(err, &lerr) {
			t.Fatalf("DefineBootstrap((def x 1)) error = %T (%v), want *core.LispicoError", err, err)
		}
	}
	namesAfter := append(env.LocalNames(), env.LocalFuncNames()...)
	if len(namesBefore) != len(namesAfter) {
		t.Fatalf("env changed on rejection: before %v, after %v", namesBefore, namesAfter)
	}
}

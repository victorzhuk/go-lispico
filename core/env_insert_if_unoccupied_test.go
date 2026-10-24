package core

import (
	"testing"
)

func TestEnvSetIfUnoccupied(t *testing.T) {
	env := NewEnv(nil)
	wrote, err := env.SetIfUnoccupied("x", Int{V: 1}, false)
	if err != nil || !wrote {
		t.Fatalf("SetIfUnoccupied on empty cell: wrote=%v err=%v, want wrote", wrote, err)
	}
	v, ok := env.Get("x")
	if !ok || !v.Equals(Int{V: 1}) {
		t.Fatalf("Get(x) = %v, %v; want 1", v, ok)
	}

	wrote, err = env.SetIfUnoccupied("x", Int{V: 2}, false)
	if err != nil || wrote {
		t.Fatalf("SetIfUnoccupied on live cell: wrote=%v err=%v, want no write", wrote, err)
	}
	v, _ = env.Get("x")
	if !v.Equals(Int{V: 1}) {
		t.Fatalf("live binding overwritten: Get(x) = %v, want 1", v)
	}

	tomb := env.Child()
	tomb.Set("y", Int{V: 1})
	tomb.Delete("y")
	wrote, err = tomb.SetIfUnoccupied("y", Int{V: 9}, false)
	if err != nil || !wrote {
		t.Fatalf("SetIfUnoccupied after delete: wrote=%v err=%v, want wrote", wrote, err)
	}
	v, _ = tomb.Get("y")
	if !v.Equals(Int{V: 9}) {
		t.Fatalf("Get(y) after revive = %v, want 9", v)
	}
}

func TestEnvSetFuncIfUnoccupied(t *testing.T) {
	env := NewEnv(nil)

	wrote, err := env.SetFuncIfUnoccupied("f", Int{V: 1}, false)
	if err != nil || !wrote {
		t.Fatalf("SetFuncIfUnoccupied on empty cell: wrote=%v err=%v, want wrote", wrote, err)
	}
	v, ok := env.GetFunc("f")
	if !ok || !v.Equals(Int{V: 1}) {
		t.Fatalf("GetFunc(f) = %v, %v; want 1", v, ok)
	}

	wrote, err = env.SetFuncIfUnoccupied("f", Int{V: 2}, false)
	if err != nil || wrote {
		t.Fatalf("SetFuncIfUnoccupied on live cell: wrote=%v err=%v, want no write", wrote, err)
	}
	v, _ = env.GetFunc("f")
	if !v.Equals(Int{V: 1}) {
		t.Fatalf("live func binding overwritten: GetFunc(f) = %v, want 1", v)
	}
}

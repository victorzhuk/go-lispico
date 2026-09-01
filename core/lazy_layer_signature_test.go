package core

import (
	"reflect"
	"testing"
)

// The reuse policy was deleted together with the process-level bootstrap
// artifact cache, so the registration seam must not carry a cache-reuse
// parameter.
func TestLazyLayer_RegisterSourceTakesEnvNameSource(t *testing.T) {
	ifaceMethod, ok := reflect.TypeOf((*LazyLayer)(nil)).Elem().MethodByName("RegisterSource")
	if !ok {
		t.Fatalf("LazyLayer has no RegisterSource method")
	}

	wantIn := []reflect.Type{reflect.TypeOf((*Env)(nil)), reflect.TypeOf(""), reflect.TypeOf("")}

	// The interface method type excludes the receiver; the method expression
	// on the pointer receiver includes it, so both are (*Env, string, string).
	seams := []struct {
		name string
		typ  reflect.Type
	}{
		{"LazyLayer.RegisterSource", ifaceMethod.Type},
		{"(*Env).RegisterSource", reflect.TypeOf((*Env).RegisterSource)},
	}

	for _, seam := range seams {
		t.Run(seam.name, func(t *testing.T) {
			if got := seam.typ.NumIn(); got != len(wantIn) {
				t.Fatalf("%s takes %d inputs, want %d %v", seam.name, got, len(wantIn), wantIn)
			}
			for i, want := range wantIn {
				if got := seam.typ.In(i); got != want {
					t.Fatalf("%s input %d is %s, want %s", seam.name, i, got, want)
				}
			}
			if got := seam.typ.NumOut(); got != 1 {
				t.Fatalf("%s returns %d values, want 1", seam.name, got)
			}
			if got := seam.typ.Out(0); got.Kind() != reflect.Bool {
				t.Fatalf("%s returns %s, want bool", seam.name, got)
			}
		})
	}
}

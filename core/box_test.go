package core

import "testing"

func TestBoxInt_MatchesUnboxedSemantics(t *testing.T) {
	t.Parallel()
	vals := []int64{
		minPreboxedInt - 1, minPreboxedInt, minPreboxedInt + 1,
		-1, 0, 1,
		255, 256,
		maxPreboxedInt - 1, maxPreboxedInt, maxPreboxedInt + 1,
	}
	for _, n := range vals {
		boxed := BoxInt(n)
		plain := Int{V: n}

		if !boxed.Equals(plain) {
			t.Errorf("BoxInt(%d).Equals(Int{%d}) = false, want true", n, n)
		}
		if !plain.Equals(boxed) {
			t.Errorf("Int{%d}.Equals(BoxInt(%d)) = false, want true", n, n)
		}
		if boxed.String() != plain.String() {
			t.Errorf("BoxInt(%d).String() = %q, want %q", n, boxed.String(), plain.String())
		}
		if boxed.Type() != plain.Type() {
			t.Errorf("BoxInt(%d).Type() = %v, want %v", n, boxed.Type(), plain.Type())
		}
		if got, ok := boxed.(Int); !ok || got.V != n {
			t.Errorf("BoxInt(%d) did not unwrap to Int{%d}, got %#v", n, n, boxed)
		}
	}
}

func TestBoxInt_UnequalValuesStayUnequal(t *testing.T) {
	t.Parallel()
	if BoxInt(5).Equals(BoxInt(6)) {
		t.Error("BoxInt(5) should not equal BoxInt(6)")
	}
	if BoxInt(-1).Equals(BoxInt(1)) {
		t.Error("BoxInt(-1) should not equal BoxInt(1)")
	}
}

func TestBoxInt_HashMapKeyInterchangeable(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	if err := m.Set(BoxInt(7), String{V: "seven"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := m.Get(Int{V: 7})
	if !ok || !got.Equals(String{V: "seven"}) {
		t.Errorf("Get(Int{7}) = %v, %v; want %q, true", got, ok, "seven")
	}

	m2 := NewHashMap()
	if err := m2.Set(Int{V: 1200}, String{V: "big"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got2, ok2 := m2.Get(BoxInt(1200))
	if !ok2 || !got2.Equals(String{V: "big"}) {
		t.Errorf("Get(BoxInt(1200)) = %v, %v; want %q, true", got2, ok2, "big")
	}
}

func TestBoxBool_MatchesUnboxedSemantics(t *testing.T) {
	t.Parallel()
	for _, b := range []bool{true, false} {
		boxed := BoxBool(b)
		plain := Bool{V: b}

		if !boxed.Equals(plain) {
			t.Errorf("BoxBool(%v).Equals(Bool{%v}) = false, want true", b, b)
		}
		if boxed.String() != plain.String() {
			t.Errorf("BoxBool(%v).String() = %q, want %q", b, boxed.String(), plain.String())
		}
		if got, ok := boxed.(Bool); !ok || got.V != b {
			t.Errorf("BoxBool(%v) did not unwrap to Bool{%v}, got %#v", b, b, boxed)
		}
	}
	if BoxBool(true).Equals(BoxBool(false)) {
		t.Error("BoxBool(true) should not equal BoxBool(false)")
	}
}

// A counting loop over values within the preboxed range must not allocate —
// this is the whole point of BoxInt on the arithmetic hot path.
func TestBoxInt_AllocsPerRun(t *testing.T) {
	var sink Value
	allocs := testing.AllocsPerRun(1000, func() {
		var acc int64
		for i := range int64(100) {
			acc += i
			sink = BoxInt(acc % (maxPreboxedInt + 1))
		}
	})
	if allocs != 0 {
		t.Errorf("BoxInt in-range loop allocs/op = %v, want 0", allocs)
	}
	_ = sink
}

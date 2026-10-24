package core

import (
	"reflect"
	"strings"
	"testing"
)

// storageID reports the backing-storage address and length of a string
// payload. Equal contents can live in different storage, so identity needs
// both.
func storageID(s string) (uintptr, int) {
	return reflect.ValueOf(s).Pointer(), len(s)
}

func inRange(src string, s string) bool {
	start := reflect.ValueOf(src).Pointer()
	addr, _ := storageID(s)
	return addr >= start && addr < start+uintptr(len(src))
}

// collectPayloads walks every string, symbol, and keyword leaf in the parsed
// form, including inside composites.
func collectPayloads(v Value, out *[]string) {
	switch v := v.(type) {
	case String:
		*out = append(*out, v.V)
	case Symbol:
		*out = append(*out, v.V)
	case Keyword:
		*out = append(*out, v.V)
	case List:
		for i := range v.Len() {
			collectPayloads(v.At(i), out)
		}
	case Vector:
		for i := range v.Len() {
			collectPayloads(v.At(i), out)
		}
	}
}

// TestReaderUnescapedPayloadsAliasSource pins the reader's zero-copy
// contract: an unescaped string, symbol, and keyword payload each point
// inside the source's backing storage. This is the borrowing the runtime
// cache detacher must repair at admission, and a positive control for any
// test that builds fixtures dynamically — a copy in the fixture itself would
// defeat every downstream identity assertion.
func TestReaderUnescapedPayloadsAliasSource(t *testing.T) {
	src := string([]byte(`(def greet "hello") :key sym [inner "nested"] (quote q-sym)`))

	vals, err := Read(src)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var payloads []string
	for _, v := range vals {
		collectPayloads(v, &payloads)
	}
	if len(payloads) == 0 {
		t.Fatal("no string, symbol, or keyword payloads collected")
	}

	aliased := 0
	for _, p := range payloads {
		if inRange(src, p) {
			aliased++
		}
	}
	if aliased == 0 {
		t.Fatal("no unescaped payload aliases the source; the reader copies payloads it must borrow")
	}
}

// TestReaderEscapedStringOwnsStorage pins that an escaped literal's decoded
// payload is a reader-owned allocation, not a slice of the source: the
// runtime is allowed to reuse it without a second copy.
func TestReaderEscapedStringOwnsStorage(t *testing.T) {
	src := string([]byte(`"a\nb" "plain"`))

	vals, err := Read(src)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(vals) != 2 {
		t.Fatalf("Read returned %d forms, want 2", len(vals))
	}
	esc, ok := vals[0].(String)
	if !ok {
		t.Fatalf("form 0 = %T, want String", vals[0])
	}
	if esc.V != "a\nb" {
		t.Fatalf("escaped string = %q, want %q", esc.V, "a\nb")
	}
	if inRange(src, esc.V) {
		t.Fatal("escaped payload aliases the source; it must be reader-owned storage")
	}

	plain, ok := vals[1].(String)
	if !ok {
		t.Fatalf("form 1 = %T, want String", vals[1])
	}
	if !inRange(src, plain.V) {
		t.Fatal("unescaped payload does not alias the source; the zero-copy contract broke")
	}
}

// TestReaderEqualContentDifferentStorage pins that an escaped spelling and
// an unescaped spelling decoding to equal text occupy different storage —
// content equality is not storage identity, the distinction the runtime
// ownership table keys on.
func TestReaderEqualContentDifferentStorage(t *testing.T) {
	src := `"a\nb" "a` + "\n" + `b"`

	vals, err := Read(src)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(vals) != 2 {
		t.Fatalf("Read returned %d forms, want 2", len(vals))
	}
	a, okA := vals[0].(String)
	b, okB := vals[1].(String)
	if !okA || !okB {
		t.Fatalf("forms = %T, %T, want String, String", vals[0], vals[1])
	}
	if a.V != b.V {
		t.Fatalf("equal spellings decoded to %q and %q", a.V, b.V)
	}
	addrA, lenA := storageID(a.V)
	addrB, lenB := storageID(b.V)
	if addrA == addrB && lenA == lenB {
		t.Fatal("escaped and unescaped spellings share storage; identity would merge distinct payloads")
	}
	if !strings.Contains(a.V, "\n") {
		t.Fatalf("escape not decoded: %q", a.V)
	}
	// The escaped spelling is reader-owned storage; the unescaped spelling
	// borrows from inside the source range.
	if inRange(src, a.V) {
		t.Fatal("escaped payload aliases the source; it must be reader-owned storage")
	}
	if !inRange(src, b.V) {
		t.Fatal("unescaped payload does not alias the source; the zero-copy contract broke")
	}
}

func TestReaderPayloadsStillEqual(t *testing.T) {
	vals, err := Read(`(:kw my-sym "str")`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	list, ok := vals[0].(List)
	if !ok {
		t.Fatalf("form = %T, want List", vals[0])
	}
	if list.Len() != 3 {
		t.Fatalf("list len = %d, want 3", list.Len())
	}
	kw := list.At(0)
	sym := list.At(1)
	str := list.At(2)
	if !kw.Equals(Keyword{V: "kw"}) || !sym.Equals(Symbol{V: "my-sym"}) || !str.Equals(String{V: "str"}) {
		t.Fatalf("payloads changed semantics: %v %v %v", kw, sym, str)
	}
	if !strings.Contains(str.String(), "str") {
		t.Fatalf("string rendering broken: %q", str.String())
	}
}

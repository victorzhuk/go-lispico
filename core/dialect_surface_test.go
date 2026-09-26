package core

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestDialect_BuilderSurfaceRemoved(t *testing.T) {
	removedMethods := []string{
		"Memoized", "Add", "Rename", "Remove", "Lisp2", "FlatCond",
		"WithoutBracketLiterals", "WithFunctionRef", "WithReaderVector",
		"Vocabulary", "WithAdapter",
	}
	removedFuncs := map[string]bool{"FullDialect": true, "EmptyDialect": true}

	var offenders []string
	typ := reflect.TypeOf(Dialect{})
	ptr := reflect.PointerTo(typ)
	for _, m := range removedMethods {
		if _, ok := typ.MethodByName(m); ok {
			offenders = append(offenders, "method Dialect."+m)
			continue
		}
		if _, ok := ptr.MethodByName(m); ok {
			offenders = append(offenders, "method (*Dialect)."+m)
		}
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := gotoken.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, fn := range f.Decls {
			d, ok := fn.(*ast.FuncDecl)
			if !ok || d.Recv != nil || !removedFuncs[d.Name.Name] {
				continue
			}
			offenders = append(offenders, "func "+d.Name.Name+" in "+name)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("dialect builder surface still present: %s", strings.Join(offenders, ", "))
	}
}

func TestDialect_ValueIsOneWord(t *testing.T) {
	if got, want := unsafe.Sizeof(Dialect{}), unsafe.Sizeof(uintptr(0)); got != want {
		t.Errorf("unsafe.Sizeof(Dialect{}) = %d, want %d (single pointer word)", got, want)
	}
}

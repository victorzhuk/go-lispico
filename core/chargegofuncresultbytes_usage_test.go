package core

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChargeGoFuncResultBytesCalledOnlyAsReturn statically verifies every
// call to ChargeGoFuncResultBytes across the module is the sole result
// expression of a return statement — "call exactly once, immediately
// before returning the value n describes" per its doc comment. A call
// assigned to a variable and used later would let the calleeCharged
// marker and the value actually returned drift apart under a later
// control-flow change nobody noticed at the call site.
func TestChargeGoFuncResultBytesCalledOnlyAsReturn(t *testing.T) {
	root := moduleRoot(t)
	fset := gotoken.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}

		// First pass: record every ChargeGoFuncResultBytes call that is
		// the sole result of a return statement — the only allowed shape.
		allowed := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			if call, ok := ret.Results[0].(*ast.CallExpr); ok && isChargeGoFuncResultBytesCall(call) {
				allowed[call] = true
			}
			return true
		})

		// Second pass: every ChargeGoFuncResultBytes call must be in the
		// allowed set from the first pass.
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isChargeGoFuncResultBytesCall(call) {
				return true
			}
			if !allowed[call] {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: ChargeGoFuncResultBytes must be called as `return ...ChargeGoFuncResultBytes(...)`, not assigned or discarded", rel, fset.Position(call.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func isChargeGoFuncResultBytesCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "ChargeGoFuncResultBytes"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "ChargeGoFuncResultBytes"
	default:
		return false
	}
}

// moduleRoot returns the repository root by walking up from the working
// directory (the package under test) until go.mod is found.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

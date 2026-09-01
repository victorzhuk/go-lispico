package stdlib

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// plainErrorSite is one fmt.Errorf or errors.New construction found by a scan.
type plainErrorSite struct {
	File string
	Line int
	Call string
}

// key is the allowlist form of a site. Callers build allowlist keys from the
// same dir strings they pass as scan roots, so keys and reported paths always
// share a base and no separate base has to be threaded through.
func (s plainErrorSite) key() string {
	return s.File + ":" + strconv.Itoa(s.Line)
}

// scanPlainErrorSites parses every non-test .go file under dirs and reports each
// fmt.Errorf and errors.New call. Sites whose key is in allow are exempt; allow
// entries that match no site come back as unused, so an exemption cannot outlive
// the code that earned it.
//
// The ban is whole-file rather than reachability-scoped: its only failure mode
// is a false positive, cleared by one reviewed allowlist line, where an
// approximate call graph would fail toward false negatives instead.
func scanPlainErrorSites(dirs []string, allow map[string]bool) (violations []plainErrorSite, unusedAllow []string, err error) {
	fset := gotoken.NewFileSet()
	matched := make(map[string]bool, len(allow))

	for _, dir := range dirs {
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Fixtures under testdata carry deliberate violations and the
				// go tool never builds them, so a production scan must not
				// reach them. A dir passed in explicitly is still scanned.
				if path != dir && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
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
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := plainErrorCallName(call)
				if !ok {
					return true
				}
				site := plainErrorSite{
					File: filepath.ToSlash(path),
					Line: fset.Position(call.Pos()).Line,
					Call: name,
				}
				if allow[site.key()] {
					matched[site.key()] = true
					return true
				}
				violations = append(violations, site)
				return true
			})
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}

	for entry := range allow {
		if !matched[entry] {
			unusedAllow = append(unusedAllow, entry)
		}
	}
	sort.Strings(unusedAllow)
	return violations, unusedAllow, nil
}

func plainErrorCallName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch {
	case pkg.Name == "fmt" && sel.Sel.Name == "Errorf",
		pkg.Name == "errors" && sel.Sel.Name == "New":
		return pkg.Name + "." + sel.Sel.Name, true
	}
	return "", false
}

// migratedScanDirs are the directories whose Builtin failures must reach the
// caller as typed core errors.
var migratedScanDirs = []string{"plugins/stdlib", "internal/collections", "cl"}

// plainErrorAllowlist exempts the stdlib bootstrap wrapping sites. Bootstrap
// source registration and evaluation are not a registered Builtin's failure,
// and the bootstrap ownership contract requires these wraps to carry an
// already-typed cause through unchanged rather than reclassify it.
var plainErrorAllowlist = []struct {
	file string
	line int
}{
	{file: "plugins/stdlib/bootstrap.go", line: 48},
	{file: "plugins/stdlib/bootstrap.go", line: 65},
	{file: "plugins/stdlib/bootstrap.go", line: 75},
}

func TestMigratedPackagesConstructOnlyTypedErrors(t *testing.T) {
	root := moduleRoot(t)

	dirs := make([]string, 0, len(migratedScanDirs))
	for _, dir := range migratedScanDirs {
		dirs = append(dirs, filepath.Join(root, filepath.FromSlash(dir)))
	}
	allow := make(map[string]bool, len(plainErrorAllowlist))
	for _, entry := range plainErrorAllowlist {
		allow[allowKey(root, entry.file, entry.line)] = true
	}

	violations, unusedAllow, err := scanPlainErrorSites(dirs, allow)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, v := range violations {
		rel, relErr := filepath.Rel(root, filepath.FromSlash(v.File))
		if relErr != nil {
			rel = v.File
		}
		t.Errorf("%s:%d: %s constructs a plain error; registered Builtin failures must use a typed core error", filepath.ToSlash(rel), v.Line, v.Call)
	}
	for _, entry := range unusedAllow {
		t.Errorf("allowlist entry %s matches no site; delete it", entry)
	}
}

func TestScanPlainErrorSitesFlagsClosureAndHelperFixtures(t *testing.T) {
	dir := filepath.Join("testdata", "plainerr")

	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, nil)
	if err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if len(unusedAllow) != 0 {
		t.Errorf("empty allowlist reported unused entries: %v", unusedAllow)
	}

	found := map[string]plainErrorSite{}
	for _, v := range violations {
		found[filepath.Base(v.File)] = v
	}
	if len(violations) != 2 {
		t.Fatalf("want 2 fixture violations, got %d: %v", len(violations), violations)
	}
	closure, ok := found["closure_violation.go"]
	if !ok || closure.Call != "fmt.Errorf" {
		t.Errorf("closure fixture not flagged as fmt.Errorf: %+v", closure)
	}
	helper, ok := found["helper_violation.go"]
	if !ok || helper.Call != "errors.New" {
		t.Errorf("helper-only fixture not flagged as errors.New: %+v", helper)
	}
}

func TestScanPlainErrorSitesReportsUnusedAllowlistEntry(t *testing.T) {
	dir := filepath.Join("testdata", "plainerr")

	all, _, err := scanPlainErrorSites([]string{dir}, nil)
	if err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no fixture sites to allowlist")
	}

	stale := filepath.ToSlash(filepath.Join(dir, "retired.go")) + ":1"
	allow := map[string]bool{all[0].key(): true, stale: true}

	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan fixtures with allowlist: %v", err)
	}
	if len(violations) != len(all)-1 {
		t.Errorf("allowlisted site still reported: want %d violations, got %d", len(all)-1, len(violations))
	}
	if len(unusedAllow) != 1 || unusedAllow[0] != stale {
		t.Errorf("want unused allowlist entry %q, got %v", stale, unusedAllow)
	}
}

func allowKey(root, rel string, line int) string {
	return filepath.ToSlash(filepath.Join(root, filepath.FromSlash(rel))) + ":" + strconv.Itoa(line)
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

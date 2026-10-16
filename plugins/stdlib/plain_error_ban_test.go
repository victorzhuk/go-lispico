package stdlib

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// plainErrorSite is one fmt.Errorf or errors.New construction found by a scan.
type plainErrorSite struct {
	File string
	Line int
	Call string
	Func string
	Expr string
}

// plainErrorKey is the structural identity of a set of equivalent calls: the
// file, the enclosing declared function, and the canonically rendered call
// expression. Unlike the old file:line key it survives line edits above the
// call and cannot drift onto an unrelated site that happens to land on the
// recorded line.
type plainErrorKey struct {
	File string
	Func string
	Expr string
}

// key is the allowlist form of a site. Callers build allowlist keys from the
// same dir strings they pass as scan roots, so keys and reported paths always
// share a base and no separate base has to be threaded through.
func (s plainErrorSite) key() plainErrorKey {
	return plainErrorKey{File: s.File, Func: s.Func, Expr: s.Expr}
}

// scanPlainErrorSites parses every non-test .go file under dirs and reports
// each fmt.Errorf and errors.New call. Sites group by structural key; an
// allowlist entry exempts a whole group only when the group's occurrence
// count equals the reviewed expected count. Entries that match no site, or
// fewer sites than expected, come back as unused, so an exemption cannot
// outlive the code that earned it; more sites than expected is an error
// rather than a silent partial exemption.
//
// The ban is whole-file rather than reachability-scoped: its only failure mode
// is a false positive, cleared by one reviewed allowlist entry, where an
// approximate call graph would fail toward false negatives instead.
func scanPlainErrorSites(dirs []string, allow map[plainErrorKey]int) (violations []plainErrorSite, unusedAllow []plainErrorKey, err error) {
	fset := gotoken.NewFileSet()
	var sites []plainErrorSite
	counts := make(map[plainErrorKey]int)

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
			for _, decl := range f.Decls {
				// Calls carry their enclosing declared function's identity —
				// closures included — and anything outside a FuncDecl belongs
				// to the package itself.
				owner := "<package>"
				if fd, ok := decl.(*ast.FuncDecl); ok {
					owner = plainErrorOwnerName(fd)
				}
				ast.Inspect(decl, func(n ast.Node) bool {
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
						Func: owner,
						Expr: renderPlainExpr(call),
					}
					sites = append(sites, site)
					counts[site.key()]++
					return true
				})
			}
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}

	exempt := make(map[plainErrorKey]bool, len(allow))
	for key, want := range allow {
		if want <= 0 {
			return nil, nil, fmt.Errorf("plain error allowlist: nonpositive expected count %d for %s / %s / %s", want, key.File, key.Func, key.Expr)
		}
		got := counts[key]
		if got < want {
			unusedAllow = append(unusedAllow, key)
			continue
		}
		if got > want {
			return nil, nil, fmt.Errorf("plain error allowlist: %s / %s / %s matches %d sites, expected %d", key.File, key.Func, key.Expr, got, want)
		}
		exempt[key] = true
	}

	for _, site := range sites {
		if !exempt[site.key()] {
			violations = append(violations, site)
		}
	}
	sort.Slice(unusedAllow, func(i, j int) bool {
		a, b := unusedAllow[i], unusedAllow[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Func != b.Func {
			return a.Func < b.Func
		}
		return a.Expr < b.Expr
	})
	return violations, unusedAllow, nil
}

// plainErrorOwnerName renders a declaration's owner identity: methods carry
// their canonical receiver type, e.g. (*VM).ResetIncremental; plain functions
// carry only their name.
func plainErrorOwnerName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	pointer := ""
	if star, ok := recv.(*ast.StarExpr); ok {
		pointer, recv = "*", star.X
	}
	id, ok := recv.(*ast.Ident)
	if !ok {
		return fd.Name.Name
	}
	if pointer != "" {
		return "(*" + id.Name + ")." + fd.Name.Name
	}
	return id.Name + "." + fd.Name.Name
}

// renderPlainExpr renders a call expression canonically. The fresh, empty
// FileSet means the node's original offsets resolve to no line information,
// so the printer emits one layout regardless of the source's own line breaks,
// and comments and positions cannot leak into the key.
func renderPlainExpr(e ast.Expr) string {
	var sb strings.Builder
	if err := format.Node(&sb, gotoken.NewFileSet(), e); err != nil {
		panic("render plain error expression: " + err.Error())
	}
	return sb.String()
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

// migratedScanDirs are the directories whose evaluation failures must reach the
// caller as typed core errors. core/vm is in scope because its native operator
// and map-literal opcodes bypass the Builtins and classify those failures
// themselves.
var migratedScanDirs = []string{"plugins/stdlib", "internal/collections", "cl", "core/vm"}

// plainErrorAllowRecord is one reviewed exemption: every occurrence of expr in
// owner within file, as a group of exactly count calls.
type plainErrorAllowRecord struct {
	file  string
	owner string
	expr  string
	count int
}

// plainErrorAllowlist exempts sites that do not originate an evaluation
// failure. A key is file, owner, and rendered expression with an expected
// occurrence count, so an edited call changes its key and shows up as a
// violation plus an unused entry rather than letting the exemption drift onto
// whatever code now sits at an old line number.
var plainErrorAllowlist = []plainErrorAllowRecord{
	// Bootstrap source registration and evaluation are not a registered
	// Builtin's failure, and the bootstrap ownership contract requires these
	// wraps to carry an already-typed cause through unchanged. The two
	// identical "bootstrap eval" wraps share one key with count 2.
	{file: "plugins/stdlib/bootstrap.go", owner: "(*Plugin).loadBootstrap", expr: `fmt.Errorf("stdlib bootstrap: installed evaluator %T does not implement core.BootstrapDefiner", owner)`, count: 1},
	{file: "plugins/stdlib/bootstrap.go", owner: "(*Plugin).loadBootstrap", expr: `fmt.Errorf("bootstrap eval: %w", err)`, count: 2},

	// Internal VM state assertions: a violated ResetIncremental invariant is a
	// kernel defect, not an evaluation failure a host can classify.
	{file: "core/vm/vm.go", owner: "(*VM).ResetIncremental", expr: `fmt.Errorf("vm: ResetIncremental invariant violated (frames=%d handlers=%d freezeStack=%d depth=%d)", len(vm.frames), len(vm.handlers), len(vm.freezeStack), vm.depth)`, count: 1},
	{file: "core/vm/vm.go", owner: "(*VM).ResetIncremental", expr: `fmt.Errorf("vm: ResetIncremental invariant violated (stack len=%d, want 0)", l)`, count: 1},

	// Wrapping, not origination: %w carries an already-typed inner error or a
	// terminal sentinel through, so errors.As and errors.Is still reach it.
	{file: "core/vm/vm.go", owner: "(*VM).pollCancel", expr: `fmt.Errorf("vm: %w", context.DeadlineExceeded)`, count: 1},
	{file: "core/vm/vm.go", owner: "(*VM).pollCancel", expr: `fmt.Errorf("vm: %w", err)`, count: 1},
	{file: "core/vm/vm.go", owner: "(*VM).run", expr: `fmt.Errorf("vm: %w", err)`, count: 1},

	// Malformed-bytecode assertions: a constant or subchunk index out of range
	// means the chunk is corrupt, not that the program failed.
	{file: "core/vm/chunk.go", owner: "(*Chunk).GetConstant", expr: `fmt.Errorf("constant index %d out of range", i)`, count: 1},
	{file: "core/vm/chunk.go", owner: "(*Chunk).GetSymbolConstant", expr: `fmt.Errorf("expected symbol constant, got %T", v)`, count: 1},
	{file: "core/vm/chunk.go", owner: "(*Chunk).GetSubChunk", expr: `fmt.Errorf("subchunk index %d out of range", i)`, count: 1},
}

// plainErrorAllowMap builds the exemption map from reviewed records. It fails
// on a duplicate key because a plain map literal would silently collapse
// duplicates and hide a second review of the same site.
func plainErrorAllowMap(root string, records []plainErrorAllowRecord) map[plainErrorKey]int {
	m := make(map[plainErrorKey]int, len(records))
	for _, r := range records {
		k := allowKey(root, r.file, r.owner, r.expr)
		if _, dup := m[k]; dup {
			panic("plain error allowlist: duplicate key: " + k.File + " / " + k.Func + " / " + k.Expr)
		}
		m[k] = r.count
	}
	return m
}

func TestMigratedPackagesConstructOnlyTypedErrors(t *testing.T) {
	root := moduleRoot(t)

	dirs := make([]string, 0, len(migratedScanDirs))
	for _, dir := range migratedScanDirs {
		dirs = append(dirs, filepath.Join(root, filepath.FromSlash(dir)))
	}
	allow := plainErrorAllowMap(root, plainErrorAllowlist)

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
		t.Errorf("allowlist entry %s / %s / %s matches no site; delete it", entry.File, entry.Func, entry.Expr)
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
	if closure.Func != "registerClosureViolation" {
		t.Errorf("closure fixture not attributed to its enclosing declared function: %+v", closure)
	}
	helper, ok := found["helper_violation.go"]
	if !ok || helper.Call != "errors.New" {
		t.Errorf("helper-only fixture not flagged as errors.New: %+v", helper)
	}
	if helper.Func != "checkHelperArity" {
		t.Errorf("helper fixture not attributed to its helper function: %+v", helper)
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

	stale := plainErrorKey{
		File: filepath.ToSlash(filepath.Join(dir, "retired.go")),
		Func: "retired",
		Expr: renderPlainExpr(mustParseExpr(t, `errors.New("retired")`)),
	}
	allow := map[plainErrorKey]int{all[0].key(): 1, stale: 1}

	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan fixtures with allowlist: %v", err)
	}
	if len(violations) != len(all)-1 {
		t.Errorf("allowlisted site still reported: want %d violations, got %d", len(all)-1, len(violations))
	}
	if len(unusedAllow) != 1 || unusedAllow[0] != stale {
		t.Errorf("want unused allowlist entry %+v, got %v", stale, unusedAllow)
	}
}

func allowKey(root, rel, owner, expr string) plainErrorKey {
	parsed, err := parser.ParseExpr(expr)
	if err != nil {
		panic("plain error allowlist: cannot parse expression `" + expr + "`: " + err.Error())
	}
	return plainErrorKey{
		File: filepath.ToSlash(filepath.Join(root, filepath.FromSlash(rel))),
		Func: owner,
		Expr: renderPlainExpr(parsed),
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

// writeScanFixture writes src under a temporary scan root so structural-key
// behavior can be exercised without touching the reviewed production files.
func writeScanFixture(t *testing.T, dir, name, src string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func mustParseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse expression %q: %v", src, err)
	}
	return e
}

const exactKeyFixtureSrc = `package fixture

import "fmt"

func keep() error {
	return fmt.Errorf("boom %d", 1)
}

func flag() error {
	return fmt.Errorf("boom %d", 2)
}
`

// TestScanPlainErrorSitesExactKeyExemptsOnlyItsOwner asserts that one reviewed
// key exempts exactly its own call and an unlisted key in the same file stays
// a violation.
func TestScanPlainErrorSitesExactKeyExemptsOnlyItsOwner(t *testing.T) {
	dir := t.TempDir()
	writeScanFixture(t, dir, "fixture.go", exactKeyFixtureSrc)

	allow := map[plainErrorKey]int{
		allowKey(dir, "fixture.go", "keep", `fmt.Errorf("boom %d", 1)`): 1,
	}
	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(unusedAllow) != 0 {
		t.Errorf("matched key reported unused: %v", unusedAllow)
	}
	if len(violations) != 1 || violations[0].Func != "flag" {
		t.Errorf("want only the unlisted flag() call as violation, got %v", violations)
	}
}

// TestScanPlainErrorSitesExemptionSurvivesLineInsertion asserts the property
// the old file:line key broke: inserting unrelated lines above an exempted
// call neither invalidates the exemption nor reassigns it.
func TestScanPlainErrorSitesExemptionSurvivesLineInsertion(t *testing.T) {
	dir := t.TempDir()
	writeScanFixture(t, dir, "fixture.go", exactKeyFixtureSrc)

	allow := map[plainErrorKey]int{
		allowKey(dir, "fixture.go", "keep", `fmt.Errorf("boom %d", 1)`): 1,
	}
	before, _, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan before edit: %v", err)
	}

	shifted := strings.Replace(exactKeyFixtureSrc, "func keep() error {",
		"// an unrelated inserted comment shifts every line below it\nfunc keep() error {", 1)
	writeScanFixture(t, dir, "fixture.go", shifted)

	after, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan after edit: %v", err)
	}
	if len(unusedAllow) != 0 {
		t.Errorf("exemption invalidated by line insertion: %v", unusedAllow)
	}
	if len(before) != len(after) || after[0].Func != "flag" {
		t.Errorf("line insertion changed violations: before %v after %v", before, after)
	}
}

// TestScanPlainErrorSitesChangedExpressionIsViolationAndStaleKey asserts that
// editing an exempted call's expression produces a violation for the new call
// and leaves the old key stale, so the edit cannot pass silently.
func TestScanPlainErrorSitesChangedExpressionIsViolationAndStaleKey(t *testing.T) {
	dir := t.TempDir()
	writeScanFixture(t, dir, "fixture.go", exactKeyFixtureSrc)

	allow := map[plainErrorKey]int{
		allowKey(dir, "fixture.go", "keep", `fmt.Errorf("boom %d", 1)`): 1,
	}
	changed := strings.Replace(exactKeyFixtureSrc, `fmt.Errorf("boom %d", 1)`, `fmt.Errorf("boom %d", 7)`, 1)
	writeScanFixture(t, dir, "fixture.go", changed)

	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan after edit: %v", err)
	}
	if len(unusedAllow) != 1 || unusedAllow[0].Func != "keep" {
		t.Errorf("want stale entry for keep(), got %v", unusedAllow)
	}
	if len(violations) != 2 {
		t.Errorf("changed call must be a violation alongside flag(), got %v", violations)
	}
	for _, v := range violations {
		if v.Func == "keep" && v.Expr != renderPlainExpr(mustParseExpr(t, `fmt.Errorf("boom %d", 7)`)) {
			t.Errorf("keep() violation carries the new expression: %+v", v)
		}
	}
}

// TestScanPlainErrorSitesAllowMapFailsOnDuplicateRecord asserts the map builder
// surfaces duplicate records instead of letting map assignment collapse them.
func TestScanPlainErrorSitesAllowMapFailsOnDuplicateRecord(t *testing.T) {
	records := []plainErrorAllowRecord{
		{file: "a.go", owner: "f", expr: `errors.New("x")`, count: 1},
		{file: "a.go", owner: "f", expr: `errors.New("x")`, count: 2},
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate record did not fail allowlist construction")
		}
	}()
	plainErrorAllowMap(t.TempDir(), records)
}

const twinFixtureSrc = `package fixture

import "fmt"

func twin() (error, error, error) {
	return fmt.Errorf("boom %d", 1), fmt.Errorf("boom %d", 1), fmt.Errorf("boom %d", 1)
}
`

// TestScanPlainErrorSitesCountMatchesIdenticalCalls asserts a count of 2
// exempts exactly two identical calls in one owner.
func TestScanPlainErrorSitesCountMatchesIdenticalCalls(t *testing.T) {
	dir := t.TempDir()
	two := strings.Replace(twinFixtureSrc, "fmt.Errorf(\"boom %d\", 1), fmt.Errorf(\"boom %d\", 1), fmt.Errorf(\"boom %d\", 1)",
		"fmt.Errorf(\"boom %d\", 1), fmt.Errorf(\"boom %d\", 1)", 1)
	two = strings.Replace(two, "(error, error, error)", "(error, error)", 1)
	writeScanFixture(t, dir, "fixture.go", two)

	allow := map[plainErrorKey]int{
		allowKey(dir, "fixture.go", "twin", `fmt.Errorf("boom %d", 1)`): 2,
	}
	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(unusedAllow) != 0 {
		t.Errorf("count-2 key reported unused: %v", unusedAllow)
	}
	if len(violations) != 0 {
		t.Errorf("want both identical calls exempt, got %v", violations)
	}
}

// TestScanPlainErrorSitesOverCountIsAnErrorNotAPartialExempt asserts that a
// third identical call with a reviewed count of 2 fails the scan outright
// rather than exempting two and ignoring the third.
func TestScanPlainErrorSitesOverCountIsAnErrorNotAPartialExempt(t *testing.T) {
	dir := t.TempDir()
	writeScanFixture(t, dir, "fixture.go", twinFixtureSrc)

	allow := map[plainErrorKey]int{
		allowKey(dir, "fixture.go", "twin", `fmt.Errorf("boom %d", 1)`): 2,
	}
	_, _, err := scanPlainErrorSites([]string{dir}, allow)
	if err == nil {
		t.Fatal("three identical calls against a count of 2 must be an error")
	}
	if !strings.Contains(err.Error(), "matches 3 sites, expected 2") {
		t.Errorf("error must name the over-count: %v", err)
	}
}

// TestScanPlainErrorSitesSameExprInOtherOwnerOrFileNotExempted asserts an
// exemption never crosses owners or files: the same expression elsewhere
// stays a violation.
func TestScanPlainErrorSitesSameExprInOtherOwnerOrFileNotExempted(t *testing.T) {
	dir := t.TempDir()
	writeScanFixture(t, dir, "a.go", `package fixture

import "fmt"

func same() error {
	return fmt.Errorf("boom %d", 1)
}

func other() error {
	return fmt.Errorf("boom %d", 1)
}
`)
	writeScanFixture(t, dir, "b.go", `package fixture

import "fmt"

func same() error {
	return fmt.Errorf("boom %d", 1)
}
`)

	allow := map[plainErrorKey]int{
		allowKey(dir, "a.go", "same", `fmt.Errorf("boom %d", 1)`): 1,
	}
	violations, unusedAllow, err := scanPlainErrorSites([]string{dir}, allow)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(unusedAllow) != 0 {
		t.Errorf("matched key reported unused: %v", unusedAllow)
	}
	if len(violations) != 2 {
		t.Fatalf("want the other-owner and other-file copies as violations, got %v", violations)
	}
	for _, v := range violations {
		if v.Func == "same" && filepath.Base(v.File) != "b.go" {
			t.Errorf("exemption crossed into another file: %+v", v)
		}
		if v.Func == "same" && filepath.Base(v.File) == "b.go" {
			continue
		}
		if v.Func != "other" {
			t.Errorf("unexpected violation: %+v", v)
		}
	}
}

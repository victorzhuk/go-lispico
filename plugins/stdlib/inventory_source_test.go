package stdlib

import (
	"errors"
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

	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// invScopeFiles are the non-test sources the inventory reconciles against.
// core/ and plugins/json are deliberately outside it: neither registers a
// stdlib builtin, and neither is edited by this migration.
//
// The charges.go entries name files later seams create. A listed file that
// does not exist yet is skipped, so the set is gated on existence rather than
// pinned to a count and a new seam inherits reconciliation without editing
// this file.
var invScopeFiles = []string{
	"cl/charges.go",
	"cl/cl.go",
	"internal/collections/charges.go",
	"internal/collections/errors.go",
	"internal/collections/kernels.go",
	"internal/collections/order.go",
	"plugins/stdlib/arithmetic.go",
	"plugins/stdlib/charges.go",
	"plugins/stdlib/bootstrap.go",
	"plugins/stdlib/collections.go",
	"plugins/stdlib/comparison.go",
	"plugins/stdlib/control.go",
	"plugins/stdlib/errors.go",
	"plugins/stdlib/higher_order.go",
	"plugins/stdlib/plugin.go",
	"plugins/stdlib/strings.go",
	"plugins/stdlib/types.go",
}

// invFileFamilies gives every in-scope file the families whose seams own it.
// A file is reconciled only once every family named here is migrated, so a
// helper shared by two families waits for the later of the two.
var invFileFamilies = map[string][]string{
	"cl/charges.go":                   {"cl-adapter"},
	"cl/cl.go":                        {"cl-adapter"},
	"internal/collections/charges.go": {"collection", "higher-order", "cl-adapter"},
	"internal/collections/errors.go":  {"support"},
	"internal/collections/kernels.go": {"collection", "higher-order", "cl-adapter"},
	"internal/collections/order.go":   {"numeric", "collection"},
	"plugins/stdlib/arithmetic.go":    {"numeric"},
	"plugins/stdlib/bootstrap.go":     {"support"},
	"plugins/stdlib/charges.go":       {"collection", "types", "higher-order", "string"},
	"plugins/stdlib/collections.go":   {"collection"},
	"plugins/stdlib/comparison.go":    {"numeric"},
	"plugins/stdlib/control.go":       {"higher-order"},
	"plugins/stdlib/errors.go":        {"support"},
	"plugins/stdlib/higher_order.go":  {"higher-order"},
	"plugins/stdlib/plugin.go":        {"support"},
	"plugins/stdlib/strings.go":       {"string"},
	"plugins/stdlib/types.go":         {"types"},
}

// invFuncFamilies narrows the file default for the three kernels each claimed
// by exactly two families. Every other function inherits its file's set; no
// family is ever folded in from a caller.
var invFuncFamilies = map[string][]string{
	"internal/collections/kernels.go:IndexedAccess": {"collection", "cl-adapter"},
	"internal/collections/kernels.go:MapSequences":  {"higher-order", "cl-adapter"},
	"internal/collections/kernels.go:StableSort":    {"collection", "cl-adapter"},
}

// invEnvEvaluatorAllow exempts the one site that legitimately installs and
// reads the owning evaluator. Keyed by file and func rather than by line: a
// line-keyed exemption drifts onto its neighbour the moment a line is inserted.
var invEnvEvaluatorAllow = map[string]bool{
	"plugins/stdlib/bootstrap.go:loadBootstrap": true,
}

// invOpaqueQualified are the package-qualified callees whose cost the builtin
// cannot see. Each must be replaced by an interruptible kernel, rejected before
// entry by a deterministic bound, or recorded as a bounded exception with proof
// and a maximum.
var invOpaqueQualified = map[string]bool{
	"strings.Split":                    true,
	"strings.Join":                     true,
	"strings.ReplaceAll":               true,
	"strings.ToUpper":                  true,
	"strings.ToLower":                  true,
	"strings.TrimSpace":                true,
	"strings.Contains":                 true,
	"strings.HasPrefix":                true,
	"strings.HasSuffix":                true,
	"fmt.Sprintf":                      true,
	"strconv.ParseInt":                 true,
	"strconv.ParseFloat":               true,
	"sort.SliceStable":                 true,
	"core.ValueDeepBytes":              true,
	"core.CheckConstructionDepthWith":  true,
	"core.CheckNestedElementDepthWith": true,
}

// invOpaqueMethods are the HashMap methods of the same set. They are matched on
// the method name alone, because a receiver's type needs a type checker and this
// scan deliberately does without one.
//
// Get and Set are deliberately absent: core.Env carries methods of both names,
// so matching them by bare name flags env.Get and env.Set as opaque callees. The
// cost of leaving them out is nil — both are log-time single-node HAMT lookups,
// bounded dispatch rather than scalable work, and wherever they run at scale
// they sit inside a loop the phase detection already reports. Each is the
// opposite and is the one that earns its place: it hides a whole traversal
// inside the callee with no loop visible at the call site. If HashMap.Get ever
// needs covering, resolve receiver types with go/types; do not restore the bare
// name and do not special-case the receiver's identifier.
var invOpaqueMethods = map[string]bool{
	"Assoc":  true,
	"Dissoc": true,
	"Each":   true,
}

// invSharedKernels are the kernels whose result is the calling builtin's own
// result. Each charges for that result once, and the charge stays exact only
// while every caller returns the call directly.
//
// IndexedAccess and StableSort are deliberately absent: neither hands back the
// builtin's return shape — one yields an access outcome the caller must branch
// on, the other a slice the caller must wrap — so capturing them is forced and
// the wrapping allocation is the caller's own result branch to charge.
var invSharedKernels = map[string]bool{
	"collections.MapSequences": true,
}

// invResultClassNeedsCharge marks the classes that hand the caller fresh
// allocation, so the row must say how much.
var invResultClassNeedsCharge = map[string]bool{
	"fresh-container":        true,
	"fresh-deep":             true,
	"incremental-persistent": true,
	"mixed-string":           true,
}

// invSourceFn is what one parsed function contributes to reconciliation: the
// phases and result branches the inventory must account for, plus the shapes
// the codes below are defined against.
type invSourceFn struct {
	file        string
	name        string
	families    []string
	phases      []string
	branches    []string
	evaluators  []string
	libCalls    []string
	opaqueCalls []string
	unflushed   []string
	wrapped     []string
	hasLoop     bool
	hasBudget   bool
	stepInLoop  bool
}

// invFinding formats one reconciliation finding as
// "<CODE> <file>:<func>:<label>: <detail>". The code set is closed:
//
//	MISSING_REGISTRATION          a function carries fewer rows than it has
//	                              detected phases or branches, or a row names a
//	                              function the source no longer has
//	UNSCOPED_FILE                 a production file invScopeFiles never lists, so
//	                              nothing reconciles it
//	DUPLICATE_ROW                 two rows record the same phase or branch
//	HELPER_ONLY_LOOP              a budgeted row whose function holds neither a
//	                              loop nor a budget
//	OPAQUE_CALL                   either an opaque library callee in a function
//	                              with no budgeted, bounded-exception or
//	                              trusted-host row, or an evaluator callback
//	                              inside a loop with no budgeted or
//	                              callback-owned row
//	MISSING_PROOF                 a bounded-exception row without Proof or MaxWork
//	TRUSTED_HOST_NOT_VALUE_METHOD a trusted-host Proof naming no allowed callee
//	UNTRACKED_UNBOUNDED           an unbounded-tracked row without a Proof naming
//	                              its tracking change, or one that states a MaxWork
//	UNFLUSHED_RETURN              a budget holder returning without settling it
//	DUPLICATE_CALLBACK_CHARGE     a second callback-owned row for one callback
//	UNCLASSIFIED_RESULT_BRANCH    a row whose Class is unknown, or which allocates
//	                              without naming a ChargeExpr
//	PREPOST_ONLY_DISPOSITION      a budgeted loop charging no Step inside its body
//	ENV_EVALUATOR_IN_BUILTIN      a builtin reaching the environment's evaluator
//	WRAPPED_KERNEL_RESULT         a caller binding a shared kernel's result and
//	                              returning it later, so the kernel's single
//	                              charge stops covering the caller's own result
func invFinding(code, file, fn, label, detail string) string {
	return code + " " + file + ":" + fn + ":" + label + ": " + detail
}

// invLatch is one binding of an identifier to a call result, together with the
// source range over which that binding is live. Exemption 3 is keyed
// on the binding, not on the identifier's name: these functions rebind err
// repeatedly, and a name-keyed set lets an err produced by a charge helper
// inherit the exemption a budget error earned elsewhere in the same function.
type invLatch struct {
	name  string
	start gotoken.Pos
	end   gotoken.Pos
}

// invBindingScope returns the range over which the assignment's bindings are
// live. An if-statement initialiser is live for the whole statement, which is
// what makes `if err := b.Step(); err != nil { return nil, err }` exempt; any
// other assignment is live from itself to the end of its block.
func invBindingScope(stack []ast.Node, assign *ast.AssignStmt) (gotoken.Pos, gotoken.Pos) {
	if len(stack) >= 2 {
		if ifStmt, ok := stack[len(stack)-2].(*ast.IfStmt); ok && ifStmt.Init == ast.Stmt(assign) {
			return ifStmt.Pos(), ifStmt.End()
		}
	}
	for i := len(stack) - 2; i >= 0; i-- {
		if block, ok := stack[i].(*ast.BlockStmt); ok {
			return assign.End(), block.End()
		}
	}
	return gotoken.NoPos, gotoken.NoPos
}

// invSweepDirs are the package directories the scope sweep walks. core/ and
// plugins/json stay out: both are deliberately outside the migration, so a
// finding naming either would be wrong.
var invSweepDirs = []string{"cl", "internal/collections", "plugins/stdlib"}

// invSweepUnscopedFiles reports production files invScopeFiles never lists.
// The scope list otherwise validates only its own entries, so a seam that adds
// a file would leave its phases and result branches reconciled by nothing —
// work added with no disposition and no failing test.
//
// A sweep directory that does not exist is skipped rather than reported: the
// reconcilers also run against a fixture root, which carries none of the
// package dirs, and a missing dir is not evidence of an unscoped file. Every
// other walk failure still surfaces.
func invSweepUnscopedFiles(root string) []string {
	scoped := make(map[string]bool, len(invScopeFiles))
	for _, rel := range invScopeFiles {
		scoped[rel] = true
	}

	var out []string
	for _, dir := range invSweepDirs {
		base := filepath.Join(root, filepath.FromSlash(dir))
		if _, statErr := os.Stat(base); errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != base && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if !scoped[rel] {
				out = append(out, invFinding("UNSCOPED_FILE", rel, "-", "-",
					"production file is absent from invScopeFiles, so no seam reconciles it"))
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			out = append(out, invFinding("UNSCOPED_FILE", dir, "-", "-", "scope sweep failed: "+err.Error()))
		}
	}
	return out
}

func invFuncKey(file, fn string) string {
	return file + ":" + fn
}

func invFamiliesOf(file, fn string) []string {
	if fams, ok := invFuncFamilies[invFuncKey(file, fn)]; ok {
		return fams
	}
	return invFileFamilies[file]
}

func invGated(families []string, migrated map[string]bool) bool {
	if len(families) == 0 {
		return false
	}
	for _, family := range families {
		if !migrated[family] {
			return false
		}
	}
	return true
}

func invLabel(kind string, fset *gotoken.FileSet, pos gotoken.Pos) string {
	return kind + "@" + strconv.Itoa(fset.Position(pos).Line)
}

func invTypeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return invTypeString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + invTypeString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + invTypeString(t.Elt)
		}
	}
	return ""
}

// invYieldsValue reports whether a function hands the caller something the
// result ledger has to classify: a Lisp value or a typed interpreter error.
// A closure returning bool or int (a sort comparator) yields nothing to
// classify and is left alone.
func invYieldsValue(ft *ast.FuncType) bool {
	if ft == nil || ft.Results == nil {
		return false
	}
	for _, result := range ft.Results.List {
		switch invTypeString(result.Type) {
		case "core.Value", "[]core.Value", "*core.LispicoError":
			return true
		}
	}
	return false
}

func invTakesBudget(ft *ast.FuncType) bool {
	if ft == nil || ft.Params == nil {
		return false
	}
	for _, param := range ft.Params.List {
		if invTypeString(param.Type) == "*core.BuiltinWorkBudget" {
			return true
		}
	}
	return false
}

func invCalleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// invOpaqueCalleeName names the opaque callee a call invokes, or "" for
// anything else. Matching is on the selector shape and the []rune conversion,
// never on a substring, so strings.Contains matches and a lookalike such as
// mystrings.ContainsFold does not.
func invOpaqueCalleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.ArrayType:
		if fn.Len == nil {
			if elt, ok := fn.Elt.(*ast.Ident); ok && elt.Name == "rune" {
				return "[]rune"
			}
		}
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			if qualified := pkg.Name + "." + fn.Sel.Name; invOpaqueQualified[qualified] {
				return qualified
			}
		}
		if invOpaqueMethods[fn.Sel.Name] {
			return "." + fn.Sel.Name
		}
	}
	return ""
}

// invSharedKernelName names the shared kernel a call invokes, or "" for
// anything else. Matching is on the package-qualified selector, so
// collections.MapSequences matches while a same-named method or a helper in
// another package does not.
func invSharedKernelName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if qualified := pkg.Name + "." + sel.Sel.Name; invSharedKernels[qualified] {
		return qualified
	}
	return ""
}

func invSelectorCallName(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

func invCallsSelector(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			found = true
		}
		return true
	})
	return found
}

func invLastResult(ret *ast.ReturnStmt) ast.Expr {
	if len(ret.Results) == 0 {
		return nil
	}
	return ret.Results[len(ret.Results)-1]
}

// invScanSources parses the in-scope files and reduces each named function to
// an invSourceFn. Anything inside a closure is attributed to the enclosing
// named function, matching how the error inventory records shared factories.
func invScanSources(root string) (map[string]*invSourceFn, error) {
	fset := gotoken.NewFileSet()
	parsed := make(map[string]*ast.File, len(invScopeFiles))
	for _, rel := range invScopeFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		parsed[rel] = f
	}

	// A flush helper is any function handed the budget that settles it. Naming
	// the helpers by shape rather than by a hardcoded list keeps a renamed or
	// newly added helper from reading as an unflushed return.
	flushHelpers := make(map[string]bool)
	for _, rel := range invScopeFiles {
		if parsed[rel] == nil {
			continue
		}
		for _, decl := range parsed[rel].Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if invTakesBudget(fn.Type) && invCallsSelector(fn.Body, "Flush") {
				flushHelpers[fn.Name.Name] = true
			}
		}
	}

	out := make(map[string]*invSourceFn)
	for _, rel := range invScopeFiles {
		if parsed[rel] == nil {
			continue
		}
		for _, decl := range parsed[rel].Decls {
			for _, unit := range invDeclUnits(decl) {
				out[invFuncKey(rel, unit.name)] = invAnalyzeFunc(fset, rel, unit, flushHelpers)
			}
		}
	}
	return out, nil
}

// invUnit is one analyzable body under the name reconciliation knows it by. A
// package-level var contributes one too: a builtin bound to a var — every CL
// adapter is — carries no FuncDecl for a row's Func field to name, and a scan
// reading declarations alone reconciles such a file against nothing.
type invUnit struct {
	name  string
	ftype *ast.FuncType
	body  *ast.BlockStmt
}

func invDeclUnits(decl ast.Decl) []invUnit {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Body == nil {
			return nil
		}
		return []invUnit{{name: d.Name.Name, ftype: d.Type, body: d.Body}}
	case *ast.GenDecl:
		if d.Tok != gotoken.VAR {
			return nil
		}
		var out []invUnit
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit := invUnitLit(vs.Values[i]); lit != nil {
					out = append(out, invUnit{name: name.Name, ftype: lit.Type, body: lit.Body})
				}
			}
		}
		return out
	}
	return nil
}

// invUnitLit picks the closure a var initialiser contributes: the literal held
// by a core.GoFunc's Fn field, which is the builtin body, and otherwise the
// outermost literal. Taking an adapter's outer literal instead would attribute
// the return that constructs the GoFunc as a result branch of the builtin.
func invUnitLit(value ast.Expr) *ast.FuncLit {
	var outer, builtin *ast.FuncLit
	ast.Inspect(value, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			if outer == nil {
				outer = x
			}
		case *ast.CompositeLit:
			if builtin != nil || invTypeString(x.Type) != "core.GoFunc" {
				return true
			}
			for _, elt := range x.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, isIdent := kv.Key.(*ast.Ident); !isIdent || key.Name != "Fn" {
					continue
				}
				if lit, isLit := kv.Value.(*ast.FuncLit); isLit {
					builtin = lit
				}
			}
		}
		return true
	})
	if builtin != nil {
		return builtin
	}
	return outer
}

func invAnalyzeFunc(fset *gotoken.FileSet, rel string, unit invUnit, flushHelpers map[string]bool) *invSourceFn {
	sf := &invSourceFn{
		file:     rel,
		name:     unit.name,
		families: invFamiliesOf(rel, unit.name),
	}

	var lits []*ast.FuncLit
	var loops []ast.Node
	var budgets []gotoken.Pos
	var flushes []gotoken.Pos
	var returns []*ast.ReturnStmt
	var latches []invLatch
	var kernelBinds []invLatch

	var stack []ast.Node
	ast.Inspect(unit.body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)

		switch x := n.(type) {
		case *ast.FuncLit:
			lits = append(lits, x)
		case *ast.ForStmt:
			loops = append(loops, x)
		case *ast.RangeStmt:
			loops = append(loops, x)
		case *ast.ReturnStmt:
			returns = append(returns, x)
		case *ast.AssignStmt:
			if len(x.Rhs) != 1 {
				return true
			}
			if call, isCall := x.Rhs[0].(*ast.CallExpr); isCall && invSharedKernelName(call) != "" {
				// Only the first result: the kernel's value is the caller's
				// result, its error is not.
				if start, end := invBindingScope(stack, x); end != gotoken.NoPos {
					if id, isIdent := x.Lhs[0].(*ast.Ident); isIdent && id.Name != "_" {
						kernelBinds = append(kernelBinds, invLatch{name: id.Name, start: start, end: end})
					}
				}
			}
			name, ok := invSelectorCallName(x.Rhs[0])
			if !ok || (name != "Step" && name != "Flush") {
				return true
			}
			start, end := invBindingScope(stack, x)
			if end == gotoken.NoPos {
				return true
			}
			for _, lhs := range x.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					latches = append(latches, invLatch{name: id.Name, start: start, end: end})
				}
			}
		case *ast.CallExpr:
			if invCalleeName(x) == "NewBuiltinWorkBudget" {
				budgets = append(budgets, x.Pos())
			}
			if callee := invOpaqueCalleeName(x); callee != "" {
				sf.libCalls = append(sf.libCalls, invLabel(callee, fset, x.Pos()))
			}
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Flush":
				flushes = append(flushes, x.Pos())
			case "Evaluator":
				sf.evaluators = append(sf.evaluators, invLabel("env-evaluator", fset, x.Pos()))
			}
		}
		return true
	})

	sf.hasLoop = len(loops) > 0
	sf.hasBudget = len(budgets) > 0

	inLoop := func(p gotoken.Pos) bool {
		for _, loop := range loops {
			if loop.Pos() <= p && p <= loop.End() {
				return true
			}
		}
		return false
	}
	// holderOf returns the innermost closure containing p, or nil for the
	// declared body itself. Budget ownership is scoped by it: a builtin body is
	// a closure, so a registrar that merely encloses one owns no budget.
	holderOf := func(p gotoken.Pos) *ast.FuncLit {
		var best *ast.FuncLit
		for _, lit := range lits {
			if lit.Pos() <= p && p <= lit.End() && (best == nil || lit.Pos() > best.Pos()) {
				best = lit
			}
		}
		return best
	}
	enclosing := func(p gotoken.Pos) *ast.FuncType {
		best := unit.ftype
		var bestPos gotoken.Pos
		for _, lit := range lits {
			if lit.Pos() <= p && p <= lit.End() && lit.Pos() > bestPos {
				best, bestPos = lit.Type, lit.Pos()
			}
		}
		return best
	}

	ast.Inspect(unit.body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !inLoop(call.Pos()) {
			return true
		}
		if sel.Sel.Name == "Step" {
			sf.stepInLoop = true
		}
		recv, ok := sel.X.(*ast.Ident)
		if ok && recv.Name == "eval" && (sel.Sel.Name == "Apply" || sel.Sel.Name == "Eval") {
			sf.opaqueCalls = append(sf.opaqueCalls, invLabel("evaluator-callback", fset, call.Pos()))
		}
		return true
	})

	for _, loop := range loops {
		kind := "for-loop"
		if _, ok := loop.(*ast.RangeStmt); ok {
			kind = "range-loop"
		}
		sf.phases = append(sf.phases, invLabel(kind, fset, loop.Pos()))
	}
	for _, pos := range budgets {
		sf.phases = append(sf.phases, invLabel("work-budget", fset, pos))
	}
	sort.Strings(sf.phases)

	for _, ret := range returns {
		if invYieldsValue(enclosing(ret.Pos())) {
			sf.branches = append(sf.branches, invLabel("return", fset, ret.Pos()))
		}
	}
	sort.Strings(sf.branches)

	// A shared kernel charges its result once, and that charge covers the caller
	// only while the caller returns the call. Binding the value and returning it
	// later leaves the caller's own result outside the kernel's charge.
	for _, ret := range returns {
		if len(ret.Results) == 0 {
			continue
		}
		id, ok := ret.Results[0].(*ast.Ident)
		if !ok {
			continue
		}
		for _, bind := range kernelBinds {
			if bind.name == id.Name && bind.start <= ret.Pos() && ret.Pos() <= bind.end {
				sf.wrapped = append(sf.wrapped, invLabel("wrapped-kernel-result", fset, ret.Pos()))
				break
			}
		}
	}
	sort.Strings(sf.wrapped)

	// Each function that CREATES a budget is its own holder scope, checked
	// against its own budget and its own flushes. A closure that creates none
	// is not a holder, so its returns are never checked — that is the narrow
	// shape exemption 2 was written for, the sort comparator that returns bool
	// and never sees the budget.
	// A return propagates a latched budget error only when a live Step/Flush
	// binding of that name covers it, in its own holder scope.
	isLatched := func(name string, at gotoken.Pos) bool {
		holder := holderOf(at)
		for _, l := range latches {
			if l.name != name || at < l.start || at > l.end {
				continue
			}
			if holderOf(l.start) == holder {
				return true
			}
		}
		return false
	}

	budgetsBy := map[*ast.FuncLit][]gotoken.Pos{}
	for _, pos := range budgets {
		holder := holderOf(pos)
		budgetsBy[holder] = append(budgetsBy[holder], pos)
	}
	if len(budgetsBy) > 0 {
		flushesBy := map[*ast.FuncLit][]gotoken.Pos{}
		for _, pos := range flushes {
			holder := holderOf(pos)
			flushesBy[holder] = append(flushesBy[holder], pos)
		}
		for _, ret := range returns {
			holder := holderOf(ret.Pos())
			scoped := budgetsBy[holder]
			if len(scoped) == 0 {
				continue
			}
			first := scoped[0]
			for _, pos := range scoped {
				if pos < first {
					first = pos
				}
			}
			if invReturnSettlesBudget(ret, first, flushesBy[holder], isLatched, flushHelpers) {
				continue
			}
			sf.unflushed = append(sf.unflushed, invLabel("return", fset, ret.Pos()))
		}
		sort.Strings(sf.unflushed)
	}

	return sf
}

// invReturnSettlesBudget reports whether a return inside a budget holder leaves
// the budget settled. Five shapes do, and each guards code that is already
// correct:
//
//  1. the return lexically precedes the budget, so no budget exists yet;
//  2. the return belongs to a closure that creates no budget of its own — the
//     caller scopes those out and they never reach here;
//  3. the return propagates a Step or Flush error whose binding is still live
//     at that return, in the same holder scope — an identifier rebound from a
//     charge helper or a conversion is not latched, however often the same name
//     is bound from the budget elsewhere;
//  4. the error position is a flush helper call, which settles on the way out;
//  5. the return carries no error and an inline Flush already ran on the path,
//     the shape a three-result kernel is forced into because no two-result
//     finish helper can express it.
//
// firstBudget and flushes belong to the holder scope this return sits in, never
// to an enclosing or sibling function.
func invReturnSettlesBudget(
	ret *ast.ReturnStmt,
	firstBudget gotoken.Pos,
	flushes []gotoken.Pos,
	latched func(name string, at gotoken.Pos) bool,
	flushHelpers map[string]bool,
) bool {
	if ret.Pos() < firstBudget {
		return true
	}

	last := invLastResult(ret)
	if last == nil {
		return false
	}
	if call, ok := last.(*ast.CallExpr); ok && flushHelpers[invCalleeName(call)] {
		return true
	}
	if id, ok := last.(*ast.Ident); ok {
		if latched(id.Name, ret.Pos()) {
			return true
		}
		if id.Name == "nil" {
			for _, pos := range flushes {
				if pos < ret.Pos() {
					return true
				}
			}
		}
	}
	return false
}

func invSortedFuncKeys(funcs map[string]*invSourceFn) []string {
	out := make([]string, 0, len(funcs))
	for key := range funcs {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func invHasDisposition(rows []inventory.WorkPhase, dispositions ...string) bool {
	for _, row := range rows {
		for _, want := range dispositions {
			if row.Disposition == want {
				return true
			}
		}
	}
	return false
}

// reconcileWork compares the recorded work phases against the source under
// root. Only functions whose every family is migrated are reconciled; a
// function with any unmigrated family is skipped whole, never folded into a
// caller's family.
func reconcileWork(root string, phases []inventory.WorkPhase, migrated map[string]bool) []string {
	funcs, err := invScanSources(root)
	if err != nil {
		return []string{invFinding("MISSING_REGISTRATION", "-", "-", "-", "source scan failed: "+err.Error())}
	}

	var out []string
	rowsByFunc := make(map[string][]inventory.WorkPhase)
	seen := make(map[string]bool)
	callbackCharges := make(map[string]int)

	for _, row := range phases {
		key := invFuncKey(row.File, row.Func)
		rowsByFunc[key] = append(rowsByFunc[key], row)

		dup := invRowKey(row.Fn, row.File, row.Func, row.PhaseLabel)
		if seen[dup] {
			out = append(out, invFinding("DUPLICATE_ROW", row.File, row.Func, row.PhaseLabel,
				"a second WorkPhases row records the same phase for "+strconv.Quote(row.Fn)))
		}
		seen[dup] = true

		sf, ok := funcs[key]
		if !ok {
			out = append(out, invFinding("MISSING_REGISTRATION", row.File, row.Func, row.PhaseLabel,
				"row names no function in the in-scope source set"))
			continue
		}

		switch row.Disposition {
		case "bounded-exception":
			if row.Proof == "" || row.MaxWork == 0 {
				out = append(out, invFinding("MISSING_PROOF", row.File, row.Func, row.PhaseLabel,
					"a bounded-exception phase needs both Proof and MaxWork"))
			}
		case "unbounded-tracked":
			if row.Proof == "" || !invNamesTrackedChange(row.Proof) || row.MaxWork != 0 {
				out = append(out, invFinding("UNTRACKED_UNBOUNDED", row.File, row.Func, row.PhaseLabel,
					"an unbounded phase needs a Proof naming its tracking change and no MaxWork"))
			}
		case "trusted-host":
			if !invNamesTrustedCallee(row.Proof) {
				out = append(out, invFinding("TRUSTED_HOST_NOT_VALUE_METHOD", row.File, row.Func, row.PhaseLabel,
					"Proof names no callee in "+strings.Join(invTrustedHostCallees, ", ")))
			}
		case "callback-owned":
			charge := invRowKey(row.Fn, key)
			callbackCharges[charge]++
			if callbackCharges[charge] > 1 {
				out = append(out, invFinding("DUPLICATE_CALLBACK_CHARGE", row.File, row.Func, row.PhaseLabel,
					"a second callback-owned row charges the same callback for "+strconv.Quote(row.Fn)))
			}
		case "budgeted":
			switch {
			case !sf.hasLoop && !sf.hasBudget:
				out = append(out, invFinding("HELPER_ONLY_LOOP", row.File, row.Func, row.PhaseLabel,
					"row claims budgeted work but the named function holds neither a loop nor a budget"))
			case sf.hasLoop && !sf.stepInLoop:
				out = append(out, invFinding("PREPOST_ONLY_DISPOSITION", row.File, row.Func, row.PhaseLabel,
					"budgeted loop charges no Step inside the loop body"))
			}
		}
	}

	for _, key := range invSortedFuncKeys(funcs) {
		sf := funcs[key]
		if len(sf.families) == 0 {
			out = append(out, invFinding("MISSING_REGISTRATION", sf.file, sf.name, "families",
				"in-scope function resolves to no family; add its file to invFileFamilies"))
			continue
		}
		if !invGated(sf.families, migrated) {
			continue
		}
		// Counted, not merely present: a function with five phases and one row
		// leaves four stretches of work owned by nobody, which is four missing
		// entries however many rows exist. Labels stay the coder's to choose,
		// so this compares counts and never names.
		if got, want := len(rowsByFunc[key]), len(sf.phases); got < want {
			out = append(out, invFinding("MISSING_REGISTRATION", sf.file, sf.name, sf.phases[0],
				strconv.Itoa(want)+" work phases detected, "+strconv.Itoa(got)+" WorkPhases rows recorded"))
		}
		if !invEnvEvaluatorAllow[key] {
			for _, label := range sf.evaluators {
				out = append(out, invFinding("ENV_EVALUATOR_IN_BUILTIN", sf.file, sf.name, label,
					"a registered builtin must not reach the environment's evaluator"))
			}
		}
		if !invHasDisposition(rowsByFunc[key],
			"budgeted", "bounded-exception", "trusted-host", "unbounded-tracked") {
			for _, label := range sf.libCalls {
				out = append(out, invFinding("OPAQUE_CALL", sf.file, sf.name, label,
					"opaque callee with no budgeted, bounded-exception, trusted-host or unbounded-tracked row"))
			}
		}
		if !invHasDisposition(rowsByFunc[key], "callback-owned", "budgeted") {
			for _, label := range sf.opaqueCalls {
				out = append(out, invFinding("OPAQUE_CALL", sf.file, sf.name, label,
					"evaluator callback inside a loop with no budgeted or callback-owned row"))
			}
		}
		for _, label := range sf.unflushed {
			out = append(out, invFinding("UNFLUSHED_RETURN", sf.file, sf.name, label,
				"a budget holder must leave through a flush helper so the budget is always settled"))
		}
		for _, label := range sf.wrapped {
			out = append(out, invFinding("WRAPPED_KERNEL_RESULT", sf.file, sf.name, label,
				"a shared kernel's result must be returned directly, so its single charge covers the caller's result"))
		}
	}

	out = append(out, invSweepUnscopedFiles(root)...)
	sort.Strings(out)
	return out
}

// reconcileResult compares the recorded result branches against the source
// under root, under the same family gate as reconcileWork.
func reconcileResult(root string, branches []inventory.ResultBranch, migrated map[string]bool) []string {
	funcs, err := invScanSources(root)
	if err != nil {
		return []string{invFinding("MISSING_REGISTRATION", "-", "-", "-", "source scan failed: "+err.Error())}
	}

	classes := invStringSet(inventory.ResultClasses)

	var out []string
	rowsByFunc := make(map[string]int)
	seen := make(map[string]bool)

	for _, row := range branches {
		key := invFuncKey(row.File, row.Func)
		rowsByFunc[key]++

		dup := invRowKey(row.Fn, row.File, row.Func, row.BranchLabel)
		if seen[dup] {
			out = append(out, invFinding("DUPLICATE_ROW", row.File, row.Func, row.BranchLabel,
				"a second ResultBranches row records the same branch for "+strconv.Quote(row.Fn)))
		}
		seen[dup] = true

		if _, ok := funcs[key]; !ok {
			out = append(out, invFinding("MISSING_REGISTRATION", row.File, row.Func, row.BranchLabel,
				"row names no function in the in-scope source set"))
		}

		switch {
		case !classes[row.Class]:
			out = append(out, invFinding("UNCLASSIFIED_RESULT_BRANCH", row.File, row.Func, row.BranchLabel,
				"class "+strconv.Quote(row.Class)+" is not a declared result class"))
		case invResultClassNeedsCharge[row.Class] && row.ChargeExpr == "":
			out = append(out, invFinding("UNCLASSIFIED_RESULT_BRANCH", row.File, row.Func, row.BranchLabel,
				"class "+row.Class+" hands the caller fresh allocation but names no ChargeExpr"))
		}
	}

	for _, key := range invSortedFuncKeys(funcs) {
		sf := funcs[key]
		if len(sf.families) == 0 {
			out = append(out, invFinding("MISSING_REGISTRATION", sf.file, sf.name, "families",
				"in-scope function resolves to no family; add its file to invFileFamilies"))
			continue
		}
		if !invGated(sf.families, migrated) {
			continue
		}
		if got, want := rowsByFunc[key], len(sf.branches); got < want {
			out = append(out, invFinding("MISSING_REGISTRATION", sf.file, sf.name, sf.branches[0],
				strconv.Itoa(want)+" result branches detected, "+strconv.Itoa(got)+" ResultBranches rows recorded"))
		}
	}

	out = append(out, invSweepUnscopedFiles(root)...)
	sort.Strings(out)
	return out
}

func TestWorkInventory_MatchesSource(t *testing.T) {
	for _, finding := range reconcileWork(moduleRoot(t), inventory.WorkPhases, inventory.FamilyMigrated) {
		t.Error(finding)
	}
}

// TestReconcileWork_UnboundedTrackedRowsNameTheirChange drives reconcileWork on
// synthetic rows because the recorded inventory cannot reach UNTRACKED_UNBOUNDED:
// a real row is written unbounded-tracked together with its change id, so the
// finding would sit unexercised and rot into a guard that no longer guards.
// The lookalike case is the one that rots first — it holds the matcher to whole
// tokens, and a substring search would accept it.
func TestReconcileWork_UnboundedTrackedRowsNameTheirChange(t *testing.T) {
	root := moduleRoot(t)

	const (
		file  = "plugins/stdlib/collections.go"
		fn    = "collectionBuiltins"
		label = "synthetic unbounded phase"
	)

	tests := []struct {
		name    string
		proof   string
		maxWork int64
		want    bool
	}{
		{
			name:  "no change named",
			proof: "core.ValueDeepBytes walks the result as a tree, so the ledger does not bound it.",
			want:  true,
		},
		{
			name:  "tracking change named",
			proof: "core.ValueDeepBytes walks the result as a tree. Owned by core-value-walk-sharing-bound.",
			want:  false,
		},
		{
			name:  "lookalike token named",
			proof: "Owned by core-value-walk-sharing-boundary.",
			want:  true,
		},
		{
			name:    "tracking change named but a ceiling stated",
			proof:   "Owned by core-value-walk-sharing-bound.",
			maxWork: 4_194_304,
			want:    true,
		},
	}

	want := invFinding("UNTRACKED_UNBOUNDED", file, fn, label, "")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []inventory.WorkPhase{{
				Families:    []string{"collection"},
				Fn:          "list",
				File:        file,
				Func:        fn,
				PhaseLabel:  label,
				Disposition: "unbounded-tracked",
				Proof:       tt.proof,
				MaxWork:     tt.maxWork,
			}}

			// Nothing is migrated, so the per-function loop is skipped whole and the
			// only findings left for this site are the per-row switch's own.
			got := false
			for _, finding := range reconcileWork(root, rows, map[string]bool{}) {
				if strings.HasPrefix(finding, want) {
					got = true
				}
			}
			if got != tt.want {
				t.Errorf("UNTRACKED_UNBOUNDED = %v, want %v for Proof %q with MaxWork %d",
					got, tt.want, tt.proof, tt.maxWork)
			}
		})
	}
}

func TestResultInventory_MatchesSource(t *testing.T) {
	for _, finding := range reconcileResult(moduleRoot(t), inventory.ResultBranches, inventory.FamilyMigrated) {
		t.Error(finding)
	}
}

func TestWorkInventory_BudgetHoldersReturnThroughFinishHelper(t *testing.T) {
	funcs, err := invScanSources(moduleRoot(t))
	if err != nil {
		t.Fatalf("scan sources: %v", err)
	}
	for _, key := range invSortedFuncKeys(funcs) {
		sf := funcs[key]
		if !invGated(sf.families, inventory.FamilyMigrated) {
			continue
		}
		for _, label := range sf.unflushed {
			t.Error(invFinding("UNFLUSHED_RETURN", sf.file, sf.name, label,
				"a budget holder must leave through a flush helper so the budget is always settled"))
		}
	}
}

// TestStdlibSources_NoEnvEvaluatorInBuiltins is ungated on purpose: reaching
// the environment's evaluator from a builtin escapes the budget entirely, so
// the ban holds over every in-scope file whether or not its family has moved.
func TestStdlibSources_NoEnvEvaluatorInBuiltins(t *testing.T) {
	funcs, err := invScanSources(moduleRoot(t))
	if err != nil {
		t.Fatalf("scan sources: %v", err)
	}

	matched := make(map[string]bool)
	for _, key := range invSortedFuncKeys(funcs) {
		sf := funcs[key]
		for _, label := range sf.evaluators {
			if invEnvEvaluatorAllow[key] {
				matched[key] = true
				continue
			}
			t.Error(invFinding("ENV_EVALUATOR_IN_BUILTIN", sf.file, sf.name, label,
				"builtin registration must not reach the environment's evaluator"))
		}
	}

	for entry := range invEnvEvaluatorAllow {
		if !matched[entry] {
			t.Errorf("env.Evaluator allowlist entry %s matches no site; delete it", entry)
		}
	}
}

// invUnitsByFile counts the analyzable units the scan attributes to each
// in-scope file.
func invUnitsByFile(funcs map[string]*invSourceFn) map[string]int {
	out := make(map[string]int, len(invScopeFiles))
	for _, sf := range funcs {
		out[sf.file]++
	}
	return out
}

// invFilesWithoutUnits reports in-scope files the scan reads nothing out of. A
// file the analyzer cannot see is reconciled by nothing: its phases and result
// branches pass every check because none of them ever runs against it.
//
// A listed file that does not exist yet is skipped, matching invScanSources.
func invFilesWithoutUnits(root string) []string {
	funcs, err := invScanSources(root)
	if err != nil {
		return []string{invFinding("MISSING_REGISTRATION", "-", "-", "-", "source scan failed: "+err.Error())}
	}

	counts := invUnitsByFile(funcs)
	var out []string
	for _, rel := range invScopeFiles {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil {
			continue
		}
		if counts[rel] == 0 {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// invSyntheticRoot writes synthetic sources under a temporary scan root, keyed
// by their in-scope path. The reconcilers take a root, so a check can be driven
// onto a shape the real tree does not carry without editing the real tree.
func invSyntheticRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func invUnitNames(funcs map[string]*invSourceFn, file string) []string {
	var out []string
	for _, sf := range funcs {
		if sf.file == file {
			out = append(out, sf.name)
		}
	}
	sort.Strings(out)
	return out
}

// invGoFuncConstructionLines finds, by text rather than by the analyzer's own
// matcher, the lines on which a var initialiser returns the constructed
// builtin. Those returns belong to the wrapper that builds the GoFunc, never to
// the builtin body, so no result branch may be attributed to one.
func invGoFuncConstructionLines(t *testing.T, path string) []int {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []int
	for i, line := range strings.Split(string(src), "\n") {
		if strings.TrimSpace(line) == "return core.GoFunc{" {
			out = append(out, i+1)
		}
	}
	return out
}

// TestInventorySource_CLAdapterVarsAreAnalyzed pins that the CL adapters reach
// the analyzer at all. All three are bound to package-level vars, so a scan
// that reads only FuncDecls sees an empty cl/cl.go and reconciles the family
// against nothing the moment it is marked migrated.
func TestInventorySource_CLAdapterVarsAreAnalyzed(t *testing.T) {
	root := moduleRoot(t)
	funcs, err := invScanSources(root)
	if err != nil {
		t.Fatalf("scan sources: %v", err)
	}

	names := invUnitNames(funcs, "cl/cl.go")
	if len(names) == 0 {
		t.Fatal("cl/cl.go yields no analyzable units")
	}

	for _, want := range []string{"clNth", "clMapcar", "clSort"} {
		sf, ok := funcs[invFuncKey("cl/cl.go", want)]
		if !ok {
			t.Errorf("adapter %s is invisible to the scan; cl/cl.go units: %v", want, names)
			continue
		}
		if len(sf.branches) == 0 {
			t.Errorf("adapter %s yields no result branches", want)
		}
	}

	// The body attributed to an adapter is the builtin's own, not the wrapper
	// that constructs it: the wrapper's return hands back a GoFunc, which is no
	// result branch of the builtin.
	construction := invGoFuncConstructionLines(t, filepath.Join(root, "cl", "cl.go"))
	if len(construction) == 0 {
		t.Fatal("cl/cl.go carries no GoFunc construction return; the attribution check would be vacuous")
	}
	for _, line := range construction {
		label := "return@" + strconv.Itoa(line)
		for _, name := range names {
			sf := funcs[invFuncKey("cl/cl.go", name)]
			for _, branch := range sf.branches {
				if branch == label {
					t.Errorf("unit %s attributes the GoFunc construction return %s as a result branch", name, label)
				}
			}
		}
	}
}

// TestInventorySource_ScopedFilesYieldUnits guards the defect the CL adapters
// exposed: a file listed as in-scope that the analyzer reads nothing out of is
// reconciled by nothing, silently.
func TestInventorySource_ScopedFilesYieldUnits(t *testing.T) {
	for _, rel := range invFilesWithoutUnits(moduleRoot(t)) {
		t.Errorf("in-scope file %s yields no analyzable unit, so nothing reconciles it", rel)
	}
}

func TestInventorySource_UnreadableScopedFileIsReported(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "file the analyzer reads nothing out of",
			src:  "package stdlib\n\nconst mapArity = 2\n",
			want: true,
		},
		{
			name: "file carrying a function",
			src:  "package stdlib\n\nfunc mapArity() int { return 2 }\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := invSyntheticRoot(t, map[string]string{"plugins/stdlib/higher_order.go": tt.src})
			got := false
			for _, rel := range invFilesWithoutUnits(root) {
				if rel == "plugins/stdlib/higher_order.go" {
					got = true
				}
			}
			if got != tt.want {
				t.Errorf("reported as unit-free = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestInventorySource_VarUnitBodies pins which package-level vars become units
// and which body each contributes. The lookalike case holds the matcher to the
// GoFunc type: a composite literal from another package carrying an Fn field is
// not a builtin, and folding one in would silently drop its wrapper's return.
func TestInventorySource_VarUnitBodies(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		units    []string
		branches map[string][]string
	}{
		{
			name: "adapter var is a unit named after the var",
			src: "package cl\n" +
				"\n" +
				"var clProbe = sync.OnceValue(func() core.Value {\n" +
				"\treturn core.GoFunc{\n" +
				"\t\tName: \"probe\",\n" +
				"\t\tFn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {\n" +
				"\t\t\treturn core.Nil{}, nil\n" +
				"\t\t},\n" +
				"\t}\n" +
				"})\n",
			units:    []string{"clProbe"},
			branches: map[string][]string{"clProbe": {"return@7"}},
		},
		{
			name: "var without a closure is no unit",
			src: "package cl\n" +
				"\n" +
				"var clNames = []string{\"nth\", \"mapcar\"}\n" +
				"\n" +
				"func helper() core.Value {\n" +
				"\treturn core.Nil{}\n" +
				"}\n",
			units:    []string{"helper"},
			branches: map[string][]string{"helper": {"return@6"}},
		},
		{
			name: "closure inside a function adds no unit",
			src: "package cl\n" +
				"\n" +
				"func register() core.Value {\n" +
				"\tfn := func() core.Value { return core.Nil{} }\n" +
				"\treturn fn()\n" +
				"}\n",
			units:    []string{"register"},
			branches: map[string][]string{"register": {"return@4", "return@5"}},
		},
		{
			name: "GoFunc lookalike keeps the outer literal",
			src: "package cl\n" +
				"\n" +
				"var clProbe = sync.OnceValue(func() core.Value {\n" +
				"\treturn other.GoFunc{\n" +
				"\t\tFn: func(ctx context.Context) (core.Value, error) {\n" +
				"\t\t\treturn core.Nil{}, nil\n" +
				"\t\t},\n" +
				"\t}\n" +
				"})\n",
			units:    []string{"clProbe"},
			branches: map[string][]string{"clProbe": {"return@4", "return@6"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := invSyntheticRoot(t, map[string]string{"cl/cl.go": tt.src})
			funcs, err := invScanSources(root)
			if err != nil {
				t.Fatalf("scan sources: %v", err)
			}
			if got := invUnitNames(funcs, "cl/cl.go"); strings.Join(got, ",") != strings.Join(tt.units, ",") {
				t.Fatalf("units = %v, want %v", got, tt.units)
			}
			for name, want := range tt.branches {
				sf := funcs[invFuncKey("cl/cl.go", name)]
				if got := strings.Join(sf.branches, ","); got != strings.Join(want, ",") {
					t.Errorf("unit %s branches = %v, want %v", name, sf.branches, want)
				}
			}
		})
	}
}

// TestReconcileWork_WrappedKernelResult drives reconcileWork on synthetic
// sources because every caller in the tree returns the kernel call directly,
// which is the shape the finding exists to keep. The lookalikes hold the
// matcher to the package-qualified callee: a same-named helper elsewhere shares
// none of the kernel's charging contract.
func TestReconcileWork_WrappedKernelResult(t *testing.T) {
	const (
		head = "package stdlib\n" +
			"\n" +
			"func mapBuiltin(ctx context.Context, eval core.Evaluator, env *core.Env, args []core.Value) (core.Value, error) {\n"
		tail = "}\n"
	)

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "kernel result bound and returned later",
			body: "\tout, err := collections.MapSequences(ctx, eval, env, args[0], args[1:])\n" +
				"\tif err != nil {\n" +
				"\t\treturn nil, err\n" +
				"\t}\n" +
				"\treturn out, nil\n",
			want: true,
		},
		{
			name: "kernel call returned directly",
			body: "\treturn collections.MapSequences(ctx, eval, env, args[0], args[1:])\n",
			want: false,
		},
		{
			name: "lookalike package",
			body: "\tout, err := seqs.MapSequences(ctx, eval, env, args[0], args[1:])\n" +
				"\treturn out, err\n",
			want: false,
		},
		{
			name: "lookalike callee",
			body: "\tout, err := collections.MapSequencesInto(ctx, eval, env, args[0], args[1:])\n" +
				"\treturn out, err\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := invSyntheticRoot(t, map[string]string{"plugins/stdlib/higher_order.go": head + tt.body + tail})
			findings := reconcileWork(root, nil, map[string]bool{"higher-order": true})

			got := false
			for _, finding := range findings {
				if invFindingCode(finding) == "WRAPPED_KERNEL_RESULT" {
					got = true
				}
			}
			if got != tt.want {
				t.Errorf("WRAPPED_KERNEL_RESULT = %v, want %v; findings: %q", got, tt.want, findings)
			}
			if other := len(findings); got && other != 1 {
				t.Errorf("want the wrapped-kernel finding alone, got %d: %q", other, findings)
			}
		})
	}
}

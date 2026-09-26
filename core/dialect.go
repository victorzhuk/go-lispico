package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"slices"
)

// formFn implements one special form. It is the value type of both the kernel
// table and every dialect's resolved dispatch table.
type formFn = func(context.Context, *engine, []Value, *Env) (Value, error)

// kernel is the canonical special-form table under neutral names. It is built
// once and never mutated after init; dialects resolve against a copy of it.
// Keeping the kernel separate from dispatch lets each Engine hold its own
// effective table (see [Dialect]) instead of sharing a package global.
var kernel map[string]formFn

func init() {
	kernel = map[string]formFn{
		"def":        evalDef,
		"defn":       evalDefn,
		"defmacro":   evalDefmacro,
		"fn":         evalFn,
		"if":         evalIf,
		"cond":       evalCond,
		"when":       evalWhen,
		"let":        evalLet,
		"let*":       evalLetStar,
		"do":         evalDo,
		"quote":      evalQuote,
		"quasiquote": evalQuasiquote,
		"set!":       evalSet,
		"loop":       evalLoop,
		"recur":      evalRecur,
		"try":        evalTry,
		"catch":      evalCatch,
		"throw":      evalThrow,
		"and":        evalAnd,
		"or":         evalOr,
		"not":        evalNot,
	}
}

// identityState is the frozen state of the zero Dialect. It is built after
// kernel is populated, so it must stay out of package-level var initializers.
var identityState *dialectState

func init() {
	identityState = freeze(DialectSpec{}, nil)
}

// namespace is the Dialect's symbol-namespace rule. The zero value is Lisp-1: a
// symbol names one binding.
type namespace int

const (
	nsLisp1 namespace = iota // single binding namespace (Clojure-style)
	nsLisp2                  // separate function cell (Common Lisp-style)
)

// bracketSyntax is the Dialect's rule for [..]/{..} literals. The zero value
// keeps them on (Clojure-style).
type bracketSyntax int

const (
	bracketsOn  bracketSyntax = iota // [..]/{..} read as vector/map literals
	bracketsOff                      // brackets are not literal syntax (CL-style)
)

// funcRefSyntax is the Dialect's rule for #'. The zero value is off, so #' is
// not special unless the axis enables it.
type funcRefSyntax int

const (
	funcRefOff funcRefSyntax = iota // # is not the function-reference reader
	funcRefOn                       // #'x reads as (function x)
)

// readerVecSyntax is the Dialect's rule for #(...). The zero value is off, so
// #(...) is not special unless the axis enables it.
type readerVecSyntax int

const (
	readerVecOff readerVecSyntax = iota // #( is not the reader-vector opener
	readerVecOn                         // #(...) reads as a vector
)

// condShape is the Dialect's cond clause-shape rule. The zero value is nested
// clauses (the kernel default).
type condShape int

const (
	condNested condShape = iota // (cond (test body...) ...) — kernel default
	condFlat                    // (cond test body test body ...) — Clojure
)

// VocabEntry is one entry in a Dialect's vocabulary map. A canonical name
// resolves to the GoFunc the engine already has under that name (a rename).
// A non-nil Adapter binds the visible name directly to that Value (an adapter).
type VocabEntry struct {
	Canonical string
	AdapterID string
	Adapter   Value
}

// Dialect is an Engine's language surface: its special-form table, namespace,
// reader and cond axes, and builtin vocabulary. Build one with NewDialect. The
// zero value is the identity dialect — the full kernel table under canonical
// names with default axes and no vocabulary. A Dialect is an immutable value
// that is safe to copy and share across goroutines.
type Dialect struct {
	st *dialectState
}

// dialectState is the resolved, immutable form of a Dialect. It is written
// once by freeze and only read afterwards, so copies of the Dialect share it
// safely.
type dialectState struct {
	base      DialectBase
	ns        namespace
	brackets  bracketSyntax
	funcRef   funcRefSyntax
	readerVec readerVecSyntax
	cond      condShape
	table     map[string]formFn
	// canon maps every name the dialect knows to its canonical kernel form;
	// a hidden name maps to "".
	canon map[string]string
	// vocab maps a visible builtin name to a canonical builtin name or an
	// adapter. nil means no vocabulary: every registered builtin stays
	// callable under its registered name.
	vocab    map[string]VocabEntry
	doName   string
	identity bool
	fp       string
}

func (d Dialect) state() *dialectState {
	if d.st == nil {
		return identityState
	}
	return d.st
}

// DialectBase selects the special-form table a DialectSpec starts from.
type DialectBase int

const (
	// BaseFull starts from the full kernel table.
	BaseFull DialectBase = iota
	// BaseEmpty starts from an empty table: only the forms the spec maps are
	// callable.
	BaseEmpty
)

// Adapter binds a visible builtin name to a Value that wraps a shared
// implementation. ID names the adapter's semantics and takes part in the
// dialect's identity.
type Adapter struct {
	ID    string
	Value Value
}

// DialectSpec declares a Dialect as data. Forms maps a visible name to the
// kernel form it exposes; Hide drops base forms. The boolean fields set the
// namespace, reader and cond axes. Vocab maps a visible builtin name to a
// canonical one and Adapters binds visible names to adapters; the dialect has
// a vocabulary iff either map is non-nil, so an empty map is a vocabulary that
// admits nothing.
type DialectSpec struct {
	Base         DialectBase
	Forms        map[string]string
	Hide         []string
	Lisp2        bool
	NoBrackets   bool
	FunctionRef  bool
	ReaderVector bool
	FlatCond     bool
	Vocab        map[string]string
	Adapters     map[string]Adapter
}

// NewDialect validates spec and returns the Dialect it describes, resolved
// once up front. The spec is copied: later writes to its maps or slices never
// reach the returned Dialect. On an invalid spec it returns the zero Dialect
// and an error naming the offending entry; the same spec always reports the
// same error.
func NewDialect(spec DialectSpec) (Dialect, error) {
	hide := slices.Clone(spec.Hide)
	slices.Sort(hide)
	hide = slices.Compact(hide)
	if err := validateSpec(spec, hide); err != nil {
		return Dialect{}, err
	}
	return Dialect{st: freeze(spec, hide)}, nil
}

// validateSpec checks spec guard by guard, each over sorted names, so a spec
// that breaks several rules always reports the same one. hide is the sorted,
// deduplicated Hide list.
func validateSpec(spec DialectSpec, hide []string) error {
	forms := slices.Sorted(maps.Keys(spec.Forms))
	adapters := slices.Sorted(maps.Keys(spec.Adapters))

	if spec.Lisp2 {
		for _, name := range forms {
			if name == "funcall" || name == "function" {
				return fmt.Errorf("dialect: %q is reserved by the Lisp-2 namespace", name)
			}
		}
	}
	for _, name := range hide {
		if _, ok := spec.Forms[name]; ok {
			return fmt.Errorf("dialect: %q is both hidden and mapped", name)
		}
	}
	for _, name := range forms {
		if _, ok := kernel[spec.Forms[name]]; !ok {
			return fmt.Errorf("dialect: %q maps to unknown kernel form %q", name, spec.Forms[name])
		}
	}
	for _, name := range hide {
		if _, ok := kernel[name]; spec.Base == BaseEmpty || !ok {
			return fmt.Errorf("dialect: hidden name %q is not in the base", name)
		}
	}
	for _, name := range adapters {
		if spec.Adapters[name].ID == "" {
			return fmt.Errorf("dialect: adapter %q has no semantic ID", name)
		}
	}
	for _, name := range adapters {
		if spec.Adapters[name].Value == nil {
			return fmt.Errorf("dialect: adapter %q has no value", name)
		}
	}
	for _, name := range adapters {
		if _, ok := spec.Vocab[name]; ok {
			return fmt.Errorf("dialect: %q is both a vocabulary rename and an adapter", name)
		}
	}
	return nil
}

// freeze resolves a validated spec into its immutable state. hide is the
// sorted, deduplicated Hide list.
func freeze(spec DialectSpec, hide []string) *dialectState {
	st := &dialectState{base: spec.Base}
	if spec.Lisp2 {
		st.ns = nsLisp2
	}
	if spec.NoBrackets {
		st.brackets = bracketsOff
	}
	if spec.FunctionRef {
		st.funcRef = funcRefOn
	}
	if spec.ReaderVector {
		st.readerVec = readerVecOn
	}
	if spec.FlatCond {
		st.cond = condFlat
	}

	st.table = make(map[string]formFn, len(kernel)+len(spec.Forms)+2)
	st.canon = make(map[string]string, len(kernel)+len(spec.Forms)+2)
	if spec.Base == BaseFull {
		for name, fn := range kernel {
			st.table[name] = fn
			st.canon[name] = name
		}
	}
	for _, name := range hide {
		delete(st.table, name)
		st.canon[name] = ""
	}
	for name, canonical := range spec.Forms {
		st.table[name] = kernel[canonical]
		st.canon[name] = canonical
	}
	// funcall and function are intrinsic to the Lisp-2 axis, not kernel forms,
	// so the axis owns these two names.
	if st.ns == nsLisp2 {
		st.table["funcall"] = evalFuncall
		st.table["function"] = evalFunction
		st.canon["funcall"] = "funcall"
		st.canon["function"] = "function"
	}

	if spec.Vocab != nil || spec.Adapters != nil {
		st.vocab = make(map[string]VocabEntry, len(spec.Vocab)+len(spec.Adapters))
		for name, canonical := range spec.Vocab {
			st.vocab[name] = VocabEntry{Canonical: canonical}
		}
		for name, a := range spec.Adapters {
			st.vocab[name] = VocabEntry{AdapterID: a.ID, Adapter: a.Value}
		}
	}

	st.doName = doNameOf(st.canon)
	st.identity = st.isIdentity()
	st.fp = st.fingerprint()
	return st
}

// doNameOf picks the visible name of the do form: do itself when visible,
// else the smallest visible alias, else "do".
func doNameOf(canon map[string]string) string {
	if canon["do"] == "do" {
		return "do"
	}
	name := ""
	for k, c := range canon {
		if c == "do" && (name == "" || k < name) {
			name = k
		}
	}
	if name == "" {
		return "do"
	}
	return name
}

// isIdentity reports whether st is the identity dialect.
func (st *dialectState) isIdentity() bool {
	defaultAxes := st.base == BaseFull && st.ns == nsLisp1 &&
		st.brackets == bracketsOn && st.funcRef == funcRefOff &&
		st.readerVec == readerVecOff
	if !defaultAxes || st.vocab != nil {
		return false
	}
	callable := 0
	for name, c := range st.canon {
		if c == "" {
			continue
		}
		if c != name {
			return false
		}
		callable++
	}
	return callable == len(kernel)
}

// fingerprint hashes st's resolved configuration: the axes, the callable
// visible→canonical table and the vocabulary, each in sorted order, so two
// dialects that resolve alike fingerprint alike however they were declared.
func (st *dialectState) fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "dialect/2|base=%d|ns=%d|brackets=%d|funcRef=%d|readerVec=%d|cond=%d",
		st.base, st.ns, st.brackets, st.funcRef, st.readerVec, st.cond)
	for _, name := range slices.Sorted(maps.Keys(st.canon)) {
		if st.canon[name] == "" {
			continue
		}
		fmt.Fprint(h, "|f")
		writeField(h, name)
		writeField(h, st.canon[name])
	}
	fmt.Fprintf(h, "|vocab=%t", st.vocab != nil)
	for _, name := range slices.Sorted(maps.Keys(st.vocab)) {
		entry := st.vocab[name]
		fmt.Fprint(h, "|v")
		writeField(h, name)
		writeField(h, entry.Canonical)
		writeField(h, entry.AdapterID)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// writeField writes s as ":<byte length>:<bytes>" so no string content can
// shift a field boundary and collide with a different field split.
func writeField(w io.Writer, s string) {
	fmt.Fprintf(w, ":%d:%s", len(s), s)
}

// Vocab returns a caller-owned copy of the vocabulary map the Dialect was
// configured with. It is nil for a dialect without a vocabulary and a non-nil
// empty map for an empty vocabulary. Each visible name maps to either a
// canonical shared builtin name (Canonical) or an adapter (Adapter non-nil).
// Writes to the returned map never reach the Dialect; adapter Values are
// shared immutable values, not copies.
func (d Dialect) Vocab() map[string]VocabEntry {
	return maps.Clone(d.state().vocab)
}

// VocabEntry returns the vocabulary entry bound to name, and whether one
// exists. It does not copy the vocabulary.
func (d Dialect) VocabEntry(name string) (VocabEntry, bool) {
	entry, ok := d.state().vocab[name]
	return entry, ok
}

// CanonicalName maps a visible special-form name to its canonical kernel name
// under this Dialect if the name is a known special form (possibly renamed).
// It returns:
//   - canonical, false, true if the name is a known special form (possibly renamed)
//   - "", true, true if the name was removed from this dialect's dispatch table
//   - "", false, false if the name is not a special form at all in this dialect
func (d Dialect) CanonicalName(name string) (canonical string, removed bool, ok bool) {
	c, ok := d.state().canon[name]
	if !ok {
		return "", false, false
	}
	return c, c == "", true
}

// TruthyFunc returns the predicate used by dialect-specific conditional evaluation.
// Default behavior uses core.IsTruthy (nil and false are falsy).
func (d Dialect) TruthyFunc() func(Value) bool {
	return IsTruthy
}

// IsBaseEmpty reports whether the Dialect starts from an empty base.
func (d Dialect) IsBaseEmpty() bool {
	return d.state().base == BaseEmpty
}

// isTruthy reports whether v is a true value. All dialects treat nil and false
// as falsy, and every other value as truthy.
func (d Dialect) isTruthy(v Value) bool {
	return IsTruthy(v)
}

// isLisp2 reports whether the Dialect uses a separate function cell. It is the
// single hook eval consults to split head from argument resolution.
func (d Dialect) isLisp2() bool {
	return d.state().ns == nsLisp2
}

// IsLisp2 reports whether d uses a separate function cell (Lisp-2).
func (d Dialect) IsLisp2() bool { return d.isLisp2() }

// readerFlags projects the reader axes onto the flag set the tokenizer consults.
func (d Dialect) readerFlags() readerFlags {
	st := d.state()
	return readerFlags{
		bracketLiterals: st.brackets == bracketsOn,
		functionRef:     st.funcRef == funcRefOn,
		readerVector:    st.readerVec == readerVecOn,
	}
}

// Read tokenizes and parses src under the Dialect's reader flags, returning all
// top-level forms. Engines read source through this so parsing honors the
// running Dialect.
func (d Dialect) Read(src string) ([]Value, error) {
	return d.ReadWithMaxDepth(src, defaultReaderDepth)
}

// ReadWithMaxDepth tokenizes and parses src under the Dialect's reader flags,
// limiting the parser's nesting depth to maxDepth. maxDepth ≤ 0 selects the
// default (1024).
func (d Dialect) ReadWithMaxDepth(src string, maxDepth int) ([]Value, error) {
	forms, _, err := d.ReadWithMaxDepthStats(src, maxDepth)
	return forms, err
}

// ReadWithMaxDepthStats tokenizes and parses src under the Dialect's reader
// flags, returning the parsed forms and deterministic allocation-metering stats.
func (d Dialect) ReadWithMaxDepthStats(src string, maxDepth int) ([]Value, ReaderStats, error) {
	s := readerScratchPool.Get().(*readerScratch)
	s.Reset()
	defer func() {
		if s.release(readerScratchNoCeiling) {
			readerScratchPool.Put(s)
		}
	}()
	return s.read(src, d.readerFlags(), maxDepth)
}

// ReadWithContextStats tokenizes and parses src under the Dialect's reader
// flags, honoring the cancellation state and allocation budget carried by ctx
// while it reads, and returns the parsed forms with the same deterministic
// allocation-metering stats ReadWithMaxDepthStats reports. maxDepth ≤ 0
// selects the default (1024).
func (d Dialect) ReadWithContextStats(ctx context.Context, src string, maxDepth int) ([]Value, ReaderStats, error) {
	s := readerScratchPool.Get().(*readerScratch)
	s.Reset()
	s.budget = s.budgetStore.init(ctx)
	ceiling := s.budget.allocCeiling()
	defer func() {
		if s.release(ceiling) {
			readerScratchPool.Put(s)
		}
	}()

	if err := s.budget.checkpoint(); err != nil {
		return nil, ReaderStats{}, err
	}
	return s.read(src, d.readerFlags(), maxDepth)
}

// IsIdentity reports whether d is the identity dialect: the full kernel base
// where every kernel form is callable under its own name and nothing else is,
// with default namespace and reader axes and no vocabulary. The cond axis does
// not take part. The bytecode VM dispatches canonical form names directly, so
// only the identity dialect is safe to run under it.
func (d Dialect) IsIdentity() bool {
	return d.state().identity
}

// resolve returns the Dialect's effective dispatch table. The table is shared
// by every engine built from d and must not be written.
func (d Dialect) resolve() map[string]formFn {
	return d.state().table
}

// Fingerprint returns a hash string that changes when the Dialect's semantic
// configuration changes. Used as part of the bytecode chunk cache key.
//
// The fingerprint is a process-local identity for one go-lispico version: it
// may change between releases and is not a persistence format.
func (d Dialect) Fingerprint() string {
	return d.state().fp
}

// NormalizeCond parses raw cond operands into canonical (test body) clauses
// under the Dialect's cond clause-shape axis. One canonical clause is one test
// plus one body expression; a Common Lisp nested clause with a multi-expression
// implicit-progn body normalizes by wrapping the body in the dialect-visible
// `do` form. Both the Evaluator and the Compiler call this, so the two paths
// cannot parse cond differently.
func (d Dialect) NormalizeCond(args []Value) ([]Value, error) {
	switch d.state().cond {
	case condFlat:
		return d.normalizeCondFlat(args)
	default:
		return d.normalizeCondNested(args)
	}
}

func (d Dialect) normalizeCondNested(args []Value) ([]Value, error) {
	var clauses []Value
	for _, arg := range args {
		list, ok := arg.(List)
		if !ok {
			return nil, evalErrorf("cond: clauses must be (test body...) lists")
		}
		n := list.Len()
		if n < 2 {
			return nil, evalErrorf("cond: clauses must be (test body...) lists")
		}
		if n == 2 {
			clauses = append(clauses, list)
		} else {
			// Multi-expression body: wrap in (doVisible body...).
			// Cursor rather than At(i): a clause body past the flat
			// threshold is a shared chain, where positional indexing
			// restarts the walk per element.
			wrapped := make([]Value, 0, n)
			wrapped = append(wrapped, Symbol{V: d.state().doName})
			cur := list.cursor()
			test, _ := cur.next()
			for i := 1; i < n; i++ {
				v, _ := cur.next()
				wrapped = append(wrapped, v)
			}
			clause := NewList([]Value{test, NewList(wrapped)})
			clauses = append(clauses, clause)
		}
	}
	return clauses, nil
}

func (d Dialect) normalizeCondFlat(args []Value) ([]Value, error) {
	if len(args)%2 != 0 {
		return nil, evalErrorf("cond: flat pairs require an even number of forms")
	}
	clauses := make([]Value, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		clauses = append(clauses, NewList([]Value{args[i], args[i+1]}))
	}
	return clauses, nil
}

package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
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

type dialectBase int

const (
	baseFull dialectBase = iota
	baseEmpty
)

// namespace is the Dialect's symbol-namespace rule. The zero value is Lisp-1: a
// symbol names one binding, and a Dialect built without touching the axis
// behaves as before.
type namespace int

const (
	nsLisp1 namespace = iota // single binding namespace (Clojure-style)
	nsLisp2                  // separate function cell (Common Lisp-style)
)

// bracketSyntax is the Dialect's rule for [..]/{..} literals. The zero value
// keeps them on (Clojure-style), so a Dialect built without touching the axis
// parses brackets as before.
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
// clauses (the kernel default), so a Dialect built without touching the axis
// parses cond as before.
type condShape int

const (
	condNested condShape = iota // (cond (test body...) ...) — kernel default
	condFlat                    // (cond test body test body ...) — Clojure
)

type deltaKind int

const (
	opRename deltaKind = iota
	opAdd
	opRemove
)

type deltaOp struct {
	kind      deltaKind
	name      string
	canonical string
}

// VocabEntry is one entry in a Dialect's vocabulary map. A canonical name
// resolves to the GoFunc the engine already has under that name (a rename).
// A non-nil Adapter binds the visible name directly to that Value (an adapter).
type VocabEntry struct {
	Canonical string
	AdapterID string
	Adapter   Value
}

// Dialect describes an Engine's special-form table as a delta over a base. The
// base is either the full kernel table or empty; the delta renames, adds, or
// removes forms. Resolving a Dialect yields the effective name→form table an
// Engine dispatches through. A Dialect is an immutable value: the builder
// methods return a new Dialect and never mutate the receiver.
type Dialect struct {
	base      dialectBase
	ops       []deltaOp
	ns        namespace
	brackets  bracketSyntax
	funcRef   funcRefSyntax
	readerVec readerVecSyntax
	// vocab is the dialect's vocabulary: a map from a dialect-visible name to
	// either a canonical shared builtin name (a rename) or a GoFunc that wraps
	// the shared implementation (an adapter). A nil vocab means the identity
	// dialect — no vocabulary filtering, every builtin plugins register is
	// callable under its registered name.
	vocab map[string]VocabEntry
	// cond is the cond clause-shape axis. Zero value (condNested) is the kernel
	// default: (cond (test body...) ...). condFlat is Clojure-style.
	cond condShape
	// cache holds this value's memoized resolve()/Fingerprint() results, or is
	// nil for an ordinary (non-memoized) Dialect. Every builder method below
	// clears it on the copy it returns: a Dialect value is copied by every
	// builder, so a cache left in place would silently serve the pre-mutation
	// base's resolved table and fingerprint instead of the mutated one. See
	// Memoized.
	cache *dialectCache
	// st is the frozen state of a Dialect built by NewDialect, or nil. Like
	// cache, every builder method clears it on the copy it returns.
	st *dialectState
}

// dialectState is the resolved, immutable form of a spec-built Dialect. It is
// written once by NewDialect and only read afterwards, so copies of the
// Dialect share it safely.
type dialectState struct {
	table map[string]formFn
	// canon maps every name the dialect knows to its canonical kernel form;
	// a removed name maps to "".
	canon  map[string]string
	doName string
	fp     string
}

// dialectCache is the memoized state for one Memoized Dialect. It is
// populated once, eagerly, by Memoized itself and never written again, so
// sharing the pointer across copies of that Dialect value (and across
// goroutines, once the value has been safely published — e.g. via
// sync.OnceValue) is race-free.
type dialectCache struct {
	table map[string]formFn
	err   error
	fp    string
}

// Memoized returns a copy of d whose resolve() table and Fingerprint() hash
// are computed once, up front, and shared by every copy of the returned
// value. The stock dialect singletons (cl.Dialect, clojure.Dialect) build
// theirs behind sync.OnceValue, so the eager computation below runs exactly
// once per process and is safely published to every caller that follows. A
// hand-built dialect reused across several engines gains the same thing:
// without it, every engine re-resolves the delta chain and re-hashes the
// fingerprint.
//
// Every builder method clears the cache on the Dialect it returns, so
// mutating a Memoized value (Add, Vocabulary, ...) always resolves and
// fingerprints the mutated copy fresh rather than inheriting the base's
// cached answer.
func (d Dialect) Memoized() Dialect {
	d.cache = &dialectCache{}
	d.cache.table, d.cache.err = d.resolveUncached()
	d.cache.fp = d.fingerprintUncached()
	return d
}

// FullDialect starts from the full kernel table. With no delta it is the
// identity dialect, reproducing the interpreter's default special forms.
func FullDialect() Dialect { return Dialect{base: baseFull} }

// EmptyDialect starts from an empty table. It is fail-closed: only the forms
// its delta explicitly adds are callable, and kernel forms added by later
// changes never leak in.
func EmptyDialect() Dialect { return Dialect{base: baseEmpty} }

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

	d := Dialect{base: baseFull}
	if spec.Base == BaseEmpty {
		d.base = baseEmpty
	}
	for _, name := range hide {
		d.ops = append(d.ops, deltaOp{kind: opRemove, name: name})
	}
	for _, name := range slices.Sorted(maps.Keys(spec.Forms)) {
		d.ops = append(d.ops, deltaOp{kind: opAdd, name: name, canonical: spec.Forms[name]})
	}
	if spec.Lisp2 {
		d.ns = nsLisp2
	}
	if spec.NoBrackets {
		d.brackets = bracketsOff
	}
	if spec.FunctionRef {
		d.funcRef = funcRefOn
	}
	if spec.ReaderVector {
		d.readerVec = readerVecOn
	}
	if spec.FlatCond {
		d.cond = condFlat
	}
	if spec.Vocab != nil || spec.Adapters != nil {
		d.vocab = make(map[string]VocabEntry, len(spec.Vocab)+len(spec.Adapters))
		for name, canonical := range spec.Vocab {
			d.vocab[name] = VocabEntry{Canonical: canonical}
		}
		for name, a := range spec.Adapters {
			d.vocab[name] = VocabEntry{AdapterID: a.ID, Adapter: a.Value}
		}
	}

	table, err := d.resolveUncached()
	if err != nil {
		return Dialect{}, err
	}
	canon := make(map[string]string, len(table)+len(d.ops))
	for _, name := range knownNames(table, d.ops) {
		if c, removed, ok := d.canonicalNameUncached(name); ok {
			if removed {
				c = ""
			}
			canon[name] = c
		}
	}
	d.st = &dialectState{
		table:  table,
		canon:  canon,
		doName: frozenDoName(canon),
		fp:     d.fingerprintUncached(),
	}
	return d, nil
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

// knownNames lists every name a resolved table or its delta mentions: the
// callable names plus the ones the delta removed or renamed away.
func knownNames(table map[string]formFn, ops []deltaOp) []string {
	names := slices.Collect(maps.Keys(table))
	for _, op := range ops {
		names = append(names, op.name)
		if op.canonical != "" {
			names = append(names, op.canonical)
		}
	}
	return names
}

// frozenDoName picks the visible name of the do form: do itself when visible,
// else the smallest visible alias, else "do".
func frozenDoName(canon map[string]string) string {
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

// Add exposes the kernel form canonical under name.
func (d Dialect) Add(name, canonical string) Dialect {
	return d.with(deltaOp{kind: opAdd, name: name, canonical: canonical})
}

// Rename exposes the kernel form canonical under to and drops the canonical
// name, unless a later op re-adds it.
func (d Dialect) Rename(canonical, to string) Dialect {
	return d.with(deltaOp{kind: opRename, name: to, canonical: canonical})
}

// Remove makes name uncallable.
func (d Dialect) Remove(name string) Dialect {
	return d.with(deltaOp{kind: opRemove, name: name})
}

// FlatCond sets the cond clause-shape axis so cond parses flat test/expression
// pairs (Clojure-style): (cond t1 e1 t2 e2 ...). The default axis keeps nested
// clauses (Common Lisp-style).
func (d Dialect) FlatCond() Dialect { d.cond = condFlat; d.cache, d.st = nil, nil; return d }

// Vocabulary sets a name→canonical-name map: each visible name resolves to
// the GoFunc the canonical name was registered under. A nil vocab (the zero
// value, the identity Dialect) leaves every registered builtin callable under
// its registered name. On an EmptyDialect the vocabulary is fail-closed: a
// builtin whose registered name is not in the map is removed from the env.
func (d Dialect) Vocabulary(vocab map[string]string) Dialect {
	d.vocab = make(map[string]VocabEntry, len(vocab))
	for name, canonical := range vocab {
		d.vocab[name] = VocabEntry{Canonical: canonical}
	}
	d.cache, d.st = nil, nil
	return d
}

// WithAdapter binds a visible name to a GoFunc that wraps a shared
// implementation. Use it for semantics-differing names where a plain rename
// is not enough; the adapter itself is expected to delegate to a shared
// builtin rather than reimplement the operation. Calling WithAdapter on a
// Dialect that already has vocabulary entries returns a new Dialect whose
// vocab is a fresh copy plus the adapter — the receiver is not mutated.
func (d Dialect) WithAdapter(name, semanticID string, value Value) Dialect {
	d.vocab = copyVocab(d.vocab)
	d.vocab[name] = VocabEntry{AdapterID: semanticID, Adapter: value}
	d.cache, d.st = nil, nil
	return d
}

// copyVocab returns a fresh map containing the receiver's entries, or an empty
// map if the receiver is nil. It exists so vocab-mutating builders
// (WithAdapter and any future ones) never share the underlying map with the
// previous Dialect.
func copyVocab(src map[string]VocabEntry) map[string]VocabEntry {
	dst := make(map[string]VocabEntry, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Vocab returns a caller-owned copy of the vocabulary map the Dialect was
// configured with. It is nil for the identity dialect and a non-nil empty map
// for an empty vocabulary. Each visible name maps to either a canonical shared
// builtin name (Canonical) or an adapter (Adapter non-nil). Writes to the
// returned map never reach the Dialect; adapter Values are shared immutable
// values, not copies.
func (d Dialect) Vocab() map[string]VocabEntry {
	return maps.Clone(d.vocab)
}

// VocabEntry returns the vocabulary entry bound to name, and whether one
// exists. It does not copy the vocabulary.
func (d Dialect) VocabEntry(name string) (VocabEntry, bool) {
	entry, ok := d.vocab[name]
	return entry, ok
}

// CanonicalName maps a visible special-form name to its canonical kernel name
// under this Dialect if the name is a known special form (possibly renamed).
// It returns:
//   - canonical, false, true if the name is a known special form (possibly renamed)
//   - "", true, true if the name was removed from this dialect's dispatch table
//   - "", false, false if the name is not a special form at all in this dialect
func (d Dialect) CanonicalName(name string) (canonical string, removed bool, ok bool) {
	if d.st != nil {
		c, ok := d.st.canon[name]
		if !ok {
			return "", false, false
		}
		return c, c == "", true
	}
	return d.canonicalNameUncached(name)
}

func (d Dialect) canonicalNameUncached(name string) (canonical string, removed bool, ok bool) {
	present := false
	affected := false
	if d.base == baseFull {
		if _, ok := kernel[name]; ok {
			canonical = name
			present = true
			affected = true
		}
	}

	for _, op := range d.ops {
		switch op.kind {
		case opAdd:
			if op.name == name {
				canonical = op.canonical
				present = true
				affected = true
			}
		case opRename:
			if op.canonical == name {
				canonical = ""
				present = false
				affected = true
			}
			if op.name == name {
				canonical = op.canonical
				present = true
				affected = true
			}
		case opRemove:
			if op.name == name {
				canonical = ""
				present = false
				affected = true
			}
		}
	}

	if d.ns == nsLisp2 {
		if name == "function" || name == "funcall" {
			return name, false, true
		}
	}
	if present {
		return canonical, false, true
	}
	if affected {
		return "", true, true
	}
	return "", false, false
}

// TruthyFunc returns the predicate used by dialect-specific conditional evaluation.
// Default behavior uses core.IsTruthy (nil and false are falsy).
func (d Dialect) TruthyFunc() func(Value) bool {
	return IsTruthy
}

// IsBaseEmpty reports whether the Dialect starts from an empty base.
func (d Dialect) IsBaseEmpty() bool {
	return d.base == baseEmpty
}

// isTruthy reports whether v is a true value. All dialects treat nil and false
// as falsy, and every other value as truthy.
func (d Dialect) isTruthy(v Value) bool {
	return IsTruthy(v)
}

// Lisp2 sets the namespace axis so a symbol may name a function and a value at
// once: head position resolves through the function cell, definition forms bind
// functions there, and the funcall and function (#') forms become available.
// The default axis is Lisp-1, a single namespace.
func (d Dialect) Lisp2() Dialect {
	d.ns = nsLisp2
	d.cache, d.st = nil, nil
	return d
}

// isLisp2 reports whether the Dialect uses a separate function cell. It is the
// single hook eval consults to split head from argument resolution.
func (d Dialect) isLisp2() bool {
	return d.ns == nsLisp2
}

// IsLisp2 reports whether d uses a separate function cell (Lisp-2).
func (d Dialect) IsLisp2() bool { return d.isLisp2() }

// WithoutBracketLiterals turns off [..]/{..} literal syntax, so those brackets
// stop reading as vector/map literals (Common Lisp-style). The default axis
// keeps bracket literals on.
func (d Dialect) WithoutBracketLiterals() Dialect {
	d.brackets = bracketsOff
	d.cache, d.st = nil, nil
	return d
}

// WithFunctionRef enables the #' reader syntax, so #'x reads as (function x).
// The default axis leaves # non-special. What (function x) means once read is
// defined by the namespace axis; this flag only makes it parse.
func (d Dialect) WithFunctionRef() Dialect {
	d.funcRef = funcRefOn
	d.cache, d.st = nil, nil
	return d
}

// WithReaderVector enables the #(...) reader syntax, so #(...) reads as a
// vector. The default axis leaves # non-special.
func (d Dialect) WithReaderVector() Dialect {
	d.readerVec = readerVecOn
	d.cache, d.st = nil, nil
	return d
}

// readerFlags projects the reader axes onto the flag set the tokenizer consults.
func (d Dialect) readerFlags() readerFlags {
	return readerFlags{
		bracketLiterals: d.brackets == bracketsOn,
		functionRef:     d.funcRef == funcRefOn,
		readerVector:    d.readerVec == readerVecOn,
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

// IsIdentity reports whether d is the identity dialect — the full kernel base
// with no delta and no vocabulary. The bytecode VM dispatches canonical form
// names directly, so only the identity dialect is safe to run under it.
func (d Dialect) IsIdentity() bool {
	return d.base == baseFull && len(d.ops) == 0 &&
		d.ns == nsLisp1 && d.brackets == bracketsOn &&
		d.funcRef == funcRefOff && d.readerVec == readerVecOff &&
		d.vocab == nil
}

func (d Dialect) with(op deltaOp) Dialect {
	ops := make([]deltaOp, len(d.ops), len(d.ops)+1)
	copy(ops, d.ops)
	d.ops = append(ops, op)
	d.cache, d.st = nil, nil
	return d
}

// resolve returns the Dialect's effective dispatch table. A Memoized value
// returns its cached table on every call; any other Dialect resolves fresh
// each time (see resolveUncached).
func (d Dialect) resolve() (map[string]formFn, error) {
	if d.st != nil {
		return d.st.table, nil
	}
	if d.cache != nil {
		return d.cache.table, d.cache.err
	}
	return d.resolveUncached()
}

// resolveUncached applies the delta to a fresh copy of the base, producing
// the effective dispatch table. It fails if a rename or add references a
// canonical form absent from the kernel.
func (d Dialect) resolveUncached() (map[string]formFn, error) {
	table := make(map[string]formFn, len(kernel))
	if d.base == baseFull {
		maps.Copy(table, kernel)
	}
	for name, entry := range d.vocab {
		if entry.Adapter != nil && entry.AdapterID == "" {
			return nil, fmt.Errorf("dialect: adapter %q has no semantic ID", name)
		}
	}
	for _, op := range d.ops {
		switch op.kind {
		case opAdd:
			fn, ok := kernel[op.canonical]
			if !ok {
				return nil, fmt.Errorf("dialect: add references unknown kernel form %q", op.canonical)
			}
			table[op.name] = fn
		case opRename:
			fn, ok := kernel[op.canonical]
			if !ok {
				return nil, fmt.Errorf("dialect: rename references unknown kernel form %q", op.canonical)
			}
			delete(table, op.canonical)
			table[op.name] = fn
		case opRemove:
			delete(table, op.name)
		}
	}
	// funcall and function are intrinsic to the Lisp-2 axis, not kernel forms, so
	// they are injected here rather than referenced through Add/Rename/Remove.
	// Injecting after the delta means the axis owns these two names.
	if d.ns == nsLisp2 {
		table["funcall"] = evalFuncall
		table["function"] = evalFunction
	}
	return table, nil
}

// Fingerprint returns a hash string that changes when the Dialect's semantic
// configuration changes. Used as part of the bytecode chunk cache key. A
// Memoized value returns its cached hash on every call; any other Dialect
// hashes fresh each time (see fingerprintUncached).
//
// The fingerprint is a process-local identity for one go-lispico version: it
// may change between releases and is not a persistence format.
func (d Dialect) Fingerprint() string {
	if d.st != nil {
		return d.st.fp
	}
	if d.cache != nil {
		return d.cache.fp
	}
	return d.fingerprintUncached()
}

// fingerprintUncached computes d's fingerprint hash from scratch.
func (d Dialect) fingerprintUncached() string {
	h := sha256.New()
	fmt.Fprintf(h, "base=%d|ns=%d|brackets=%d|funcRef=%d|readerVec=%d|cond=%d",
		d.base, d.ns, d.brackets, d.funcRef, d.readerVec, d.cond)
	for _, op := range d.ops {
		fmt.Fprintf(h, "|%d", op.kind)
		writeField(h, op.name)
		writeField(h, op.canonical)
	}
	// Sort vocabulary keys for stable order.
	if len(d.vocab) > 0 {
		keys := make([]string, 0, len(d.vocab))
		for k := range d.vocab {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			entry := d.vocab[k]
			fmt.Fprint(h, "|v")
			writeField(h, k)
			writeField(h, entry.Canonical)
			writeField(h, entry.AdapterID)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// writeField writes s as ":<byte length>:<bytes>" so no string content can
// shift a field boundary and collide with a different field split.
func writeField(w io.Writer, s string) {
	fmt.Fprintf(w, ":%d:%s", len(s), s)
}

// visibleName returns the dialect-visible name for a canonical kernel form.
// It scans the delta ops; with no rename/add targeting canonical, the canonical
// name is itself the visible name (identity behavior).
func (d Dialect) visibleName(canonical string) string {
	if d.st != nil && canonical == "do" {
		return d.st.doName
	}
	for i := len(d.ops) - 1; i >= 0; i-- {
		op := d.ops[i]
		if op.canonical == canonical && (op.kind == opRename || op.kind == opAdd) {
			return op.name
		}
	}
	return canonical
}

// NormalizeCond parses raw cond operands into canonical (test body) clauses
// under the Dialect's cond clause-shape axis. One canonical clause is one test
// plus one body expression; a Common Lisp nested clause with a multi-expression
// implicit-progn body normalizes by wrapping the body in the dialect-visible
// `do` form. Both the Evaluator and the Compiler call this, so the two paths
// cannot parse cond differently.
func (d Dialect) NormalizeCond(args []Value) ([]Value, error) {
	switch d.cond {
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
			doName := d.visibleName("do")
			wrapped := make([]Value, 0, n)
			wrapped = append(wrapped, Symbol{V: doName})
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

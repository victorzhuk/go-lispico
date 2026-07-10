package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
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
		"unless":     evalUnless,
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

// truthiness is the Dialect's falsy rule. The zero value keeps both nil and
// false falsy, so a Dialect built without touching the axis behaves as before.
type truthiness int

const (
	truthNilFalse truthiness = iota // nil and false are falsy (Clojure-style)
	truthNilOnly                    // only nil is falsy (Common Lisp-style)
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
	truth     truthiness
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
}

// FullDialect starts from the full kernel table. With no delta it is the
// identity dialect, reproducing the interpreter's default special forms.
func FullDialect() Dialect { return Dialect{base: baseFull} }

// EmptyDialect starts from an empty table. It is fail-closed: only the forms
// its delta explicitly adds are callable, and kernel forms added by later
// changes never leak in.
func EmptyDialect() Dialect { return Dialect{base: baseEmpty} }

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
	return d
}

// WithAdapter binds a visible name to a GoFunc that wraps a shared
// implementation. Use it for semantics-differing names where a plain rename
// is not enough; the adapter itself is expected to delegate to a shared
// builtin rather than reimplement the operation. Calling WithAdapter on a
// Dialect that already has vocabulary entries returns a new Dialect whose
// vocab is a fresh copy plus the adapter — the receiver is not mutated.
func (d Dialect) WithAdapter(name string, fn Value) Dialect {
	d.vocab = copyVocab(d.vocab)
	d.vocab[name] = VocabEntry{Adapter: fn}
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

// Vocab returns the vocabulary map the Dialect was configured with. It is nil
// for the identity dialect. Each visible name maps to either a canonical
// shared builtin name (Canonical) or an adapter (Adapter non-nil).
func (d Dialect) Vocab() map[string]VocabEntry {
	return d.vocab
}

// CanonicalName maps a visible special-form name to its canonical kernel name
// under this Dialect if the name is a known special form (possibly renamed).
// It returns:
//   - canonical, false, true if the name is a known special form (possibly renamed)
//   - "", true, true if the name was explicitly removed
//   - "", false, false if the name is not a special form at all in this dialect
func (d Dialect) CanonicalName(name string) (canonical string, removed bool, ok bool) {
	// Walk delta ops in reverse so the most recent change to this name wins.
	for i := len(d.ops) - 1; i >= 0; i-- {
		op := d.ops[i]
		if op.name == name {
			switch op.kind {
			case opRename:
				return op.canonical, false, true
			case opAdd:
				return op.canonical, false, true
			case opRemove:
				return "", true, true
			}
		}
	}
	// Lisp-2 injects function and funcall into the resolved form table.
	if d.ns == nsLisp2 {
		if name == "function" || name == "funcall" {
			return name, false, true
		}
	}
	if d.base == baseFull {
		if _, ok := kernel[name]; ok {
			return name, false, true
		}
	}
	return "", false, false
}

// TruthyFunc returns the Dialect's truthiness predicate for conditional forms.
// Returns nil for the default (nil+false falsy) meaning the caller can use core.IsTruthy.
func (d Dialect) TruthyFunc() func(Value) bool {
	if d.truth == truthNilOnly {
		return func(v Value) bool {
			_, isNil := v.(Nil)
			return !isNil
		}
	}
	return nil
}

// IsBaseEmpty reports whether the Dialect starts from an empty base.
func (d Dialect) IsBaseEmpty() bool {
	return d.base == baseEmpty
}

// NilOnlyFalsy sets the truthiness axis so only nil is falsy; false becomes a
// true value. The default axis keeps both nil and false falsy.
func (d Dialect) NilOnlyFalsy() Dialect {
	d.truth = truthNilOnly
	return d
}

// isTruthy reports whether v is a true value under the Dialect's truthiness
// axis. It is the single hook the conditional special forms consult.
func (d Dialect) isTruthy(v Value) bool {
	if d.truth == truthNilOnly {
		_, isNil := v.(Nil)
		return !isNil
	}
	return IsTruthy(v)
}

// Lisp2 sets the namespace axis so a symbol may name a function and a value at
// once: head position resolves through the function cell, definition forms bind
// functions there, and the funcall and function (#') forms become available.
// The default axis is Lisp-1, a single namespace.
func (d Dialect) Lisp2() Dialect {
	d.ns = nsLisp2
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
	return d
}

// WithFunctionRef enables the #' reader syntax, so #'x reads as (function x).
// The default axis leaves # non-special. What (function x) means once read is
// defined by the namespace axis; this flag only makes it parse.
func (d Dialect) WithFunctionRef() Dialect {
	d.funcRef = funcRefOn
	return d
}

// WithReaderVector enables the #(...) reader syntax, so #(...) reads as a
// vector. The default axis leaves # non-special.
func (d Dialect) WithReaderVector() Dialect {
	d.readerVec = readerVecOn
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
	r := NewReaderWithFlags(src, d.readerFlags())
	tokens, err := r.Tokenize()
	if err != nil {
		return nil, err
	}
	p := NewParserWithDepth(tokens, maxDepth)
	var forms []Value
	for p.peek().typ != tokenEOF {
		form, err := p.Parse()
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, nil
}

// IsIdentity reports whether d is the identity dialect — the full kernel base
// with no delta and no vocabulary. The bytecode VM dispatches canonical form
// names directly, so only the identity dialect is safe to run under it.
func (d Dialect) IsIdentity() bool {
	return d.base == baseFull && len(d.ops) == 0 &&
		d.truth == truthNilFalse && d.ns == nsLisp1 &&
		d.brackets == bracketsOn && d.funcRef == funcRefOff && d.readerVec == readerVecOff &&
		d.vocab == nil
}

func (d Dialect) with(op deltaOp) Dialect {
	ops := make([]deltaOp, len(d.ops), len(d.ops)+1)
	copy(ops, d.ops)
	d.ops = append(ops, op)
	return d
}

// resolve applies the delta to a fresh copy of the base, producing the
// effective dispatch table. It fails if a rename or add references a canonical
// form absent from the kernel.
func (d Dialect) resolve() (map[string]formFn, error) {
	table := make(map[string]formFn, len(kernel))
	if d.base == baseFull {
		maps.Copy(table, kernel)
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

// Fingerprint returns a stable hash string that changes when the Dialect's
// semantic configuration changes. Used as part of the bytecode chunk cache key.
func (d Dialect) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "base=%d|truth=%d|ns=%d|brackets=%d|funcRef=%d|readerVec=%d",
		d.base, d.truth, d.ns, d.brackets, d.funcRef, d.readerVec)
	for _, op := range d.ops {
		fmt.Fprintf(h, "|%d:%s:%s", op.kind, op.name, op.canonical)
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
			fmt.Fprintf(h, "|v:%s:%s:", k, entry.Canonical)
			if entry.Adapter != nil {
				fmt.Fprintf(h, "%T", entry.Adapter)
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

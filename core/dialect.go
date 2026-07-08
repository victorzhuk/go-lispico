package core

import (
	"context"
	"fmt"
	"maps"
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

// Dialect describes an Engine's special-form table as a delta over a base. The
// base is either the full kernel table or empty; the delta renames, adds, or
// removes forms. Resolving a Dialect yields the effective name→form table an
// Engine dispatches through. A Dialect is an immutable value: the builder
// methods return a new Dialect and never mutate the receiver.
type Dialect struct {
	base  dialectBase
	ops   []deltaOp
	truth truthiness
	ns    namespace
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

// IsIdentity reports whether d is the identity dialect — the full kernel base
// with no delta. The bytecode VM dispatches canonical form names directly, so
// only the identity dialect is safe to run under it.
func (d Dialect) IsIdentity() bool {
	return d.base == baseFull && len(d.ops) == 0 &&
		d.truth == truthNilFalse && d.ns == nsLisp1
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

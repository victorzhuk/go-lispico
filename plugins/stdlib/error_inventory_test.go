package stdlib

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// errorSite is one concrete error-return site, classified by the contract it
// reports as violated rather than by the Go type it currently returns.
//
// Fn holds one or more space-separated Lisp names: a site inside a shared
// factory (unaryMathFunc, orderingFunc, unaryStringFunc) or a shared helper
// (chargeConsResult, IndexedAccess) is one site serving several builtins, and
// splitting it into a row per name would inflate the per-file site counts this
// inventory reconciles against.
type errorSite struct {
	Family, Fn, File, Func, Class string
	Reachable                     bool
}

const (
	fileArith      = "plugins/stdlib/arithmetic.go"
	fileBootstrap  = "plugins/stdlib/bootstrap.go"
	fileCollection = "plugins/stdlib/collections.go"
	fileComparison = "plugins/stdlib/comparison.go"
	fileControl    = "plugins/stdlib/control.go"
	fileHigher     = "plugins/stdlib/higher_order.go"
	filePlugin     = "plugins/stdlib/plugin.go"
	fileStrings    = "plugins/stdlib/strings.go"
	fileTypes      = "plugins/stdlib/types.go"
	fileKernels    = "internal/collections/kernels.go"
	fileOrder      = "internal/collections/order.go"
	fileCL         = "cl/cl.go"

	// fileBoundary is the single non-stdlib origin: toHashKey's plain error
	// crosses into stdlib through HashMap.Set/Assoc/Dissoc and is converted at
	// the stdlib boundary. core/ is not modified by this change, so the row is
	// recorded as out of the static-ban scope.
	fileBoundary = "core/types.go"
)

func originFiles() []string {
	return []string{
		fileArith, fileBootstrap, fileCollection, fileComparison, fileControl,
		fileHigher, filePlugin, fileStrings, fileTypes,
		fileKernels, fileOrder, fileCL,
	}
}

func errorClasses() []string {
	return []string{
		"ArityError",
		"TypeError",
		"EvalError",
		"terminal-passthrough",
		"callback-passthrough",
		"external-conversion",
	}
}

var errorInventory = []errorSite{
	{Family: "Arithmetic", Fn: "+", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "-", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "-", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "-", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "*", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "/", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "/", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "/", File: fileArith, Func: "registerArithmetic", Class: "EvalError", Reachable: true},
	{Family: "Arithmetic", Fn: "/", File: fileArith, Func: "registerArithmetic", Class: "EvalError", Reachable: true},
	{Family: "Arithmetic", Fn: "/", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "mod", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "mod", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "mod", File: fileArith, Func: "registerArithmetic", Class: "EvalError", Reachable: true},
	{Family: "Arithmetic", Fn: "quot", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "quot", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "quot", File: fileArith, Func: "registerArithmetic", Class: "EvalError", Reachable: true},
	{Family: "Arithmetic", Fn: "pow", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "pow", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "pow", File: fileArith, Func: "registerArithmetic", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "zero?", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "pos?", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "neg?", File: fileArith, Func: "registerArithmetic", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "sqrt abs floor ceil", File: fileArith, Func: "unaryMathFunc", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "sqrt abs floor ceil", File: fileArith, Func: "unaryMathFunc", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "max min", File: fileArith, Func: "minMaxFunc", Class: "ArityError", Reachable: true},
	{Family: "Arithmetic", Fn: "max min", File: fileArith, Func: "minMaxFunc", Class: "TypeError", Reachable: true},
	{Family: "Arithmetic", Fn: "max min", File: fileArith, Func: "minMaxFunc", Class: "TypeError", Reachable: true},

	{Family: "Comparison", Fn: "=", File: fileComparison, Func: "registerComparison", Class: "ArityError", Reachable: true},
	{Family: "Comparison", Fn: "< > <= >=", File: fileComparison, Func: "orderingFunc", Class: "ArityError", Reachable: true},
	{Family: "Comparison", Fn: "< > <= >=", File: fileComparison, Func: "orderingFunc", Class: "TypeError", Reachable: true},
	{Family: "Comparison", Fn: "< > <= >=", File: fileComparison, Func: "orderingFunc", Class: "TypeError", Reachable: true},

	{Family: "Control", Fn: "assert", File: fileControl, Func: "registerControl", Class: "ArityError", Reachable: true},
	{Family: "Control", Fn: "assert", File: fileControl, Func: "registerControl", Class: "EvalError", Reachable: true},
	{Family: "Control", Fn: "assert", File: fileControl, Func: "registerControl", Class: "EvalError", Reachable: true},
	{Family: "Control", Fn: "assert", File: fileControl, Func: "registerControl", Class: "EvalError", Reachable: true},

	{Family: "Higher order", Fn: "map", File: fileHigher, Func: "registerHigherOrder", Class: "ArityError", Reachable: true},
	{Family: "Higher order", Fn: "map", File: fileHigher, Func: "registerHigherOrder", Class: "TypeError", Reachable: true},
	{Family: "Higher order", Fn: "filter", File: fileHigher, Func: "registerHigherOrder", Class: "ArityError", Reachable: true},
	{Family: "Higher order", Fn: "filter", File: fileHigher, Func: "registerHigherOrder", Class: "TypeError", Reachable: true},
	{Family: "Higher order", Fn: "filter", File: fileHigher, Func: "registerHigherOrder", Class: "callback-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "reduce", File: fileHigher, Func: "registerHigherOrder", Class: "ArityError", Reachable: true},
	{Family: "Higher order", Fn: "reduce", File: fileHigher, Func: "registerHigherOrder", Class: "TypeError", Reachable: true},
	{Family: "Higher order", Fn: "reduce", File: fileHigher, Func: "registerHigherOrder", Class: "callback-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "apply", File: fileHigher, Func: "registerHigherOrder", Class: "ArityError", Reachable: true},
	{Family: "Higher order", Fn: "apply", File: fileHigher, Func: "registerHigherOrder", Class: "TypeError", Reachable: true},

	{Family: "Types", Fn: "type", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "nil?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "bool?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "int?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "float?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "string?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "keyword?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "symbol?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "list?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "vector?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "map?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "fn?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "macro?", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "str->keyword", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "str->keyword", File: fileTypes, Func: "registerTypes", Class: "TypeError", Reachable: true},
	{Family: "Types", Fn: "keyword->str", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "keyword->str", File: fileTypes, Func: "registerTypes", Class: "TypeError", Reachable: true},
	{Family: "Types", Fn: "int->float", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "int->float", File: fileTypes, Func: "registerTypes", Class: "TypeError", Reachable: true},
	{Family: "Types", Fn: "float->int", File: fileTypes, Func: "registerTypes", Class: "ArityError", Reachable: true},
	{Family: "Types", Fn: "float->int", File: fileTypes, Func: "registerTypes", Class: "TypeError", Reachable: true},

	{Family: "Strings", Fn: "format", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "format", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "format", File: fileStrings, Func: "registerStrings", Class: "terminal-passthrough", Reachable: true},
	{Family: "Strings", Fn: "string/join", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/join", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/join", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/split", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/split", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/replace", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/replace", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/contains?", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/contains?", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/starts-with?", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/starts-with?", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/ends-with?", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/ends-with?", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/length", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/length", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string/lines", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/lines", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string->int", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string->int", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string->int", File: fileStrings, Func: "registerStrings", Class: "external-conversion", Reachable: true},
	{Family: "Strings", Fn: "string->float", File: fileStrings, Func: "registerStrings", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string->float", File: fileStrings, Func: "registerStrings", Class: "TypeError", Reachable: true},
	{Family: "Strings", Fn: "string->float", File: fileStrings, Func: "registerStrings", Class: "external-conversion", Reachable: true},
	{Family: "Strings", Fn: "string/trim string/upper string/lower", File: fileStrings, Func: "unaryStringFunc", Class: "ArityError", Reachable: true},
	{Family: "Strings", Fn: "string/trim string/upper string/lower", File: fileStrings, Func: "unaryStringFunc", Class: "TypeError", Reachable: true},

	{Family: "Collections", Fn: "list", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "concat", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "concat", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "concat", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "concat", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "reverse", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "reverse", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "vector", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "hash-map", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "hash-map", File: fileCollection, Func: "registerCollections", Class: "external-conversion", Reachable: true},
	{Family: "Collections", Fn: "hash-map", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "first", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "first", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "rest", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "rest", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "last", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "last", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileCollection, Func: "registerCollections", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "count", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "count", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "cons", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "cons", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "cons", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "cons", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "external-conversion", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "conj", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "empty?", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "get", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "get", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "get", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "assoc", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "assoc", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "assoc", File: fileCollection, Func: "registerCollections", Class: "external-conversion", Reachable: true},
	{Family: "Collections", Fn: "assoc", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "keys", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "keys", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "vals", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "vals", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "contains?", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "contains?", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "merge", File: fileCollection, Func: "registerCollections", Class: "external-conversion", Reachable: true},
	{Family: "Collections", Fn: "merge", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "merge", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "dissoc", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "dissoc", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "dissoc", File: fileCollection, Func: "registerCollections", Class: "external-conversion", Reachable: true},
	{Family: "Collections", Fn: "dissoc", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileCollection, Func: "registerCollections", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileCollection, Func: "registerCollections", Class: "callback-passthrough", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "range", File: fileCollection, Func: "registerCollections", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "concat", File: fileCollection, Func: "appendCollectionElems", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInLookup", Class: "ArityError", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInLookup", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInLookup", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInLookup", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInResult", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "get-in", File: fileCollection, Func: "getInResult", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "list concat vector hash-map merge range", File: fileCollection, Func: "chargeCollectionResult", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "list concat vector hash-map merge range", File: fileCollection, Func: "chargeCollectionResult", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "cons conj assoc dissoc", File: fileCollection, Func: "chargeConsResult", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "cons conj assoc dissoc", File: fileCollection, Func: "chargeConsResult", Class: "terminal-passthrough", Reachable: true},

	{Family: "Bootstrap", Fn: "stdlib bootstrap", File: fileBootstrap, Func: "loadBootstrap", Class: "TypeError", Reachable: true},
	{Family: "Bootstrap", Fn: "stdlib bootstrap", File: fileBootstrap, Func: "loadBootstrap", Class: "callback-passthrough", Reachable: true},
	{Family: "Bootstrap", Fn: "stdlib bootstrap", File: fileBootstrap, Func: "loadBootstrap", Class: "callback-passthrough", Reachable: true},

	{Family: "Collections", Fn: "nth", File: fileKernels, Func: "IndexedAccess", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileKernels, Func: "IndexedAccess", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileKernels, Func: "IndexedAccess", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "nth", File: fileKernels, Func: "IndexedAccess", Class: "terminal-passthrough", Reachable: true},
	// MapSequences' default branch is dead from both live callers: registerHigherOrder
	// pre-checks List|Vector and clMapcar pre-checks List|Nil, so no sequence type
	// reaches it. Recorded, never driven.
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "MapSequences", Class: "TypeError", Reachable: false},
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "MapSequences", Class: "terminal-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "MapSequences", Class: "terminal-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "MapSequences", Class: "callback-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "MapSequences", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "StableSort", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "StableSort", Class: "callback-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "StableSort", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "StableSort", Class: "callback-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "StableSort", Class: "terminal-passthrough", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileKernels, Func: "finishSort", Class: "terminal-passthrough", Reachable: true},
	{Family: "Higher order", Fn: "map mapcar", File: fileKernels, Func: "flushErr", Class: "terminal-passthrough", Reachable: true},

	{Family: "Comparison", Fn: "< > <= >= sort", File: fileOrder, Func: "ToFloat", Class: "TypeError", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileOrder, Func: "NaturalCmp", Class: "EvalError", Reachable: true},
	{Family: "Collections", Fn: "sort", File: fileOrder, Func: "NaturalCmp", Class: "EvalError", Reachable: true},

	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "ArityError", Reachable: true},
	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "EvalError", Reachable: true},
	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "terminal-passthrough", Reachable: true},
	{Family: "CL adapters", Fn: "nth", File: fileCL, Func: "clNth", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "mapcar", File: fileCL, Func: "clMapcar", Class: "ArityError", Reachable: true},
	{Family: "CL adapters", Fn: "mapcar", File: fileCL, Func: "clMapcar", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "mapcar", File: fileCL, Func: "clMapcar", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "ArityError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "ArityError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "EvalError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "EvalError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "TypeError", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "callback-passthrough", Reachable: true},
	{Family: "CL adapters", Fn: "sort", File: fileCL, Func: "clSort", Class: "callback-passthrough", Reachable: true},

	{Family: "Collections", Fn: "hash-map assoc conj dissoc merge", File: fileBoundary, Func: "toHashKey", Class: "external-conversion", Reachable: true},
}

// clAdapterNames mirrors cl/cl.go's WithAdapter calls as literals. Building
// them through a runtime Engine would import cl into the stdlib test binary
// and make the dialect's own wiring the thing under test.
func clAdapterNames() []string { return []string{"nth", "mapcar", "sort"} }

func TestErrorInventoryRowsAreWellFormed(t *testing.T) {
	classes := map[string]bool{}
	for _, c := range errorClasses() {
		classes[c] = true
	}
	files := map[string]bool{fileBoundary: true}
	for _, f := range originFiles() {
		files[f] = true
	}

	require.NotEmpty(t, errorInventory)
	for i, site := range errorInventory {
		require.NotEmptyf(t, site.Fn, "row %d (%s) has no Fn", i, site.File)
		require.Truef(t, classes[site.Class], "row %d (%s %s) has class %q outside the six", i, site.File, site.Fn, site.Class)
		require.Truef(t, files[site.File], "row %d (%s) names file %q outside the origin set", i, site.Fn, site.File)
	}
}

func TestErrorInventoryCoversRegistrationSurface(t *testing.T) {
	env := core.NewEnv(nil)
	require.NoError(t, New().Init(env))

	// The surface is read back from the env rather than hand-listed so a newly
	// added builtin fails this test instead of slipping past the inventory.
	surface := map[string]bool{}
	for _, name := range env.VarNames() {
		v, ok := env.Get(name)
		if !ok {
			continue
		}
		if _, isBuiltin := v.(core.GoFunc); isBuiltin {
			surface[name] = true
		}
	}
	require.Equal(t, 81, len(surface), "registered GoFunc surface changed")

	covered := map[string]bool{}
	for _, site := range errorInventory {
		for _, name := range strings.Fields(site.Fn) {
			covered[name] = true
		}
	}

	for _, name := range clAdapterNames() {
		require.Truef(t, covered[name], "CL adapter %q has no inventory row", name)
	}

	var uncovered []string
	for name := range surface {
		if !covered[name] {
			uncovered = append(uncovered, name)
		}
	}
	sort.Strings(uncovered)
	// str is the only total builtin: it concatenates whatever it is given and
	// has no error-return site to inventory.
	require.Equal(t, []string{"str"}, uncovered)
}

func TestErrorInventoryFilesAreOriginFiles(t *testing.T) {
	origins := map[string]bool{}
	for _, f := range originFiles() {
		origins[f] = true
	}

	var boundary []errorSite
	for _, site := range errorInventory {
		if site.File == fileBoundary {
			boundary = append(boundary, site)
			continue
		}
		require.Truef(t, origins[site.File], "%s (%s) is not one of the 12 origin files", site.File, site.Fn)
	}

	require.Len(t, boundary, 1, "core/types.go is the single documented non-origin row")
	require.Equal(t, "external-conversion", boundary[0].Class)
	require.Equal(t, "toHashKey", boundary[0].Func)
}

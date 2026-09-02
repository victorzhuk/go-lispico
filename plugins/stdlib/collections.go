package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/collections"
)

const defaultStdlibCollectionLen = 10_000_000

func (p *Plugin) registerCollections(env *core.Env) error {
	for _, b := range collectionBuiltins() {
		if err := env.RegisterValue(b.Name, b, false); err != nil {
			return err
		}
	}
	return nil
}

// collectionBuiltins is the collection seam's builtin table. The bodies live
// here rather than in the registrar so no work budget is ever opened in a
// function that also returns after it: every budget below belongs to the call
// that opened it and is settled through a finish helper on the way out.
func collectionBuiltins() []core.GoFunc {
	return []core.GoFunc{
		{
			Name: "list",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				budget := core.NewBuiltinWorkBudget(ctx)
				items := make([]core.Value, len(args))
				for i, arg := range args {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					items[i] = arg
				}
				res := core.NewList(items)
				if err := chargeCollectionResult(ctx, eval, "list", res, core.ValueDeepBytes(res)); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, res, nil)
			},
		},

		{
			Name: "concat",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) == 0 {
					if err := chargeFreshContainer(ctx, core.ListShallowBytes(0)); err != nil {
						return nil, err
					}
					return core.NewList(nil), nil
				}

				budget := core.NewBuiltinWorkBudget(ctx)

				// The last argument extends without copying when it's a List:
				// every earlier argument's elements get Cons'd onto it in
				// reverse, so baseList's own backing — however long — is never
				// walked or duplicated. A Vector or Nil last argument falls
				// back to a full flatten, same as before.
				baseList, sharesLast := args[len(args)-1].(core.List)
				if !sharesLast {
					var result []core.Value
					for _, arg := range args {
						if err := appendCollectionElems(budget, "concat", &result, arg); err != nil {
							return finishBuiltin(budget, nil, err)
						}
					}
					res := core.NewList(result)
					if err := chargeCollectionResult(ctx, eval, "concat", res, core.ListShallowBytes(len(result))); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					return finishBuiltin(budget, res, nil)
				}

				var prefix []core.Value
				for _, arg := range args[:len(args)-1] {
					if err := appendCollectionElems(budget, "concat", &prefix, arg); err != nil {
						return finishBuiltin(budget, nil, err)
					}
				}
				res := baseList
				var bytes int64
				for i := len(prefix) - 1; i >= 0; i-- {
					var b int64
					res, b = res.Cons(prefix[i])
					bytes += b
				}
				if err := chargeCollectionResult(ctx, eval, "concat", res, bytes); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, res, nil)
			},
		},

		{
			Name: "reverse",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("reverse: requires 1 argument")
				}

				items, ok := seqInput(args[0])
				if !ok {
					return nil, typeErrorf("reverse: expected collection, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				// seqInput copied the whole subject in one ToSlice call. That walk
				// is invisible to the budget unless it is charged here, so the
				// copy is billed before the reversal it feeds.
				for range items {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
				}
				result := make([]core.Value, len(items))
				for i, v := range items {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					result[len(items)-1-i] = v
				}
				// The result borrows every element from the subject, so only the
				// container it allocated is new to the ledger.
				if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(result))); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, core.NewList(result), nil)
			},
		},

		{
			Name: "vector",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				budget := core.NewBuiltinWorkBudget(ctx)
				items := make([]core.Value, len(args))
				for i, arg := range args {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					items[i] = arg
				}
				res := core.NewVector(items)
				if err := chargeCollectionResult(ctx, eval, "vector", res, core.ValueDeepBytes(res)); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, res, nil)
			},
		},

		{
			Name: "hash-map",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args)%2 != 0 {
					return nil, arityErrorf("hash-map: requires even number of arguments")
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				m := core.NewHashMap()
				for i := 0; i < len(args); i += 2 {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					if err := m.Set(args[i], args[i+1]); err != nil {
						return finishBuiltin(budget, nil, wrapCause("hash-map", err))
					}
				}
				// Every entry holds an argument the map now owns, so the result is
				// charged deeply: the apply site's shallow fallback would bill the
				// header alone and miss the whole payload.
				if err := chargeCollectionResult(ctx, eval, "hash-map", m, core.ValueDeepBytes(m)); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, m, nil)
			},
		},

		{
			Name: "first",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("first: requires 1 argument")
				}

				switch c := args[0].(type) {
				case core.List:
					if c.Len() == 0 {
						return core.Nil{}, nil
					}
					if err := chargeBorrowedResult(ctx); err != nil {
						return nil, err
					}
					return c.At(0), nil
				case core.Vector:
					if c.Len() == 0 {
						return core.Nil{}, nil
					}
					if err := chargeBorrowedResult(ctx); err != nil {
						return nil, err
					}
					return c.At(0), nil
				case core.Nil:
					return core.Nil{}, nil
				default:
					return nil, typeErrorf("first: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "rest",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("rest: requires 1 argument")
				}

				switch c := args[0].(type) {
				case core.List:
					// Rest hands back a shared tail or a reslice of the subject's own
					// backing, so the caller ends up owning nothing the ledger has not
					// already seen.
					if err := chargeBorrowedResult(ctx); err != nil {
						return nil, err
					}
					return c.Rest(), nil
				case core.Vector:
					if c.Len() <= 1 {
						if err := chargeFreshContainer(ctx, core.ListShallowBytes(0)); err != nil {
							return nil, err
						}
						return core.NewList([]core.Value{}), nil
					}
					budget := core.NewBuiltinWorkBudget(ctx)
					items := make([]core.Value, c.Len()-1)
					for i := 1; i < c.Len(); i++ {
						if err := budget.Step(); err != nil {
							return finishBuiltin(budget, nil, err)
						}
						items[i-1] = c.At(i)
					}
					// The elements are borrowed from the subject; only the list holding
					// them is new.
					if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(items))); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					return finishBuiltin(budget, core.NewList(items), nil)
				case core.Nil:
					if err := chargeFreshContainer(ctx, core.ListShallowBytes(0)); err != nil {
						return nil, err
					}
					return core.NewList([]core.Value{}), nil
				default:
					return nil, typeErrorf("rest: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "last",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("last: requires 1 argument")
				}

				switch c := args[0].(type) {
				case core.List:
					if c.Len() == 0 {
						return core.Nil{}, nil
					}
					// At walks one node per index on the shared representation and
					// cannot be stopped once it starts, so the whole traversal is
					// charged before it runs.
					budget := core.NewBuiltinWorkBudget(ctx)
					for range c.Len() {
						if err := budget.Step(); err != nil {
							return finishBuiltin(budget, nil, err)
						}
					}
					if err := chargeBorrowedResult(ctx); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					return finishBuiltin(budget, c.At(c.Len()-1), nil)
				case core.Vector:
					if c.Len() == 0 {
						return core.Nil{}, nil
					}
					if err := chargeBorrowedResult(ctx); err != nil {
						return nil, err
					}
					return c.At(c.Len() - 1), nil
				case core.Nil:
					return core.Nil{}, nil
				default:
					return nil, typeErrorf("last: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "nth",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 2 || len(args) > 3 {
					return nil, arityErrorf("nth: requires 2 or 3 arguments")
				}

				idx, ok := args[1].(core.Int)
				if !ok {
					return nil, typeErrorf("nth: index must be integer")
				}

				var val core.Value
				outcome := collections.AccessOutOfRange
				if _, isNil := args[0].(core.Nil); !isNil {
					var err error
					val, outcome, err = collections.IndexedAccess(ctx, args[0], idx.V)
					if err != nil {
						return nil, err
					}
				}
				switch outcome {
				case collections.AccessHit:
					if err := chargeBorrowedResult(ctx); err != nil {
						return nil, err
					}
					return val, nil
				case collections.AccessOutOfRange:
					if len(args) == 3 {
						if err := chargeBorrowedResult(ctx); err != nil {
							return nil, err
						}
						return args[2], nil
					}
					return nil, domainErrorf("nth: index out of bounds")
				default:
					return nil, typeErrorf("nth: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "count",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("count: requires 1 argument")
				}

				switch c := args[0].(type) {
				case core.List:
					return core.Int{V: int64(c.Len())}, nil
				case core.Vector:
					return core.Int{V: int64(c.Len())}, nil
				case *core.HashMap:
					return core.Int{V: int64(c.Len())}, nil
				case core.String:
					return core.Int{V: int64(len([]rune(c.V)))}, nil
				case core.Nil:
					return core.Int{V: 0}, nil
				default:
					return nil, typeErrorf("count: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "cons",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 2 {
					return nil, arityErrorf("cons: requires 2 arguments")
				}

				base := args[1]
				if _, isNil := base.(core.Nil); isNil {
					base = core.NewList(nil)
				}
				switch c := base.(type) {
				case core.List:
					res, bytes := c.Cons(args[0])
					if err := chargeConsResult(ctx, eval, "cons", res, bytes, args[0]); err != nil {
						return nil, err
					}
					return res, nil
				case core.Vector:
					budget := core.NewBuiltinWorkBudget(ctx)
					items := make([]core.Value, c.Len()+1)
					items[0] = args[0]
					for i, v := range c.ToSlice() {
						if err := budget.Step(); err != nil {
							return finishBuiltin(budget, nil, err)
						}
						items[i+1] = v
					}
					res := core.NewList(items)
					if err := chargeConsResult(ctx, eval, "cons", res, core.ListShallowBytes(len(items)), args[0]); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					return finishBuiltin(budget, res, nil)
				default:
					return nil, typeErrorf("cons: expected collection, got %T", args[1])
				}
			},
		},

		{
			Name: "conj",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 2 {
					return nil, arityErrorf("conj: requires at least 2 arguments")
				}

				base := args[0]
				if _, isNil := base.(core.Nil); isNil {
					base = core.NewList(nil)
				}
				switch c := base.(type) {
				case core.List:
					// conj prepends args[1:] onto c in order — equivalent to
					// consing them on one at a time starting from the last, so
					// c's own backing is extended, never copied.
					res := c
					var bytes int64
					for i := len(args) - 1; i >= 1; i-- {
						var b int64
						res, b = res.Cons(args[i])
						bytes += b
					}
					if err := chargeConsResult(ctx, eval, "conj", res, bytes, args[1:]...); err != nil {
						return nil, err
					}
					return res, nil
				case core.Vector:
					budget := core.NewBuiltinWorkBudget(ctx)
					for range args[1:] {
						if err := budget.Step(); err != nil {
							return finishBuiltin(budget, nil, err)
						}
					}
					res, bytes := c.Conj(args[1:]...)
					if err := chargeConsResult(ctx, eval, "conj", res, bytes, args[1:]...); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					return finishBuiltin(budget, res, nil)
				case *core.HashMap:
					if len(args) != 3 {
						return nil, arityErrorf("conj on map requires key and value")
					}
					res, allocated, err := c.Assoc(args[1], args[2])
					if err != nil {
						return nil, wrapCause("conj", err)
					}
					// See assoc's WHY comment below: charge the path this call
					// copied plus the inserted value, not the shared remainder.
					bytes := allocated + core.ValueDeepBytes(args[2])
					if err := chargeConsResult(ctx, eval, "conj", res, bytes, args[2]); err != nil {
						return nil, err
					}
					return res, nil
				default:
					return nil, typeErrorf("conj: expected collection, got %T", args[0])
				}
			},
		},

		{
			Name: "empty?",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("empty?: requires 1 argument")
				}

				switch c := args[0].(type) {
				case core.List:
					return core.Bool{V: c.Len() == 0}, nil
				case core.Vector:
					return core.Bool{V: c.Len() == 0}, nil
				case *core.HashMap:
					return core.Bool{V: c.Len() == 0}, nil
				case core.Nil:
					return core.Bool{V: true}, nil
				default:
					return core.Bool{V: false}, nil
				}
			},
		},

		{
			Name: "get",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 2 || len(args) > 3 {
					return nil, lookupArityError("get", len(args))
				}

				result := core.Value(core.Nil{})
				if len(args) == 3 {
					result = args[2]
				}

				switch m := args[0].(type) {
				case *core.HashMap:
					if v, found := m.Get(args[1]); found {
						result = v
					}
				case core.Nil:
				default:
					return nil, lookupTypeError("get", "map", args[0])
				}

				if err := chargeBorrowedResult(ctx); err != nil {
					return nil, err
				}
				return result, nil
			},
		},

		{
			Name: "get-in",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				return getInLookup(ctx, args)
			},
		},

		{
			Name: "assoc",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 3 || len(args)%2 == 0 {
					return nil, arityErrorf("assoc: requires odd number of arguments (map + keyvals)")
				}

				m, ok := args[0].(*core.HashMap)
				if !ok {
					return nil, typeErrorf("assoc: expected map, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				result := m
				var bytes int64
				for i := 1; i < len(args); i += 2 {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					var err error
					var allocated int64
					result, allocated, err = result.Assoc(args[i], args[i+1])
					if err != nil {
						return finishBuiltin(budget, nil, wrapCause("assoc", err))
					}
					// Only what this call allocated: the copied path, plus the
					// inserted value, which is new to the ledger. The rest of the
					// map is shared with the argument and was charged when it was
					// created — re-charging it would grow with the accumulated
					// size and make a chained assoc quadratic.
					bytes += allocated + core.ValueDeepBytes(args[i+1])
				}

				// Guarded like conj's: prepending a scalar cannot deepen the
				// map, and CheckConstructionDepth walks (and sorts) every
				// entry, which would be O(n log n) on every call.
				inserted := make([]core.Value, 0, len(args)/2)
				for i := 2; i < len(args); i += 2 {
					inserted = append(inserted, args[i])
				}
				if err := chargeConsResult(ctx, eval, "assoc", result, bytes, inserted...); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, result, nil)
			},
		},

		{
			Name: "keys",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("keys: requires 1 argument")
				}

				m, ok := args[0].(*core.HashMap)
				if !ok {
					return nil, typeErrorf("keys: expected map, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				items := make([]core.Value, 0, m.Len())
				// Each cannot be stopped part way, so the budget error latches and
				// the remaining entries fall through untouched.
				var stepErr error
				m.Each(func(k, v core.Value) {
					if stepErr != nil {
						return
					}
					if stepErr = budget.Step(); stepErr != nil {
						return
					}
					items = append(items, k)
				})
				if stepErr != nil {
					return finishBuiltin(budget, nil, stepErr)
				}

				// Every key is borrowed from the map; only the list holding them is
				// new.
				if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(items))); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, core.NewList(items), nil)
			},
		},

		{
			Name: "vals",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("vals: requires 1 argument")
				}

				m, ok := args[0].(*core.HashMap)
				if !ok {
					return nil, typeErrorf("vals: expected map, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				items := make([]core.Value, 0, m.Len())
				var stepErr error
				m.Each(func(k, v core.Value) {
					if stepErr != nil {
						return
					}
					if stepErr = budget.Step(); stepErr != nil {
						return
					}
					items = append(items, v)
				})
				if stepErr != nil {
					return finishBuiltin(budget, nil, stepErr)
				}

				if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(items))); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, core.NewList(items), nil)
			},
		},

		{
			Name: "contains?",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 2 {
					return nil, arityErrorf("contains?: requires 2 arguments")
				}

				m, ok := args[0].(*core.HashMap)
				if !ok {
					return nil, typeErrorf("contains?: expected map, got %T", args[0])
				}

				_, found := m.Get(args[1])
				return core.Bool{V: found}, nil
			},
		},

		{
			Name: "merge",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				budget := core.NewBuiltinWorkBudget(ctx)
				result := core.NewHashMap()
				for _, arg := range args {
					switch m := arg.(type) {
					case *core.HashMap:
						// Each cannot be stopped part way, so both failures latch
						// and the remaining entries fall through untouched. They
						// stay apart because a budget error must reach the caller
						// as itself: wrapping it would hide the cancellation or
						// deadline that produced it.
						var stepErr, setErr error
						m.Each(func(k, v core.Value) {
							if stepErr != nil || setErr != nil {
								return
							}
							if stepErr = budget.Step(); stepErr != nil {
								return
							}
							setErr = result.Set(k, v)
						})
						if stepErr != nil {
							return finishBuiltin(budget, nil, stepErr)
						}
						if setErr != nil {
							return finishBuiltin(budget, nil, wrapCause("merge", setErr))
						}
					case core.Nil:
					default:
						return finishBuiltin(budget, nil, typeErrorf("merge: expected map, got %T", arg))
					}
				}
				if err := chargeCollectionResult(ctx, eval, "merge", result, core.ValueDeepBytes(result)); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, result, nil)
			},
		},

		{
			Name: "dissoc",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 1 {
					return nil, arityErrorf("dissoc: requires at least 1 argument")
				}

				m, ok := args[0].(*core.HashMap)
				if !ok {
					return nil, typeErrorf("dissoc: expected map, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				result := m
				var bytes int64
				for _, k := range args[1:] {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					var err error
					var allocated int64
					result, allocated, err = result.Dissoc(k)
					if err != nil {
						return finishBuiltin(budget, nil, wrapCause("dissoc", err))
					}
					bytes += allocated
				}

				// No newElems: removing a key cannot deepen the map, so the
				// construction-depth walk is skipped entirely.
				if err := chargeConsResult(ctx, eval, "dissoc", result, bytes); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, result, nil)
			},
		},

		{
			Name: "sort",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) != 1 {
					return nil, arityErrorf("sort: requires 1 argument")
				}

				var subject []core.Value
				switch c := args[0].(type) {
				case core.List:
					subject = c.ToSlice()
				case core.Vector:
					subject = c.ToSlice()
				case core.Nil:
					if err := chargeFreshContainer(ctx, core.ListShallowBytes(0)); err != nil {
						return nil, err
					}
					return core.NewList([]core.Value{}), nil
				default:
					return nil, typeErrorf("sort: expected collection, got %T", args[0])
				}

				budget := core.NewBuiltinWorkBudget(ctx)
				// The ToSlice copy above happens before the kernel is entered, and
				// StableSort charges only the work it does itself, so this walk is
				// stdlib's to bill. Charging the kernel again here would double it.
				for range subject {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
				}

				sorted, err := collections.StableSort(ctx, subject, nil, func(a, b core.Value) (bool, error) {
					cmp, err := collections.NaturalCmp(a, b)
					if err != nil {
						return false, err
					}
					return cmp < 0, nil
				})
				if err != nil {
					return finishBuiltin(budget, nil, err)
				}

				// Every element is borrowed from the subject; only the list holding
				// them is new.
				if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(sorted))); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, core.NewList(sorted), nil)
			},
		},

		{
			Name: "range",
			Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
				if len(args) < 1 || len(args) > 3 {
					return nil, arityErrorf("range: requires 1 to 3 arguments")
				}

				bounds := make([]int64, len(args))
				for i, arg := range args {
					n, ok := arg.(core.Int)
					if !ok {
						return nil, typeErrorf("range: requires integer arguments, got %T", arg)
					}
					bounds[i] = n.V
				}

				var start, end, step int64 = 0, 0, 1
				switch len(bounds) {
				case 1:
					end = bounds[0]
				case 2:
					start, end = bounds[0], bounds[1]
				case 3:
					start, end, step = bounds[0], bounds[1], bounds[2]
					if step == 0 {
						return nil, domainErrorf("range: step must not be zero")
					}
				}

				var span, stepMag uint64
				if step > 0 {
					stepMag = uint64(step)
					if end > start {
						span = uint64(end) - uint64(start)
					}
				} else {
					stepMag = uint64(-step)
					if start > end {
						span = uint64(start) - uint64(end)
					}
				}
				count := span / stepMag
				if span%stepMag != 0 {
					count++
				}
				maxLen := collectionLimit(eval)
				if count > uint64(maxLen) {
					return nil, core.NewResourceLimitError(fmt.Sprintf("range length %d exceeds collection limit %d", count, maxLen))
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				budget := core.NewBuiltinWorkBudget(ctx)
				items := make([]core.Value, 0, count)
				cur := start
				for k := uint64(0); k < count; k++ {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					// The budget syncs once per batch, which is coarser than the
					// one-element granularity range's cancellation contract
					// requires, so the caller's own context is still probed here.
					if err := ctx.Err(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					items = append(items, core.Int{V: cur})
					if k+1 < count {
						cur += step
					}
				}
				res := core.NewList(items)
				if err := chargeCollectionResult(ctx, eval, "range", res, core.ValueDeepBytes(res)); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				return finishBuiltin(budget, res, nil)
			},
		},
	}
}

// appendCollectionElems appends arg's elements to *dst, one budget unit per
// element — List and Vector are read through ToSlice, a single O(n) walk,
// never an indexed At() loop (O(n) per call on a shared List, O(n^2) total
// across the loop).
func appendCollectionElems(budget *core.BuiltinWorkBudget, name string, dst *[]core.Value, arg core.Value) error {
	var elems []core.Value
	switch c := arg.(type) {
	case core.List:
		elems = c.ToSlice()
	case core.Vector:
		elems = c.ToSlice()
	case core.Nil:
		return nil
	default:
		return typeErrorf("%s: expected collection, got %T", name, arg)
	}
	for _, v := range elems {
		if err := budget.Step(); err != nil {
			return err
		}
		*dst = append(*dst, v)
	}
	return nil
}

// lookupArityError reports a lookup builtin called outside its 2-or-3
// argument window; NewArityError takes a single expected count and cannot
// express a range.
func lookupArityError(name string, got int) *core.LispicoError {
	return &core.LispicoError{Code: "ArityError", Message: fmt.Sprintf("%s: requires 2 or 3 arguments, got %d", name, got)}
}

// lookupTypeError reports a lookup builtin applied to an unsupported
// subject. A type error is not a miss: the caller must not fall through to
// the default.
func lookupTypeError(name, expected string, got core.Value) *core.LispicoError {
	return &core.LispicoError{Code: "TypeError", Message: fmt.Sprintf("%s: expected %s, got %T", name, expected, got)}
}

// getInLookup walks a key path through nested maps. A key found holding nil
// is a hit: that nil is carried on and only reads as missing once a further
// key has to be looked up inside it. An empty path is a hit on the subject
// itself, so the default is never consulted.
func getInLookup(ctx context.Context, args []core.Value) (core.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, lookupArityError("get-in", len(args))
	}

	path, ok := newKeyPathCursor(args[1])
	if !ok {
		return nil, lookupTypeError("get-in", "key path", args[1])
	}

	missing := core.Value(core.Nil{})
	if len(args) == 3 {
		missing = args[2]
	}

	budget := core.NewBuiltinWorkBudget(ctx)
	subject := args[0]
	for {
		key, more := path.next()
		if !more {
			return getInResult(ctx, budget, subject, nil)
		}

		switch m := subject.(type) {
		case *core.HashMap:
			if err := budget.Step(); err != nil {
				return getInResult(ctx, budget, nil, err)
			}
			v, found := m.Get(key)
			if !found {
				return getInResult(ctx, budget, missing, nil)
			}
			subject = v
		case core.Nil:
			return getInResult(ctx, budget, missing, nil)
		default:
			return getInResult(ctx, budget, nil, lookupTypeError("get-in", "map", subject))
		}
	}
}

// getInResult settles the budget before anything leaves the builtin: a
// terminal sync error outranks a lookup error, and only a value return
// reaches the result ledger.
func getInResult(ctx context.Context, budget *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {
	if ferr := budget.Flush(); ferr != nil && (err == nil || (core.IsTerminalEvalError(ferr) && !core.IsTerminalEvalError(err))) {
		return nil, ferr
	}
	if err != nil {
		return nil, err
	}
	if cerr := chargeBorrowedResult(ctx); cerr != nil {
		return nil, cerr
	}
	return v, nil
}

// keyPathCursor hands out path keys one at a time without materializing the
// path. A List advances by Rest, O(1) per key on the shared representation
// where a repeated At would be quadratic.
type keyPathCursor struct {
	list  core.List
	vec   core.Vector
	isVec bool
	i, n  int
}

func newKeyPathCursor(path core.Value) (keyPathCursor, bool) {
	switch p := path.(type) {
	case core.List:
		return keyPathCursor{list: p, n: p.Len()}, true
	case core.Vector:
		return keyPathCursor{vec: p, isVec: true, n: p.Len()}, true
	case core.Nil:
		return keyPathCursor{}, true
	default:
		return keyPathCursor{}, false
	}
}

// next bounds-checks before every At: both At implementations panic out of
// range.
func (c *keyPathCursor) next() (core.Value, bool) {
	if c.i >= c.n {
		return nil, false
	}
	c.i++
	if c.isVec {
		return c.vec.At(c.i - 1), true
	}
	key := c.list.At(0)
	c.list = c.list.Rest()
	return key, true
}

// chargeCollectionResult validates res against the collection-length and
// structural-depth limits, then charges bytes to the allocation ledger.
// Callers decide what bytes means: the full deep size for a fresh builder
// assembling unrelated values, or just what an operation newly allocated
// when it derives its result from an existing collection.
func chargeCollectionResult(ctx context.Context, eval core.Evaluator, name string, res core.Value, bytes int64) error {
	if n, ok := collectionLen(res); ok {
		maxLen := collectionLimit(eval)
		if n > maxLen {
			return core.NewResourceLimitError(fmt.Sprintf("%s length %d exceeds collection limit %d", name, n, maxLen))
		}
	}
	if err := core.CheckConstructionDepthWith(res, eval); err != nil {
		return err
	}
	return core.ChargeGoFuncResultBytes(ctx, bytes)
}

// isNestedCollection reports whether v could itself introduce structural
// depth if consed or conj'd into a collection — List, Vector, *HashMap,
// Lambda, and Macro all recurse in CheckConstructionDepth; every other
// type does not.
func isNestedCollection(v core.Value) bool {
	switch v.(type) {
	case core.List, core.Vector, *core.HashMap, core.Lambda, core.Macro:
		return true
	default:
		return false
	}
}

// chargeConsResult validates and charges a cons/conj result. Unlike
// chargeCollectionResult, it narrows the structural-depth walk: prepending
// a scalar can't increase a list's nesting, so it only pays
// CheckConstructionDepth's O(n) walk when at least one of the newly
// introduced elements could itself nest.
func chargeConsResult(ctx context.Context, eval core.Evaluator, name string, res core.Value, bytes int64, newElems ...core.Value) error {
	if n, ok := collectionLen(res); ok {
		maxLen := collectionLimit(eval)
		if n > maxLen {
			return core.NewResourceLimitError(fmt.Sprintf("%s length %d exceeds collection limit %d", name, n, maxLen))
		}
	}
	for _, e := range newElems {
		if isNestedCollection(e) {
			// Only the new element can deepen the result: the collection
			// being extended was depth-checked when it was built, so
			// re-walking it here would cost the accumulated size on every
			// call and make a loop that conses collections quadratic.
			if err := core.CheckNestedElementDepthWith(e, eval); err != nil {
				return err
			}
		}
	}
	return core.ChargeGoFuncResultBytes(ctx, bytes)
}

func seqInput(v core.Value) ([]core.Value, bool) {
	switch c := v.(type) {
	case core.List:
		return c.ToSlice(), true
	case core.Vector:
		return c.ToSlice(), true
	case core.Nil:
		return nil, true
	default:
		return nil, false
	}
}

func collectionLen(v core.Value) (int, bool) {
	switch c := v.(type) {
	case core.List:
		return c.Len(), true
	case core.Vector:
		return c.Len(), true
	case *core.HashMap:
		return c.Len(), true
	default:
		return 0, false
	}
}

func collectionLimit(eval core.Evaluator) int {
	if cl, ok := eval.(core.CollectionLimiter); ok {
		if n := cl.CollectionLimit(); n > 0 {
			return n
		}
	}
	return defaultStdlibCollectionLen
}

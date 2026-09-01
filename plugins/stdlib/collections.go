package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/collections"
)

const defaultStdlibCollectionLen = 10_000_000

func (p *Plugin) registerCollections(env *core.Env) error {
	if err := env.RegisterValue("list", core.GoFunc{
		Name: "list",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			res := core.NewList(append([]core.Value(nil), args...))
			if err := chargeCollectionResult(ctx, env, "list", res, core.ValueDeepBytes(res)); err != nil {
				return nil, err
			}
			return res, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("concat", core.GoFunc{
		Name: "concat",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return core.NewList(nil), nil
			}

			// The last argument extends without copying when it's a List:
			// every earlier argument's elements get Cons'd onto it in
			// reverse, so baseList's own backing — however long — is never
			// walked or duplicated. A Vector or Nil last argument falls
			// back to a full flatten, same as before.
			baseList, sharesLast := args[len(args)-1].(core.List)
			if !sharesLast {
				var result []core.Value
				for _, arg := range args {
					if err := appendCollectionElems(&result, arg); err != nil {
						return nil, fmt.Errorf("concat: %w", err)
					}
				}
				res := core.NewList(result)
				if err := chargeCollectionResult(ctx, env, "concat", res, core.ListShallowBytes(len(result))); err != nil {
					return nil, err
				}
				return res, nil
			}

			var prefix []core.Value
			for _, arg := range args[:len(args)-1] {
				if err := appendCollectionElems(&prefix, arg); err != nil {
					return nil, fmt.Errorf("concat: %w", err)
				}
			}
			res := baseList
			var bytes int64
			for i := len(prefix) - 1; i >= 0; i-- {
				var b int64
				res, b = res.Cons(prefix[i])
				bytes += b
			}
			if err := chargeCollectionResult(ctx, env, "concat", res, bytes); err != nil {
				return nil, err
			}
			return res, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("reverse", core.GoFunc{
		Name: "reverse",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("reverse: requires 1 argument")
			}

			var items []core.Value
			switch c := args[0].(type) {
			case core.List:
				items = c.ToSlice()
			case core.Vector:
				items = c.ToSlice()
			default:
				return nil, fmt.Errorf("reverse: expected collection, got %T", args[0])
			}
			result := make([]core.Value, len(items))
			for i, v := range items {
				result[len(items)-1-i] = v
			}
			return core.NewList(result), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("vector", core.GoFunc{
		Name: "vector",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			res := core.NewVector(append([]core.Value(nil), args...))
			if err := chargeCollectionResult(ctx, env, "vector", res, core.ValueDeepBytes(res)); err != nil {
				return nil, err
			}
			return res, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("hash-map", core.GoFunc{
		Name: "hash-map",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args)%2 != 0 {
				return nil, fmt.Errorf("hash-map: requires even number of arguments")
			}

			m := core.NewHashMap()
			for i := 0; i < len(args); i += 2 {
				if err := m.Set(args[i], args[i+1]); err != nil {
					return nil, fmt.Errorf("hash-map: %w", err)
				}
			}
			if err := core.CheckConstructionDepth(m, env); err != nil {
				return nil, err
			}
			return m, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("first", core.GoFunc{
		Name: "first",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("first: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				if c.Len() == 0 {
					return core.Nil{}, nil
				}
				return c.At(0), nil
			case core.Vector:
				if c.Len() == 0 {
					return core.Nil{}, nil
				}
				return c.At(0), nil
			case core.Nil:
				return core.Nil{}, nil
			default:
				return nil, fmt.Errorf("first: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("rest", core.GoFunc{
		Name: "rest",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("rest: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				return c.Rest(), nil
			case core.Vector:
				if c.Len() <= 1 {
					return core.NewList([]core.Value{}), nil
				}
				items := make([]core.Value, c.Len()-1)
				for i := 1; i < c.Len(); i++ {
					items[i-1] = c.At(i)
				}
				return core.NewList(items), nil
			case core.Nil:
				return core.NewList([]core.Value{}), nil
			default:
				return nil, fmt.Errorf("rest: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("last", core.GoFunc{
		Name: "last",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("last: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				if c.Len() == 0 {
					return core.Nil{}, nil
				}
				return c.At(c.Len() - 1), nil
			case core.Vector:
				if c.Len() == 0 {
					return core.Nil{}, nil
				}
				return c.At(c.Len() - 1), nil
			case core.Nil:
				return core.Nil{}, nil
			default:
				return nil, fmt.Errorf("last: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("nth", core.GoFunc{
		Name: "nth",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("nth: requires 2 or 3 arguments")
			}

			idx, ok := args[1].(core.Int)
			if !ok {
				return nil, fmt.Errorf("nth: index must be integer")
			}

			val, outcome, err := collections.IndexedAccess(ctx, args[0], idx.V)
			if err != nil {
				return nil, err
			}
			switch outcome {
			case collections.AccessHit:
				return val, nil
			case collections.AccessOutOfRange:
				if len(args) == 3 {
					return args[2], nil
				}
				return nil, fmt.Errorf("nth: index out of bounds")
			default:
				return nil, fmt.Errorf("nth: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("count", core.GoFunc{
		Name: "count",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("count: requires 1 argument")
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
				return nil, fmt.Errorf("count: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("cons", core.GoFunc{
		Name: "cons",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("cons: requires 2 arguments")
			}

			switch c := args[1].(type) {
			case core.List:
				res, bytes := c.Cons(args[0])
				if err := chargeConsResult(ctx, env, "cons", res, bytes, args[0]); err != nil {
					return nil, err
				}
				return res, nil
			case core.Vector:
				items := make([]core.Value, c.Len()+1)
				items[0] = args[0]
				copy(items[1:], c.ToSlice())
				res := core.NewList(items)
				if err := chargeConsResult(ctx, env, "cons", res, core.ListShallowBytes(len(items)), args[0]); err != nil {
					return nil, err
				}
				return res, nil
			default:
				return nil, fmt.Errorf("cons: expected collection, got %T", args[1])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("conj", core.GoFunc{
		Name: "conj",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("conj: requires at least 2 arguments")
			}

			switch c := args[0].(type) {
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
				if err := chargeConsResult(ctx, env, "conj", res, bytes, args[1:]...); err != nil {
					return nil, err
				}
				return res, nil
			case core.Vector:
				res, bytes := c.Conj(args[1:]...)
				if err := chargeConsResult(ctx, env, "conj", res, bytes, args[1:]...); err != nil {
					return nil, err
				}
				return res, nil
			case *core.HashMap:
				if len(args) != 3 {
					return nil, fmt.Errorf("conj on map requires key and value")
				}
				res, allocated, err := c.Assoc(args[1], args[2])
				if err != nil {
					return nil, err
				}
				// See assoc's WHY comment below: charge the path this call
				// copied plus the inserted value, not the shared remainder.
				bytes := allocated + core.ValueDeepBytes(args[2])
				if err := chargeConsResult(ctx, env, "conj", res, bytes, args[2]); err != nil {
					return nil, err
				}
				return res, nil
			default:
				return nil, fmt.Errorf("conj: expected collection, got %T", args[0])
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("empty?", core.GoFunc{
		Name: "empty?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("empty?: requires 1 argument")
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("get", core.GoFunc{
		Name: "get",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("get: requires 2 or 3 arguments")
			}

			switch m := args[0].(type) {
			case *core.HashMap:
				if v, found := m.Get(args[1]); found {
					return v, nil
				}
			case core.Nil:
			default:
				return nil, fmt.Errorf("get: expected map, got %T", args[0])
			}

			if len(args) == 3 {
				return args[2], nil
			}

			return core.Nil{}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("assoc", core.GoFunc{
		Name: "assoc",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 3 || len(args)%2 == 0 {
				return nil, fmt.Errorf("assoc: requires odd number of arguments (map + keyvals)")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("assoc: expected map, got %T", args[0])
			}

			result := m
			var bytes int64
			for i := 1; i < len(args); i += 2 {
				var err error
				var allocated int64
				result, allocated, err = result.Assoc(args[i], args[i+1])
				if err != nil {
					return nil, fmt.Errorf("assoc: %w", err)
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
			if err := chargeConsResult(ctx, env, "assoc", result, bytes, inserted...); err != nil {
				return nil, err
			}
			return result, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("keys", core.GoFunc{
		Name: "keys",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("keys: requires 1 argument")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("keys: expected map, got %T", args[0])
			}

			items := make([]core.Value, 0, m.Len())
			m.Each(func(k, v core.Value) {
				items = append(items, k)
			})

			return core.NewList(items), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("vals", core.GoFunc{
		Name: "vals",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("vals: requires 1 argument")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("vals: expected map, got %T", args[0])
			}

			items := make([]core.Value, 0, m.Len())
			m.Each(func(k, v core.Value) {
				items = append(items, v)
			})

			return core.NewList(items), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("contains?", core.GoFunc{
		Name: "contains?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("contains?: requires 2 arguments")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("contains?: expected map, got %T", args[0])
			}

			_, found := m.Get(args[1])
			return core.Bool{V: found}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("merge", core.GoFunc{
		Name: "merge",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			result := core.NewHashMap()
			for _, arg := range args {
				switch m := arg.(type) {
				case *core.HashMap:
					var err error
					m.Each(func(k, v core.Value) {
						if err == nil {
							err = result.Set(k, v)
						}
					})
					if err != nil {
						return nil, fmt.Errorf("merge: %w", err)
					}
				case core.Nil:
				default:
					return nil, fmt.Errorf("merge: expected map, got %T", arg)
				}
			}
			if err := chargeCollectionResult(ctx, env, "merge", result, core.ValueDeepBytes(result)); err != nil {
				return nil, err
			}
			return result, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("dissoc", core.GoFunc{
		Name: "dissoc",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("dissoc: requires at least 1 argument")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("dissoc: expected map, got %T", args[0])
			}

			result := m
			var bytes int64
			for _, k := range args[1:] {
				var err error
				var allocated int64
				result, allocated, err = result.Dissoc(k)
				if err != nil {
					return nil, fmt.Errorf("dissoc: %w", err)
				}
				bytes += allocated
			}

			// No newElems: removing a key cannot deepen the map, so the
			// construction-depth walk is skipped entirely.
			if err := chargeConsResult(ctx, env, "dissoc", result, bytes); err != nil {
				return nil, err
			}
			return result, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("sort", core.GoFunc{
		Name: "sort",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("sort: requires 1 argument")
			}

			var sorted []core.Value
			switch c := args[0].(type) {
			case core.List:
				sorted = c.ToSlice()
			case core.Vector:
				sorted = c.ToSlice()
			case core.Nil:
				return core.NewList([]core.Value{}), nil
			default:
				return nil, fmt.Errorf("sort: expected collection, got %T", args[0])
			}

			sorted, err := collections.StableSort(ctx, sorted, nil, func(a, b core.Value) (bool, error) {
				cmp, err := collections.NaturalCmp(a, b)
				if err != nil {
					return false, err
				}
				return cmp < 0, nil
			})
			if err != nil {
				return nil, err
			}

			return core.NewList(sorted), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("range", core.GoFunc{
		Name: "range",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 || len(args) > 3 {
				return nil, fmt.Errorf("range: requires 1 to 3 arguments")
			}

			bounds := make([]int64, len(args))
			for i, arg := range args {
				n, ok := arg.(core.Int)
				if !ok {
					return nil, fmt.Errorf("range: requires integer arguments, got %T", arg)
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
					return nil, fmt.Errorf("range: step must not be zero")
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
			maxLen := collectionLimit(env)
			if count > uint64(maxLen) {
				return nil, core.NewResourceLimitError(fmt.Sprintf("range length %d exceeds collection limit %d", count, maxLen))
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			items := make([]core.Value, 0, count)
			cur := start
			for k := uint64(0); k < count; k++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				items = append(items, core.Int{V: cur})
				if k+1 < count {
					cur += step
				}
			}
			res := core.NewList(items)
			if err := chargeCollectionResult(ctx, env, "range", res, core.ValueDeepBytes(res)); err != nil {
				return nil, err
			}
			return res, nil
		},
	}, false); err != nil {
		return err
	}
	return nil
}

// appendCollectionElems appends arg's elements to *dst — List and Vector via
// ToSlice, a single O(n) walk, never an indexed At() loop (O(n) per call on
// a shared List, O(n^2) total across the loop).
func appendCollectionElems(dst *[]core.Value, arg core.Value) error {
	switch c := arg.(type) {
	case core.List:
		*dst = append(*dst, c.ToSlice()...)
	case core.Vector:
		*dst = append(*dst, c.ToSlice()...)
	case core.Nil:
	default:
		return fmt.Errorf("expected collection, got %T", arg)
	}
	return nil
}

// chargeCollectionResult validates res against the collection-length and
// structural-depth limits, then charges bytes to the allocation ledger.
// Callers decide what bytes means: the full deep size for a fresh builder
// assembling unrelated values, or just what an operation newly allocated
// when it derives its result from an existing collection.
func chargeCollectionResult(ctx context.Context, env *core.Env, name string, res core.Value, bytes int64) error {
	if n, ok := collectionLen(res); ok {
		maxLen := collectionLimit(env)
		if n > maxLen {
			return core.NewResourceLimitError(fmt.Sprintf("%s length %d exceeds collection limit %d", name, n, maxLen))
		}
	}
	if err := core.CheckConstructionDepth(res, env); err != nil {
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
func chargeConsResult(ctx context.Context, env *core.Env, name string, res core.Value, bytes int64, newElems ...core.Value) error {
	if n, ok := collectionLen(res); ok {
		maxLen := collectionLimit(env)
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
			if err := core.CheckNestedElementDepth(e, env); err != nil {
				return err
			}
		}
	}
	return core.ChargeGoFuncResultBytes(ctx, bytes)
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

func collectionLimit(env *core.Env) int {
	if env == nil {
		return defaultStdlibCollectionLen
	}
	if ev := env.Evaluator(); ev != nil {
		if cl, ok := ev.(core.CollectionLimiter); ok {
			if n := cl.CollectionLimit(); n > 0 {
				return n
			}
		}
	}
	return defaultStdlibCollectionLen
}

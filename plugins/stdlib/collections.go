package stdlib

import (
	"context"
	"fmt"
	"sort"

	"github.com/victorzhuk/go-lispico/core"
)

const defaultStdlibCollectionLen = 10_000_000

func (p *Plugin) registerCollections(env *core.Env) {
	maxCollectionLen := defaultStdlibCollectionLen
	if ev := env.Evaluator(); ev != nil {
		if cl, ok := ev.(core.CollectionLimiter); ok {
			if n := cl.CollectionLimit(); n > 0 {
				maxCollectionLen = n
			}
		}
	}
	env.Set("list", core.GoFunc{
		Name: "list",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return core.List{Items: args}, nil
		},
	})

	env.Set("concat", core.GoFunc{
		Name: "concat",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			var result []core.Value
			for _, arg := range args {
				switch c := arg.(type) {
				case core.List:
					result = append(result, c.Items...)
				case core.Vector:
					result = append(result, c.Items...)
				case core.Nil:
				default:
					return nil, fmt.Errorf("concat: expected collection, got %T", arg)
				}
			}
			return core.List{Items: result}, nil
		},
	})

	env.Set("reverse", core.GoFunc{
		Name: "reverse",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("reverse: requires 1 argument")
			}

			var items []core.Value
			switch c := args[0].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			default:
				return nil, fmt.Errorf("reverse: expected collection, got %T", args[0])
			}

			result := make([]core.Value, len(items))
			for i, item := range items {
				result[len(items)-1-i] = item
			}
			return core.List{Items: result}, nil
		},
	})

	env.Set("vector", core.GoFunc{
		Name: "vector",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return core.Vector{Items: args}, nil
		},
	})

	env.Set("hash-map", core.GoFunc{
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
			return m, nil
		},
	})

	env.Set("first", core.GoFunc{
		Name: "first",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("first: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				if len(c.Items) == 0 {
					return core.Nil{}, nil
				}
				return c.Items[0], nil
			case core.Vector:
				if len(c.Items) == 0 {
					return core.Nil{}, nil
				}
				return c.Items[0], nil
			case core.Nil:
				return core.Nil{}, nil
			default:
				return nil, fmt.Errorf("first: expected collection, got %T", args[0])
			}
		},
	})

	env.Set("rest", core.GoFunc{
		Name: "rest",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("rest: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				if len(c.Items) <= 1 {
					return core.List{Items: []core.Value{}}, nil
				}
				return core.List{Items: c.Items[1:]}, nil
			case core.Vector:
				if len(c.Items) <= 1 {
					return core.List{Items: []core.Value{}}, nil
				}
				return core.List{Items: c.Items[1:]}, nil
			case core.Nil:
				return core.List{Items: []core.Value{}}, nil
			default:
				return nil, fmt.Errorf("rest: expected collection, got %T", args[0])
			}
		},
	})

	env.Set("last", core.GoFunc{
		Name: "last",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("last: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				if len(c.Items) == 0 {
					return core.Nil{}, nil
				}
				return c.Items[len(c.Items)-1], nil
			case core.Vector:
				if len(c.Items) == 0 {
					return core.Nil{}, nil
				}
				return c.Items[len(c.Items)-1], nil
			case core.Nil:
				return core.Nil{}, nil
			default:
				return nil, fmt.Errorf("last: expected collection, got %T", args[0])
			}
		},
	})

	env.Set("nth", core.GoFunc{
		Name: "nth",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("nth: requires 2 or 3 arguments")
			}

			idx, ok := args[1].(core.Int)
			if !ok {
				return nil, fmt.Errorf("nth: index must be integer")
			}

			var items []core.Value
			switch c := args[0].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			default:
				return nil, fmt.Errorf("nth: expected collection, got %T", args[0])
			}

			if idx.V < 0 || int(idx.V) >= len(items) {
				if len(args) == 3 {
					return args[2], nil
				}
				return nil, fmt.Errorf("nth: index out of bounds")
			}

			return items[idx.V], nil
		},
	})

	env.Set("count", core.GoFunc{
		Name: "count",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("count: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				return core.Int{V: int64(len(c.Items))}, nil
			case core.Vector:
				return core.Int{V: int64(len(c.Items))}, nil
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
	})

	env.Set("cons", core.GoFunc{
		Name: "cons",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("cons: requires 2 arguments")
			}

			switch c := args[1].(type) {
			case core.List:
				items := append([]core.Value{args[0]}, c.Items...)
				return core.List{Items: items}, nil
			case core.Vector:
				items := append([]core.Value{args[0]}, c.Items...)
				return core.List{Items: items}, nil
			default:
				return nil, fmt.Errorf("cons: expected collection, got %T", args[1])
			}
		},
	})

	env.Set("conj", core.GoFunc{
		Name: "conj",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("conj: requires at least 2 arguments")
			}

			switch c := args[0].(type) {
			case core.List:
				items := make([]core.Value, len(args)-1)
				copy(items, args[1:])
				items = append(items, c.Items...)
				return core.List{Items: items}, nil
			case core.Vector:
				items := make([]core.Value, len(c.Items)+len(args)-1)
				copy(items, c.Items)
				copy(items[len(c.Items):], args[1:])
				return core.Vector{Items: items}, nil
			case *core.HashMap:
				if len(args) != 3 {
					return nil, fmt.Errorf("conj on map requires key and value")
				}
				return c.Assoc(args[1], args[2])
			default:
				return nil, fmt.Errorf("conj: expected collection, got %T", args[0])
			}
		},
	})

	env.Set("empty?", core.GoFunc{
		Name: "empty?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("empty?: requires 1 argument")
			}

			switch c := args[0].(type) {
			case core.List:
				return core.Bool{V: len(c.Items) == 0}, nil
			case core.Vector:
				return core.Bool{V: len(c.Items) == 0}, nil
			case *core.HashMap:
				return core.Bool{V: c.Len() == 0}, nil
			case core.Nil:
				return core.Bool{V: true}, nil
			default:
				return core.Bool{V: false}, nil
			}
		},
	})

	env.Set("get", core.GoFunc{
		Name: "get",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("get: requires 2 or 3 arguments")
			}

			m, ok := args[0].(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("get: expected map, got %T", args[0])
			}

			if v, found := m.Get(args[1]); found {
				return v, nil
			}

			if len(args) == 3 {
				return args[2], nil
			}

			return core.Nil{}, nil
		},
	})

	env.Set("assoc", core.GoFunc{
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
			for i := 1; i < len(args); i += 2 {
				var err error
				result, err = result.Assoc(args[i], args[i+1])
				if err != nil {
					return nil, fmt.Errorf("assoc: %w", err)
				}
			}

			return result, nil
		},
	})

	env.Set("keys", core.GoFunc{
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

			return core.List{Items: items}, nil
		},
	})

	env.Set("vals", core.GoFunc{
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

			return core.List{Items: items}, nil
		},
	})

	env.Set("contains?", core.GoFunc{
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
	})

	env.Set("merge", core.GoFunc{
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
			return result, nil
		},
	})

	env.Set("dissoc", core.GoFunc{
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
			for _, k := range args[1:] {
				var err error
				result, err = result.Dissoc(k)
				if err != nil {
					return nil, fmt.Errorf("dissoc: %w", err)
				}
			}
			return result, nil
		},
	})

	env.Set("sort", core.GoFunc{
		Name: "sort",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("sort: requires 1 argument")
			}

			var items []core.Value
			switch c := args[0].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			case core.Nil:
				return core.List{Items: []core.Value{}}, nil
			default:
				return nil, fmt.Errorf("sort: expected collection, got %T", args[0])
			}

			sorted := make([]core.Value, len(items))
			copy(sorted, items)

			var sortErr error
			sort.SliceStable(sorted, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				cmp, err := naturalCmp(sorted[i], sorted[j])
				if err != nil {
					sortErr = err
					return false
				}
				return cmp < 0
			})
			if sortErr != nil {
				return nil, sortErr
			}

			return core.List{Items: sorted}, nil
		},
	})

	env.Set("range", core.GoFunc{
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
			maxLen := maxCollectionLen
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
			return core.List{Items: items}, nil
		},
	})
}

// naturalCmp orders two values of the same kind: numbers by numCmp, strings
// and keywords lexicographically. Mixed kinds (beyond int/float) are an error.
func naturalCmp(a, b core.Value) (int, error) {
	if as, ok := a.(core.String); ok {
		bs, ok := b.(core.String)
		if !ok {
			return 0, fmt.Errorf("sort: cannot compare %T with %T", a, b)
		}
		switch {
		case as.V < bs.V:
			return -1, nil
		case as.V > bs.V:
			return 1, nil
		}
		return 0, nil
	}
	if ak, ok := a.(core.Keyword); ok {
		bk, ok := b.(core.Keyword)
		if !ok {
			return 0, fmt.Errorf("sort: cannot compare %T with %T", a, b)
		}
		switch {
		case ak.V < bk.V:
			return -1, nil
		case ak.V > bk.V:
			return 1, nil
		}
		return 0, nil
	}
	return numCmp("sort", a, b)
}

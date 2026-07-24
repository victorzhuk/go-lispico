package core

import (
	"fmt"
	"strings"
)

const (
	DefaultMaxStructuralDepth = 1024
	MaxCompileDepth           = 1024
)

type ConstructionDepthEvaluator interface {
	ConstructionDepthLimit() int
}

func CheckConstructionDepth(v Value, env *Env) error {
	limit := DefaultMaxStructuralDepth
	if env != nil {
		if ev := env.Evaluator(); ev != nil {
			if de, ok := ev.(ConstructionDepthEvaluator); ok {
				if n := de.ConstructionDepthLimit(); n > 0 {
					limit = n
				}
			}
		}
	}
	if limit <= 0 {
		return nil
	}
	if constructionDepthExceeded(v, 0, limit) {
		return &LispicoError{Code: CodeResourceLimit, Message: fmt.Sprintf("structural depth limit %d exceeded", limit)}
	}
	return nil
}

// ValueDepthExceeds reports whether v's collection nesting exceeds limit.
// The scan descends at most limit+1 collection levels, so it cannot overflow
// the Go stack on a pathological value.
func ValueDepthExceeds(v Value, limit int) bool {
	return constructionDepthExceeded(v, 0, limit)
}

func constructionDepthExceeded(v Value, depth, limit int) bool {
	switch val := v.(type) {
	case List:
		depth++
		if depth > limit {
			return true
		}
		for _, item := range val.Items {
			switch item.(type) {
			case List, Vector, *HashMap, Lambda, Macro:
				if constructionDepthExceeded(item, depth, limit) {
					return true
				}
			}
		}
	case Vector:
		depth++
		if depth > limit {
			return true
		}
		for _, item := range val.Items {
			switch item.(type) {
			case List, Vector, *HashMap, Lambda, Macro:
				if constructionDepthExceeded(item, depth, limit) {
					return true
				}
			}
		}
	case *HashMap:
		depth++
		if depth > limit {
			return true
		}
		exceeded := false
		val.Each(func(k, v Value) {
			if exceeded {
				return
			}
			exceeded = constructionDepthExceeded(k, depth, limit) || constructionDepthExceeded(v, depth, limit)
		})
		return exceeded
	case Lambda:
		depth++
		if depth > limit {
			return true
		}
		for _, body := range val.Body {
			if constructionDepthExceeded(body, depth, limit) {
				return true
			}
		}
	case Macro:
		depth++
		if depth > limit {
			return true
		}
		for _, body := range val.Body {
			if constructionDepthExceeded(body, depth, limit) {
				return true
			}
		}
	}
	return false
}

func boundedString(v Value, depth int) string {
	if depth > DefaultMaxStructuralDepth {
		return "..."
	}
	switch val := v.(type) {
	case nil:
		return "nil"
	case List:
		parts := make([]string, 0, len(val.Items))
		for _, item := range val.Items {
			s := boundedString(item, depth+1)
			parts = append(parts, s)
			if depth+1 > DefaultMaxStructuralDepth {
				break
			}
		}
		return "(" + strings.Join(parts, " ") + ")"
	case Vector:
		parts := make([]string, 0, len(val.Items))
		for _, item := range val.Items {
			s := boundedString(item, depth+1)
			parts = append(parts, s)
			if depth+1 > DefaultMaxStructuralDepth {
				break
			}
		}
		return "[" + strings.Join(parts, " ") + "]"
	case *HashMap:
		entries := val.sortedEntries()
		parts := make([]string, 0, len(entries)*2)
		for _, e := range entries {
			parts = append(parts, boundedString(e.k, depth+1), boundedString(e.v, depth+1))
			if depth+1 > DefaultMaxStructuralDepth {
				break
			}
		}
		return "{" + strings.Join(parts, " ") + "}"
	case Lambda:
		if val.Name != "" {
			return "#<fn:" + val.Name + ">"
		}
		return "#<fn>"
	case Macro:
		if val.Name != "" {
			return "#<macro:" + val.Name + ">"
		}
		return "#<macro>"
	default:
		return v.String()
	}
}

func boundedEquals(a, b Value, depth int) bool {
	if depth > DefaultMaxStructuralDepth {
		return false
	}
	switch av := a.(type) {
	case nil:
		return b == nil
	case List:
		bv, ok := b.(List)
		if !ok || len(av.Items) != len(bv.Items) {
			return false
		}
		for i := range av.Items {
			if !boundedEquals(av.Items[i], bv.Items[i], depth+1) {
				return false
			}
		}
		return true
	case Vector:
		bv, ok := b.(Vector)
		if !ok || len(av.Items) != len(bv.Items) {
			return false
		}
		for i := range av.Items {
			if !boundedEquals(av.Items[i], bv.Items[i], depth+1) {
				return false
			}
		}
		return true
	case *HashMap:
		bv, ok := b.(*HashMap)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		equal := true
		av.eachRaw(func(e entry) {
			if !equal {
				return
			}
			other, found := bv.getByHashKey(e.hk)
			if !found || !boundedEquals(e.v, other, depth+1) {
				equal = false
			}
		})
		return equal
	default:
		return a.Equals(b)
	}
}

func boundedDeepBytes(v Value, depth int) int64 {
	if depth > DefaultMaxStructuralDepth {
		return 0
	}
	switch val := v.(type) {
	case nil:
		return 0
	case List:
		bytes := ListShallowBytes(len(val.Items))
		for _, item := range val.Items {
			bytes += boundedDeepBytes(item, depth+1)
		}
		return bytes
	case Vector:
		bytes := VectorShallowBytes(len(val.Items))
		for _, item := range val.Items {
			bytes += boundedDeepBytes(item, depth+1)
		}
		return bytes
	case *HashMap:
		bytes := HashMapShallowBytes(val.Len())
		val.Each(func(k, v Value) {
			bytes += boundedDeepBytes(k, depth+1) + boundedDeepBytes(v, depth+1)
		})
		return bytes
	case Lambda:
		bytes := ClosureShallowBytes(len(val.Params)+len(val.Body)) + StringShallowBytes(len(val.Name))
		for _, p := range val.Params {
			bytes += boundedDeepBytes(p, depth+1)
		}
		if val.Variadic.V != "" {
			bytes += boundedDeepBytes(val.Variadic, depth+1)
		}
		for _, body := range val.Body {
			bytes += boundedDeepBytes(body, depth+1)
		}
		return bytes
	case Macro:
		bytes := ClosureShallowBytes(len(val.Params)+len(val.Body)) + StringShallowBytes(len(val.Name))
		for _, p := range val.Params {
			bytes += boundedDeepBytes(p, depth+1)
		}
		if val.Variadic.V != "" {
			bytes += boundedDeepBytes(val.Variadic, depth+1)
		}
		for _, body := range val.Body {
			bytes += boundedDeepBytes(body, depth+1)
		}
		return bytes
	default:
		return ValueShallowBytes(v)
	}
}

func boundedNodeCount(v Value, depth int) int {
	if depth > DefaultMaxStructuralDepth {
		return 0
	}
	switch val := v.(type) {
	case nil:
		return 0
	case List:
		nodes := 1
		for _, item := range val.Items {
			nodes += boundedNodeCount(item, depth+1)
		}
		return nodes
	case Vector:
		nodes := 1
		for _, item := range val.Items {
			nodes += boundedNodeCount(item, depth+1)
		}
		return nodes
	case *HashMap:
		nodes := 1
		val.Each(func(k, v Value) {
			nodes += boundedNodeCount(k, depth+1) + boundedNodeCount(v, depth+1)
		})
		return nodes
	case Lambda:
		nodes := 1
		for _, body := range val.Body {
			nodes += boundedNodeCount(body, depth+1)
		}
		return nodes
	case Macro:
		nodes := 1
		for _, body := range val.Body {
			nodes += boundedNodeCount(body, depth+1)
		}
		return nodes
	default:
		return 1
	}
}

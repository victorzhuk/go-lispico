package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type valueWalkBudget struct {
	ctx      context.Context
	st       *evalState
	limit    int64
	used     int64
	since    int64
	terminal error
}

func newValueWalkBudget(ctx context.Context) *valueWalkBudget {
	if ctx == nil {
		ctx = context.Background()
	}
	st := evalStateFrom(ctx)
	limit := st.maxAllocBytes / MeterValueSlotBytes
	if limit < 1 {
		limit = 1
	}
	return &valueWalkBudget{ctx: ctx, st: st, limit: limit}
}

func (w *valueWalkBudget) resource() error {
	return &LispicoError{Code: CodeResourceLimit, Message: "value walk allocation budget exceeded"}
}

func (w *valueWalkBudget) sync() error {
	if w.terminal != nil {
		return w.terminal
	}
	if !w.st.deadline.IsZero() && !time.Now().Before(w.st.deadline) {
		w.terminal = context.DeadlineExceeded
		return w.terminal
	}
	if err := w.ctx.Err(); err != nil {
		w.terminal = err
		return err
	}
	return nil
}

func (w *valueWalkBudget) step() error {
	if w.terminal != nil {
		return w.terminal
	}
	if w.used >= w.limit {
		w.terminal = w.resource()
		return w.terminal
	}
	w.used++
	w.since++
	if w.since >= checkInterval {
		w.since = 0
		if err := w.sync(); err != nil {
			return err
		}
	}
	return nil
}

func (w *valueWalkBudget) reserve(n int64) error {
	if n <= 0 {
		return nil
	}
	units := (n + MeterValueSlotBytes - 1) / MeterValueSlotBytes
	if units > w.limit-w.used {
		w.terminal = w.resource()
		return w.terminal
	}
	w.used += units
	w.since += units
	if w.since >= checkInterval {
		w.since = 0
		return w.sync()
	}
	return nil
}

func ValueStringContext(ctx context.Context, v Value) (string, error) {
	w := newValueWalkBudget(ctx)
	var b strings.Builder
	if err := walkString(&b, v, 0, w); err != nil {
		return "", err
	}
	if err := w.sync(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func walkString(b *strings.Builder, v Value, depth int, w *valueWalkBudget) error {
	if depth > DefaultMaxStructuralDepth {
		b.WriteString("...")
		return nil
	}
	if err := w.step(); err != nil {
		return err
	}
	switch val := v.(type) {
	case nil:
		b.WriteString("nil")
	case List:
		b.WriteByte('(')
		first := true
		var err error
		val.each(func(item Value) bool {
			if !first { b.WriteByte(' ') }
			first = false
			err = walkString(b, item, depth+1, w)
			return err == nil
		})
		if err != nil { return err }
		b.WriteByte(')')
	case Vector:
		b.WriteByte('[')
		for i := 0; i < val.Len(); i++ {
			if i > 0 { b.WriteByte(' ') }
			if err := walkString(b, val.At(i), depth+1, w); err != nil { return err }
		}
		b.WriteByte(']')
	case *HashMap:
		b.WriteByte('{')
		for i, e := range val.sortedEntries() {
			if i > 0 { b.WriteByte(' ') }
			if err := walkString(b, e.k, depth+1, w); err != nil { return err }
			b.WriteByte(' ')
			if err := walkString(b, e.v, depth+1, w); err != nil { return err }
		}
		b.WriteByte('}')
	case Lambda:
		s := boundedString(val, depth)
		if err := w.reserve(int64(len(s))); err != nil { return err }; b.WriteString(s)
	case Macro:
		s := boundedString(val, depth)
		if err := w.reserve(int64(len(s))); err != nil { return err }; b.WriteString(s)
	default:
		s := v.String()
		if _, ok := v.(String); ok {
			if err := w.reserve(int64(len(s))); err != nil { return err }
		}
		b.WriteString(s)
	}
	return nil
}

func ValueDeepBytesContext(ctx context.Context, v Value) (int64, error) {
	w := newValueWalkBudget(ctx)
	n, err := walkDeepBytes(v, 0, w)
	if err != nil { return 0, err }
	if err := w.sync(); err != nil { return 0, err }
	return n, nil
}

func walkDeepBytes(v Value, depth int, w *valueWalkBudget) (int64, error) {
	if depth > DefaultMaxStructuralDepth { return 0, nil }
	if err := w.step(); err != nil { return 0, err }
	switch val := v.(type) {
	case nil: return 0, nil
	case List:
		n := ListShallowBytes(val.Len()); var err error
		val.each(func(x Value) bool { var z int64; z, err = walkDeepBytes(x, depth+1, w); n += z; return err == nil }); return n, err
	case Vector:
		n := VectorShallowBytes(val.Len()); for i:=0;i<val.Len();i++ { z,err:=walkDeepBytes(val.At(i),depth+1,w); n+=z; if err!=nil{return 0,err} }; return n,nil
	case *HashMap:
		n:=HashMapShallowBytes(val.Len()); var err error; val.Each(func(k,v Value){if err!=nil{return}; a,e:=walkDeepBytes(k,depth+1,w);if e!=nil{err=e;return}; c,e:=walkDeepBytes(v,depth+1,w);n+=a+c;err=e});return n,err
	case Lambda: return boundedDeepBytes(val, depth), nil
	case Macro: return boundedDeepBytes(val, depth), nil
	default: return ValueShallowBytes(v), nil
	}
}

func ValueNodeCountContext(ctx context.Context, v Value) (int, error) {
	w:=newValueWalkBudget(ctx); n,err:=walkNodeCount(v,0,w);if err!=nil{return 0,err};if err=w.sync();err!=nil{return 0,err};return n,nil
}
func walkNodeCount(v Value, depth int, w *valueWalkBudget)(int,error){
	if depth>DefaultMaxStructuralDepth{return 0,nil};if err:=w.step();err!=nil{return 0,err};switch val:=v.(type){case nil:return 0,nil;case List:n:=1;var err error;val.each(func(x Value)bool{z,e:=walkNodeCount(x,depth+1,w);n+=z;err=e;return e==nil});return n,err;case Vector:n:=1;for i:=0;i<val.Len();i++{z,e:=walkNodeCount(val.At(i),depth+1,w);n+=z;if e!=nil{return 0,e}};return n,nil;case *HashMap:n:=1;var err error;val.Each(func(k,v Value){if err!=nil{return};a,e:=walkNodeCount(k,depth+1,w);if e!=nil{err=e;return};c,e:=walkNodeCount(v,depth+1,w);n+=a+c;err=e});return n,err;case Lambda,Macro:return 1,nil;default:return 1,nil}}

func CheckConstructionDepthContext(ctx context.Context, v Value, eval Evaluator) error { return checkDepthContext(ctx,v,0,eval) }
func CheckNestedElementDepthContext(ctx context.Context, v Value, eval Evaluator) error { return checkDepthContext(ctx,v,1,eval) }
func checkDepthContext(ctx context.Context,v Value,depth int,eval Evaluator) error { limit:=DefaultMaxStructuralDepth;if de,ok:=eval.(ConstructionDepthEvaluator);ok&&de.ConstructionDepthLimit()>0{limit=de.ConstructionDepthLimit()};w:=newValueWalkBudget(ctx);bad,err:=walkDepth(v,depth,limit,w);if err!=nil{return err};if bad{return &LispicoError{Code:CodeResourceLimit,Message:fmt.Sprintf("structural depth limit %d exceeded",limit)}};return w.sync() }
func walkDepth(v Value,depth,limit int,w *valueWalkBudget)(bool,error){if depth>limit{return true,nil};if err:=w.step();err!=nil{return false,err};switch val:=v.(type){case List:depth++;if depth>limit{return true,nil};var bad bool;var err error;val.each(func(x Value)bool{bad,err=walkDepth(x,depth,limit,w);return err==nil&&!bad});return bad,err;case Vector:depth++;if depth>limit{return true,nil};for i:=0;i<val.Len();i++{bad,err:=walkDepth(val.At(i),depth,limit,w);if err!=nil||bad{return bad,err}};case *HashMap:depth++;if depth>limit{return true,nil};var bad bool;var err error;val.Each(func(k,v Value){if bad||err!=nil{return};bad,err=walkDepth(k,depth,limit,w);if !bad&&err==nil{bad,err=walkDepth(v,depth,limit,w)}});return bad,err;case Lambda,Macro:depth++;if depth>limit{return true,nil}};return false,nil}

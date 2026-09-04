package core

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"
)

// sharedConsChain builds the canonical shared DAG without materializing its
// exponential expansion: each level references the prior list twice.
func sharedConsChain(levels int, leaves []Value) Value {
    var v Value = NewList(leaves)
    for range levels {
        v = NewList([]Value{v, v})
    }
    return v
}

func runWalk(t *testing.T, want string, fn func()) {
    t.Helper()
    defer func() {
        if p := recover(); p != nil {
            if p == "not implemented" {
                t.Fatalf("%s: contextual walk API is not implemented", want)
            }
            panic(p)
        }
    }()
    fn()
}

func TestValueWalk_SharedGraphBounded(t *testing.T) {
    v := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
    ctx := WithEvalResourceLimits(context.Background(), 1_000_000, 4096)
    runWalk(t, "shared graph must refuse deterministically at the 4096-byte/256-unit ceiling", func() {
        _, err := ValueStringContext(ctx, v)
        var le *LispicoError
        if !errors.As(err, &le) || le.Code != CodeResourceLimit || !IsTerminalEvalError(err) {
            t.Fatalf("shared graph walk error = %v, want terminal %s", err, CodeResourceLimit)
        }
    })
}

func TestValueWalk_OrdinaryParity(t *testing.T) {
    v := NewList([]Value{Int{V: 1}, String{V: "ok"}, Bool{V: true}})
    runWalk(t, "ordinary values must preserve host rendering and metrics exactly", func() {
        s, err := ValueStringContext(context.Background(), v)
        if err != nil || s != v.String() { t.Fatalf("context String = %q, %v; want %q, nil", s, err, v.String()) }
        b, err := ValueDeepBytesContext(context.Background(), v)
        if err != nil || b != ValueDeepBytes(v) { t.Fatalf("context DeepBytes = %d, %v; want %d, nil", b, err, ValueDeepBytes(v)) }
        n, err := ValueNodeCountContext(context.Background(), v)
        if err != nil || n != ValueNodeCount(v) { t.Fatalf("context NodeCount = %d, %v; want %d, nil", n, err, ValueNodeCount(v)) }
    })
}

func TestValueWalk_DepthBoundary(t *testing.T) {
    eval := depthLimitEvaluator{limit: DefaultMaxStructuralDepth}
    runWalk(t, "context depth checks must preserve the exact structural boundary", func() {
        if err := CheckConstructionDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth), eval); err != nil { t.Fatalf("at boundary: %v", err) }
        if err := CheckConstructionDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth+1), eval); err == nil { t.Fatal("one past boundary: want ResourceLimit") }
        if err := CheckNestedElementDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth), eval); err == nil { t.Fatal("nested element over boundary: want ResourceLimit") }
    })
}

func TestValueWalk_TerminalClasses(t *testing.T) {
    v := sharedConsChain(5, []Value{Int{V: 1}})
    runWalk(t, "context walks must classify resource, deadline, and cancellation terminals", func() {
        _, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 16), v)
        if errCode(t, err) != CodeResourceLimit { t.Fatalf("resource terminal = %v, want %s", err, CodeResourceLimit) }
        d := WithEvalDeadline(WithEvalResourceLimits(context.Background(), 1_000_000, 1<<30), timeNowPast())
        _, err = ValueStringContext(d, v)
        if !errors.Is(err, context.DeadlineExceeded) { t.Fatalf("deadline terminal = %v, want DeadlineExceeded", err) }
        c, cancel := context.WithCancel(context.Background()); cancel()
        _, err = ValueStringContext(c, v)
        if !errors.Is(err, context.Canceled) { t.Fatalf("cancel terminal = %v, want Canceled", err) }
    })
}

func TestValueWalk_TerminalPrecedence(t *testing.T) {
    v := sharedConsChain(5, []Value{Int{V: 1}})
    runWalk(t, "terminal reduction must prioritize ResourceLimit over deadline and cancellation", func() {
        c, cancel := context.WithCancel(context.Background()); cancel()
        c = WithEvalDeadline(WithEvalResourceLimits(c, 1_000_000, 16), timeNowPast())
        _, err := ValueStringContext(c, v)
        if errCode(t, err) != CodeResourceLimit { t.Fatalf("precedence = %v, want %s", err, CodeResourceLimit) }
    })
}

func TestValueWalk_RenderReservation(t *testing.T) {
    v := String{V: strings.Repeat("x", 1200)}
    runWalk(t, "render reservation must use ceil(bytes/16) against MaxAllocationBytes/16", func() {
        _, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 1024), v)
        if errCode(t, err) != CodeResourceLimit { t.Fatalf("render reservation = %v, want %s", err, CodeResourceLimit) }
    })
}

func TestSharedWalkFixtureAccounting(t *testing.T) {
    v := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
    runWalk(t, "canonical 10-scalar/5-self-Cons fixture must be refused at 256 units", func() {
        _, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 4096), v)
        if errCode(t, err) != CodeResourceLimit { t.Fatalf("fixture accounting = %v, want %s", err, CodeResourceLimit) }
    })
}

func BenchmarkSharedValueWalk(b *testing.B) {
    for _, levels := range []int{8, 12, 16} {
        b.Run("Cons"+itoa(levels), func(b *testing.B) {
            v := sharedConsChain(levels, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
            ctx := WithEvalResourceLimits(context.Background(), 1_000_000_000, 1<<30)
            b.ReportAllocs()
            for range b.N { _, _ = ValueStringContext(ctx, v) }
        })
    }
    b.Log("canonical 26-Cons baseline is report-only; do not render it")
}

func timeNowPast() time.Time { return time.Now().Add(-time.Millisecond) }
func itoa(n int) string { return fmt.Sprintf("%d", n) }

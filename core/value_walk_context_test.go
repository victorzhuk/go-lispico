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
		if err != nil || s != v.String() {
			t.Fatalf("context String = %q, %v; want %q, nil", s, err, v.String())
		}
		b, err := ValueDeepBytesContext(context.Background(), v)
		if err != nil || b != ValueDeepBytes(v) {
			t.Fatalf("context DeepBytes = %d, %v; want %d, nil", b, err, ValueDeepBytes(v))
		}
		n, err := ValueNodeCountContext(context.Background(), v)
		if err != nil || n != ValueNodeCount(v) {
			t.Fatalf("context NodeCount = %d, %v; want %d, nil", n, err, ValueNodeCount(v))
		}
	})
}

func TestValueWalk_DepthBoundary(t *testing.T) {
	eval := depthLimitEvaluator{limit: DefaultMaxStructuralDepth}
	runWalk(t, "context depth checks must preserve the exact structural boundary", func() {
		if err := CheckConstructionDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth), eval); err != nil {
			t.Fatalf("at boundary: %v", err)
		}
		if err := CheckConstructionDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth+1), eval); err == nil {
			t.Fatal("one past boundary: want ResourceLimit")
		}
		if err := CheckNestedElementDepthContext(context.Background(), nestedList(DefaultMaxStructuralDepth), eval); err == nil {
			t.Fatal("nested element over boundary: want ResourceLimit")
		}
	})
}

func TestValueWalk_TerminalClasses(t *testing.T) {
	v := sharedConsChain(5, []Value{Int{V: 1}})
	runWalk(t, "context walks must classify resource, deadline, and cancellation terminals", func() {
		_, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 16), v)
		if errCode(t, err) != CodeResourceLimit {
			t.Fatalf("resource terminal = %v, want %s", err, CodeResourceLimit)
		}
		d := WithEvalDeadline(WithEvalResourceLimits(context.Background(), 1_000_000, 1<<30), timeNowPast())
		_, err = ValueStringContext(d, v)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline terminal = %v, want DeadlineExceeded", err)
		}
		c, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = ValueStringContext(c, v)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel terminal = %v, want Canceled", err)
		}
	})
}

func TestValueWalk_TerminalPrecedence(t *testing.T) {
	v := sharedConsChain(5, []Value{Int{V: 1}})
	runWalk(t, "terminal reduction must prioritize ResourceLimit over deadline and cancellation", func() {
		c, cancel := context.WithCancel(context.Background())
		cancel()
		c = WithEvalDeadline(WithEvalResourceLimits(c, 1_000_000, 16), timeNowPast())
		_, err := ValueStringContext(c, v)
		if errCode(t, err) != CodeResourceLimit {
			t.Fatalf("precedence = %v, want %s", err, CodeResourceLimit)
		}
	})
}

func TestValueWalk_RenderReservation(t *testing.T) {
	v := String{V: strings.Repeat("x", 1200)}
	runWalk(t, "render reservation must use ceil(bytes/16) against MaxAllocationBytes/16", func() {
		_, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 1024), v)
		if errCode(t, err) != CodeResourceLimit {
			t.Fatalf("render reservation = %v, want %s", err, CodeResourceLimit)
		}
	})
}

// TestValueWalk_WorkCap pins the per-walk work ceiling on the canonical
// low-cap fixture: a 10-scalar base wrapped in exactly 5 shared Cons levels
// has 11 * 2^5 = 352 logical visits, deterministically above the 256-unit
// ceiling derived from MaxAllocationBytes 4,096 (4096/16 = 256). Every walk
// entry point named by the plan must refuse with a terminal resource limit
// there, not run to completion or grow with the reference count.
func TestValueWalk_WorkCap(t *testing.T) {
	v := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
	ctx := WithEvalResourceLimits(context.Background(), 1_000_000, 4096)
	eval := depthLimitEvaluator{limit: DefaultMaxStructuralDepth}

	cases := []struct {
		name string
		run  func() error
	}{
		{"ValueStringContext", func() error { _, err := ValueStringContext(ctx, v); return err }},
		{"ValueDeepBytesContext", func() error { _, err := ValueDeepBytesContext(ctx, v); return err }},
		{"ValueNodeCountContext", func() error { _, err := ValueNodeCountContext(ctx, v); return err }},
		{"CheckConstructionDepthContext", func() error { return CheckConstructionDepthContext(ctx, v, eval) }},
		{"CheckNestedElementDepthContext", func() error { return CheckNestedElementDepthContext(ctx, v, eval) }},
		// EqualsBounded charges one unit per compared node pair and
		// synchronizes every 128 units: a 192-reduction remainder (1.5
		// batches) refuses deterministically mid-walk on the 352-visit
		// fixture, proving the check sits inside the walk loop.
		{"EqualsBounded", func() error {
			_, err := EqualsBounded(v, v, NewBuiltinWorkBudget(budgetCtx(context.Background(), 192)))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWalk(t, tc.name+" must refuse terminally at the 256-unit work ceiling", func() {
				err := tc.run()
				if err == nil {
					t.Fatalf("%s on 352-visit shared fixture under a 256-unit ceiling: want terminal %s, got nil", tc.name, CodeResourceLimit)
				}
				if errCode(t, err) != CodeResourceLimit {
					t.Fatalf("%s error = %v, want code %s", tc.name, err, CodeResourceLimit)
				}
				if !IsTerminalEvalError(err) {
					t.Fatalf("%s error = %v, want a terminal eval error", tc.name, err)
				}
			})
		})
	}
}

// TestValueWalk_HostDegradation pins the context-free host entry points on
// the canonical 19-Cons fixture: 11 * 2^19 = 5,767,168 logical visits,
// deterministically above the default 4,194,304-unit ceiling, at structural
// depth 20 (far inside MaxStructuralDepth 1024). Host walks must terminate
// and answer from their own bounded rules — depth-only answers for the depth
// checks, deterministic values for String/Equals/metrics — never a work-cap
// error, while the contextual twin of the same walk refuses at the ceiling.
func TestValueWalk_HostDegradation(t *testing.T) {
	v := sharedConsChain(19, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
	depthEval := depthLimitEvaluator{limit: DefaultMaxStructuralDepth}

	runWalk(t, "host walks must terminate with bounded answers above the default work ceiling", func() {
		// Depth-only answers: within MaxStructuralDepth the checks return
		// their existing nil/bounded answers with no work-cap error.
		if err := CheckConstructionDepthWith(v, depthEval); err != nil {
			t.Fatalf("CheckConstructionDepthWith above default work ceiling = %v, want nil (depth-only answer)", err)
		}
		if err := CheckNestedElementDepthWith(v, depthEval); err != nil {
			t.Fatalf("CheckNestedElementDepthWith above default work ceiling = %v, want nil (depth-only answer)", err)
		}
		if ValueDepthExceeds(v, DefaultMaxStructuralDepth) {
			t.Fatal("ValueDepthExceeds at depth 20 under limit 1024 = true, want false")
		}
		// The depth answer itself is unchanged by sharing: one level past
		// the limit it still refuses on depth, not on work.
		if err := CheckConstructionDepthWith(v, depthLimitEvaluator{limit: 5}); errCode(t, err) != CodeResourceLimit {
			t.Fatalf("CheckConstructionDepthWith past a depth limit of 5 = %v, want %s", err, CodeResourceLimit)
		}

		// Metric hosts terminate and are deterministic above the ceiling.
		b1, b2 := ValueDeepBytes(v), ValueDeepBytes(v)
		if b1 <= 0 || b1 != b2 {
			t.Fatalf("ValueDeepBytes on 19-Cons fixture = %d then %d, want positive and deterministic", b1, b2)
		}
		n1, n2 := ValueNodeCount(v), ValueNodeCount(v)
		if n1 <= 0 || n1 != n2 {
			t.Fatalf("ValueNodeCount on 19-Cons fixture = %d then %d, want positive and deterministic", n1, n2)
		}

		// String and Equals hosts terminate on the shared structure and
		// stay self-consistent.
		s1, s2 := v.String(), v.String()
		if s1 == "" || s1 != s2 {
			t.Fatalf("host String on 19-Cons fixture: want non-empty deterministic render, got %d then %d bytes", len(s1), len(s2))
		}
		if !v.Equals(sharedConsChain(19, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})) {
			t.Fatal("host Equals of two identical 19-Cons fixtures = false, want true")
		}

		// The contextual twin of the same walk refuses at the default
		// 4,194,304-unit ceiling: 5,767,168 visits are above it.
		if _, err := ValueDeepBytesContext(context.Background(), v); errCode(t, err) != CodeResourceLimit || !IsTerminalEvalError(err) {
			t.Fatalf("ValueDeepBytesContext on 19-Cons fixture = %v, want terminal %s", err, CodeResourceLimit)
		}
	})
}

func TestSharedWalkFixtureAccounting(t *testing.T) {
	v := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
	runWalk(t, "canonical 10-scalar/5-self-Cons fixture must be refused at 256 units", func() {
		_, err := ValueStringContext(WithEvalResourceLimits(context.Background(), 1_000_000, 4096), v)
		if errCode(t, err) != CodeResourceLimit {
			t.Fatalf("fixture accounting = %v, want %s", err, CodeResourceLimit)
		}
	})
}

func BenchmarkSharedValueWalk(b *testing.B) {
	for _, levels := range []int{8, 12, 16} {
		b.Run("Cons"+itoa(levels), func(b *testing.B) {
			v := sharedConsChain(levels, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
			ctx := WithEvalResourceLimits(context.Background(), 1_000_000_000, 1<<30)
			b.ReportAllocs()
			for range b.N {
				_, _ = ValueStringContext(ctx, v)
			}
		})
	}
	b.Log("canonical 26-Cons baseline is report-only; do not render it")
}

// TestValueWalk_ClosureBodyIsBudgeted places the canonical 352-visit shared
// fixture where only a closure body reaches it, under the same 256-unit
// ceiling TestValueWalk_WorkCap uses. A walk that descends Params, Variadic
// and Body under its own budget exhausts that ceiling; one that hands the
// closure to a context-free helper answers with the whole figure instead, so
// the bound the ceiling exists to enforce is absent behind every Lambda and
// Macro.
func TestValueWalk_ClosureBodyIsBudgeted(t *testing.T) {
	shared := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
	ctx := WithEvalResourceLimits(context.Background(), 1_000_000, 4096)

	for _, c := range []struct {
		name string
		v    Value
	}{
		{"Lambda", Lambda{Name: "f", Body: []Value{shared}}},
		{"Macro", Macro{Name: "m", Body: []Value{shared}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Run("ValueDeepBytesContext", func(t *testing.T) {
				runWalk(t, c.name+" deep size must refuse terminally at the 256-unit work ceiling", func() {
					n, err := ValueDeepBytesContext(ctx, c.v)
					if err == nil {
						t.Fatalf("ValueDeepBytesContext over a %s whose body holds the 352-visit shared fixture = %d bytes, nil under a 256-unit ceiling: a closure payload must be walked under the same budget as any other node, not handed to the context-free helper", c.name, n)
					}
					if errCode(t, err) != CodeResourceLimit || !IsTerminalEvalError(err) {
						t.Fatalf("ValueDeepBytesContext over a %s body = %v, want terminal %s", c.name, err, CodeResourceLimit)
					}
				})
			})
			t.Run("ValueStringContext", func(t *testing.T) {
				runWalk(t, c.name+" render must refuse terminally at the 256-unit work ceiling", func() {
					s, err := ValueStringContext(ctx, c.v)
					if err == nil {
						t.Fatalf("ValueStringContext over a %s whose body holds the 352-visit shared fixture = %q, nil under a 256-unit ceiling: a closure payload must be walked under the same budget as any other node, not handed to the context-free helper", c.name, s)
					}
					if errCode(t, err) != CodeResourceLimit || !IsTerminalEvalError(err) {
						t.Fatalf("ValueStringContext over a %s body = %v, want terminal %s", c.name, err, CodeResourceLimit)
					}
				})
			})
		})
	}
}

// TestValueWalk_ClosureDepthDescendsBody pins the contextual construction
// depth check to the answer core/depth.go already gives on the identical
// value. constructionDepthExceeded descends a closure's Body, so a form
// nested past the limit inside a Lambda or Macro is refused on the host path;
// the contextual twin counts the closure as one level and stops, accepting
// what the value the check exists to reject.
//
// The under-limit closure is the other half: a check that refused every
// closure would satisfy the first assertion while breaking every ordinary
// program that defines a function.
func TestValueWalk_ClosureDepthDescendsBody(t *testing.T) {
	const limit = 5
	eval := depthLimitEvaluator{limit: limit}
	over, under := nestedList(limit+3), nestedList(limit-3)

	for _, c := range []struct {
		name         string
		over, within Value
	}{
		{"Lambda", Lambda{Name: "f", Body: []Value{over}}, Lambda{Name: "f", Body: []Value{under}}},
		{"Macro", Macro{Name: "m", Body: []Value{over}}, Macro{Name: "m", Body: []Value{under}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			runWalk(t, c.name+" construction depth must match the context-free answer on the same value", func() {
				hostErr := CheckConstructionDepthWith(c.over, eval)
				if hostErr == nil || errCode(t, hostErr) != CodeResourceLimit {
					t.Fatalf("CheckConstructionDepthWith over a %s whose body nests %d levels past a limit of %d = %v, want %s: the parity this test pins is against the host path's refusal", c.name, limit+3-limit, limit, hostErr, CodeResourceLimit)
				}
				err := CheckConstructionDepthContext(context.Background(), c.over, eval)
				if err == nil {
					t.Fatalf("CheckConstructionDepthContext over a %s whose body nests %d levels under a limit of %d = nil while CheckConstructionDepthWith refuses the same value with %s: the contextual check must descend the closure body, not count the closure as its last level", c.name, limit+3, limit, CodeResourceLimit)
				}
				if errCode(t, err) != CodeResourceLimit {
					t.Fatalf("CheckConstructionDepthContext over a %s body = %v, want %s", c.name, err, CodeResourceLimit)
				}
				if err := CheckConstructionDepthWith(c.within, eval); err != nil {
					t.Fatalf("CheckConstructionDepthWith over a %s whose body nests %d levels under a limit of %d = %v, want nil", c.name, limit-3, limit, err)
				}
				if err := CheckConstructionDepthContext(context.Background(), c.within, eval); err != nil {
					t.Fatalf("CheckConstructionDepthContext over a %s whose body nests %d levels under a limit of %d = %v, want nil: descending the body must not refuse a closure that fits", c.name, limit-3, limit, err)
				}
			})
		})
	}
}

// TestValueWalk_ClosureNodeCountDescendsBody pins the contextual node count to
// the answer core/depth.go already gives on the identical value: boundedNodeCount
// descends a closure's Body and counts every form in it, while the contextual
// walk counts the closure as one node and stops. Both halves of that divergence
// are pinned — the count, and the budget: a closure body that costs nothing is a
// hole under every ceiling the walk exists to enforce.
//
// Params are deliberately non-empty: the context-free count charges the Body
// only, so a walk that also counted parameters would diverge the other way.
func TestValueWalk_ClosureNodeCountDescendsBody(t *testing.T) {
	params := []Symbol{{V: "x"}, {V: "y"}}
	body := []Value{
		NewList([]Value{Symbol{V: "+"}, Symbol{V: "x"}, Int{V: 1}}),
		NewList([]Value{Symbol{V: "*"}, Symbol{V: "y"}, Int{V: 2}}),
		String{V: "tail"},
	}
	// One closure node, two 4-node lists, one scalar; parameters are not nodes.
	const wantNodes = 10

	shared := sharedConsChain(5, []Value{Int{V: 0}, Int{V: 1}, Int{V: 2}, Int{V: 3}, Int{V: 4}, Int{V: 5}, Int{V: 6}, Int{V: 7}, Int{V: 8}, Int{V: 9}})
	capped := WithEvalResourceLimits(context.Background(), 1_000_000, 4096)

	for _, c := range []struct {
		name             string
		counted, overCap Value
	}{
		{"Lambda", Lambda{Name: "f", Params: params, Body: body}, Lambda{Name: "f", Params: params, Body: []Value{shared}}},
		{"Macro", Macro{Name: "m", Params: params, Body: body}, Macro{Name: "m", Params: params, Body: []Value{shared}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			runWalk(t, c.name+" node count must match the context-free answer on the same value", func() {
				host := ValueNodeCount(c.counted)
				if host != wantNodes {
					t.Fatalf("ValueNodeCount over a %s with a %d-form body = %d, want %d: the context-free count is the reference this parity pins and must not move", c.name, len(body), host, wantNodes)
				}
				got, err := ValueNodeCountContext(context.Background(), c.counted)
				if err != nil {
					t.Fatalf("ValueNodeCountContext over a %s with a %d-form body = %v, want %d nodes and no error", c.name, len(body), err, wantNodes)
				}
				if got != host {
					t.Fatalf("ValueNodeCountContext over a %s with a %d-form body = %d while ValueNodeCount reports %d on the same value: the contextual walk must descend the closure body, not count the closure as a single node", c.name, len(body), got, host)
				}

				n, err := ValueNodeCountContext(capped, c.overCap)
				if err == nil {
					t.Fatalf("ValueNodeCountContext over a %s whose body holds the 352-visit shared fixture = %d, nil under a 256-unit ceiling: a closure payload must cost budget like any other node, not answer without walking it", c.name, n)
				}
				if errCode(t, err) != CodeResourceLimit || !IsTerminalEvalError(err) {
					t.Fatalf("ValueNodeCountContext over a %s body above the ceiling = %v, want terminal %s", c.name, err, CodeResourceLimit)
				}
			})
		})
	}
}

func timeNowPast() time.Time { return time.Now().Add(-time.Millisecond) }
func itoa(n int) string      { return fmt.Sprintf("%d", n) }

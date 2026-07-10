package stdlib

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestCollections_Contains(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"key present", "(contains? {:a 1} :a)", core.Bool{V: true}},
		{"key absent", "(contains? {:a 1} :b)", core.Bool{V: false}},
		{"empty map", "(contains? {} :a)", core.Bool{V: false}},
		{"string key", `(contains? {"k" 1} "k")`, core.Bool{V: true}},
		{"nil value still present", "(contains? {:a nil} :a)", core.Bool{V: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCollections_Merge(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"two maps", "(merge {:a 1} {:b 2})", "{:a 1 :b 2}"},
		{"later wins", "(merge {:a 1} {:a 2})", "{:a 2}"},
		{"single map", "(merge {:a 1})", "{:a 1}"},
		{"no args", "(merge)", "{}"},
		{"nil skipped", "(merge {:a 1} nil {:b 2})", "{:a 1 :b 2}"},
		{"three maps chain", "(merge {:a 1} {:b 2} {:a 3})", "{:a 3 :b 2}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestCollections_Dissoc(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"remove one", "(dissoc {:a 1 :b 2} :a)", "{:b 2}"},
		{"remove several", "(dissoc {:a 1 :b 2 :c 3} :a :c)", "{:b 2}"},
		{"absent key", "(dissoc {:a 1} :b)", "{:a 1}"},
		{"no keys", "(dissoc {:a 1})", "{:a 1}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestCollections_Dissoc_Immutable(t *testing.T) {
	env := setupEnv(t)

	eval(t, env, "(def m {:a 1 :b 2})")
	eval(t, env, "(dissoc m :a)")
	result := eval(t, env, "m")
	if result.String() != "{:a 1 :b 2}" {
		t.Errorf("dissoc mutated its input: %s", result.String())
	}
}

func TestCollections_Sort(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"ints", "(sort [3 1 2])", "(1 2 3)"},
		{"already sorted", "(sort (list 1 2 3))", "(1 2 3)"},
		{"mixed numbers", "(sort [2.5 1 3])", "(1 2.5 3)"},
		{"strings", `(sort ["b" "a" "c"])`, `("a" "b" "c")`},
		{"keywords", "(sort [:c :a :b])", "(:a :b :c)"},
		{"empty", "(sort [])", "()"},
		{"nil", "(sort nil)", "()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestCollections_Range(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"end only", "(range 3)", "(0 1 2)"},
		{"start end", "(range 2 5)", "(2 3 4)"},
		{"with step", "(range 0 10 3)", "(0 3 6 9)"},
		{"negative step", "(range 3 0 -1)", "(3 2 1)"},
		{"empty", "(range 0)", "()"},
		{"unreachable", "(range 5 2)", "()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestCollections_ExtraErrors(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"contains? on vector", "(contains? [1 2] 1)", "expected map"},
		{"contains? arity", "(contains? {:a 1})", "requires 2 arguments"},
		{"merge non-map", "(merge {:a 1} 5)", "expected map"},
		{"dissoc non-map", "(dissoc [1 2] 0)", "expected map"},
		{"sort mixed kinds", `(sort [1 "a"])`, "cannot compare"},
		{"sort non-collection", "(sort 5)", "expected collection"},
		{"range zero step", "(range 0 5 0)", "step must not be zero"},
		{"range non-int", "(range 1.5)", "requires integer arguments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evalErr(t, env, tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

// TestRange_StandaloneDefaultCap: with no Engine evaluator on the env, range
// uses the safe stdlib default, so an oversized range fails closed and a normal
// one works.
func TestRange_StandaloneDefaultCap(t *testing.T) {
	env := setupEnv(t)
	err := evalErr(t, env, "(range 0 99999999)")
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr) && lerr.Code == core.CodeResourceLimit, "expected ResourceLimitError, got %v", err)

	v := eval(t, env, "(range 0 5)")
	list, ok := v.(core.List)
	require.True(t, ok)
	require.Len(t, list.Items, 5)
}

// TestRange_ExtremeBoundsNoOverflow: a 2-element span just below MaxInt64 must
// compute its cardinality via uint64 magnitudes (no int64 overflow) and return
// the two correct elements.
func TestRange_ExtremeBoundsNoOverflow(t *testing.T) {
	env := setupEnv(t)
	v := eval(t, env, "(range 9223372036854775805 9223372036854775807)")
	list, ok := v.(core.List)
	require.True(t, ok)
	require.Len(t, list.Items, 2)
	i0, _ := list.Items[0].(core.Int)
	i1, _ := list.Items[1].(core.Int)
	require.Equal(t, int64(9223372036854775805), i0.V)
	require.Equal(t, int64(9223372036854775806), i1.V)
}

// cancelAfterN is a context.Context whose Err() returns nil for the first
// `after` calls and context.Canceled afterward, closing its Done channel once
// at the threshold. It drives deterministic mid-loop cancellation with no
// timing and no large allocation.
type cancelAfterN struct {
	n, after int
	done     chan struct{}
}

func newCancelAfterN(after int) *cancelAfterN {
	return &cancelAfterN{after: after, done: make(chan struct{})}
}
func (c *cancelAfterN) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterN) Done() <-chan struct{}       { return c.done }
func (c *cancelAfterN) Value(any) any               { return nil }
func (c *cancelAfterN) Err() error {
	c.n++
	if c.n > c.after {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
		return context.Canceled
	}
	return nil
}

// TestRange_CancelledMidBuild invokes the registered range GoFunc directly
// (bypassing the evaluator's entry ctx check) with a context that cancels after
// a fixed number of Err() probes. range's first probe is its pre-loop check;
// later probes are inside the build loop. With after=50, cancellation fires
// inside the loop, proving the cooperative check aborts mid-build before the
// list is produced. (stdlib-plugin spec: cancellation/time-out mid-build.)
func TestRange_CancelledMidBuild(t *testing.T) {
	env := setupEnv(t)
	fnVal, ok := env.Get("range")
	require.True(t, ok)
	gfn, ok := fnVal.(core.GoFunc)
	require.True(t, ok)

	ctx := newCancelAfterN(50)
	v, err := gfn.Fn(ctx, nil, []core.Value{core.Int{V: 0}, core.Int{V: 1000}}, env)
	require.Error(t, err, "mid-build cancellation must abort range")
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got %v", err)
	if list, ok := v.(core.List); ok {
		assert.Less(t, len(list.Items), 1000, "range must not complete the list on mid-build cancel")
	}
}

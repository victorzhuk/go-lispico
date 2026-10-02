package core_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
	"github.com/victorzhuk/go-lispico/runtime"
)

// markedValue implements core.Callable. Its methods panic: classification
// must decide from the marker alone and never invoke Type or LispCallable.
type markedValue struct{}

func (markedValue) Type() core.Keyword { panic("markedValue.Type must not be called") }
func (markedValue) String() string     { panic("markedValue.String must not be called") }
func (markedValue) Equals(core.Value) bool {
	panic("markedValue.Equals must not be called")
}
func (markedValue) LispCallable() { panic("markedValue.LispCallable must not be called") }

// plainValue implements core.Value without the marker and claims a callable
// type name; classification must still reject it without calling Type.
type plainValue struct{}

func (plainValue) Type() core.Keyword { panic("plainValue.Type must not be called") }
func (plainValue) String() string     { panic("plainValue.String must not be called") }
func (plainValue) Equals(core.Value) bool {
	panic("plainValue.Equals must not be called")
}

// vmClosure builds a real bytecode closure for (fn (x) x).
func vmClosure(t *testing.T) *vm.Closure {
	t.Helper()
	e, err := runtime.New(nil, runtime.WithDialect(cl.Dialect()), runtime.WithBytecode())
	assert.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	v, err := e.Eval(context.Background(), "callable-contract", "(fn (x) x)")
	assert.NoError(t, err)
	c, ok := v.(*vm.Closure)
	assert.True(t, ok, "evaluating (fn (x) x) must yield *vm.Closure, got %T", v)
	return c
}

func TestIsCallable_AllValueKinds(t *testing.T) {
	trueCases := []struct {
		name  string
		value core.Value
	}{
		{"GoFunc", core.GoFunc{Name: "f"}},
		{"Lambda", core.Lambda{}},
		{"Keyword", core.Keyword{V: "fn"}},
		{"VMClosure", vmClosure(t)},
		{"markedValue", markedValue{}},
	}
	for _, tc := range trueCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, core.IsCallable(tc.value),
				"core.IsCallable(%s) must be true without invoking Type or LispCallable", tc.name)
		})
	}

	var nilValue core.Value
	falseCases := []struct {
		name  string
		value core.Value
	}{
		{"nil", nilValue},
		{"Nil", core.Nil{}},
		{"Bool", core.Bool{}},
		{"Int", core.Int{V: 1}},
		{"Float", core.Float{V: 1.5}},
		{"String", core.String{V: "x"}},
		{"Symbol", core.Symbol{V: "x"}},
		{"List", core.NewList([]core.Value{core.Int{V: 1}})},
		{"Vector", core.NewVector([]core.Value{core.Int{V: 1}})},
		{"HashMap", core.NewHashMap()},
		{"Macro", core.Macro{}},
		{"plainValue", plainValue{}},
	}
	for _, tc := range falseCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, core.IsCallable(tc.value),
				"core.IsCallable(%s) must be false without invoking Type", tc.name)
		})
	}
}

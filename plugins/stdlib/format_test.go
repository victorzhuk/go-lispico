package stdlib

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func formatGoFunc(t *testing.T, env *core.Env) core.GoFunc {
	t.Helper()
	v, ok := env.Get("format")
	if !ok {
		t.Fatal("format is not registered")
	}
	fn, ok := v.(core.GoFunc)
	if !ok {
		t.Fatalf("format is not a GoFunc: %T", v)
	}
	return fn
}

func TestStrings_Format(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"string placeholder", `(format "%s" "hi")`, core.String{V: "hi"}},
		{"int placeholder", `(format "%d" 42)`, core.String{V: "42"}},
		{"indexed int placeholder", `(format "%[2]d" "skip" 42)`, core.String{V: "42"}},
		{"default placeholder", `(format "%v" "hello")`, core.String{V: "hello"}},
		{"empty format", `(format "" "x")`, core.String{V: ""}},
		{"no placeholders", `(format "hello")`, core.String{V: "hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}

	t.Run("type mismatch", func(t *testing.T) {
		result := eval(t, env, `(format "%d" "not-a-number")`)
		s, ok := result.(core.String)
		if !ok {
			t.Fatalf("expected String, got %T", result)
		}
		if !strings.Contains(s.V, "%!d") {
			t.Errorf("expected %%!d format error in result, got %q", s.V)
		}
	})
}

func TestStrings_FormatChargesDefaultFloatPrecision(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	expected := fmt.Sprintf("%f", 1e308)
	tightBudget := int64(len(expected)) - 1
	if tightBudget < 0 {
		t.Fatalf("tight budget underflow: %d", tightBudget)
	}

	closedCtx := core.WithEvalResourceLimits(t.Context(), 1<<20, int(tightBudget))
	_, err := fn.Fn(closedCtx, nil, []core.Value{
		core.String{V: "%f"},
		core.Float{V: 1e308},
	}, env)
	requireResourceLimit(t, err)

	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)
	got, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%f"},
		core.Float{V: 1e308},
	}, env)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	rendered, ok := got.(core.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if rendered.V != expected {
		t.Fatalf("format output %q, want %q", rendered.V, expected)
	}

	charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	if charged < int64(len(expected)) {
		t.Fatalf("charged %d bytes, want at least %d", charged, len(expected))
	}
}

func TestStrings_FormatChargesEstimatedOutputBeforeSprintf(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)
	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%10000000s"},
		core.String{V: "x"},
	}, env)
	runtime.ReadMemStats(&after)

	requireResourceLimit(t, err)
	if got := after.TotalAlloc - before.TotalAlloc; got > 4<<20 {
		t.Fatalf("format allocated %d bytes before returning limit error", got)
	}
}

func TestStrings_FormatChargesPrecisionAndDynamicWidth(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	tests := []struct {
		name string
		args []core.Value
	}{
		{
			name: "large precision",
			args: []core.Value{
				core.String{V: "%.999999f"},
				core.Float{V: 1.25},
			},
		},
		{
			name: "dynamic width",
			args: []core.Value{
				core.String{V: "%*s"},
				core.Int{V: 10_000_000},
				core.String{V: "x"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)
			_, err := fn.Fn(ctx, nil, tt.args, env)
			requireResourceLimit(t, err)
		})
	}
}

func TestStrings_FormatChargesIndexedDynamicWidth(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)

	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%[2]*s"},
		core.String{V: "x"},
		core.Int{V: 1_000_000},
		core.String{V: "ok"},
	}, env)
	requireResourceLimit(t, err)
}

func TestStrings_FormatChargesIndexedDynamicWidthImplicitValue(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)
	huge := strings.Repeat("x", 2<<20)
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)

	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%[1]*s"},
		core.Int{V: 1},
		core.String{V: huge},
	}, env)
	requireResourceLimit(t, err)

	charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	if charged < int64(len(huge)) {
		t.Fatalf("charged %d bytes, want at least %d", charged, len(huge))
	}
}

func TestStrings_FormatChargesHexLargeString(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	huge := strings.Repeat("x", 2<<20)
	ctx := core.WithEvalResourceLimits(t.Context(), len(huge)*2-1, 4096)

	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%x"},
		core.String{V: huge},
	}, env)
	requireResourceLimit(t, err)

	charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	if charged < int64(2*len(huge)) {
		t.Fatalf("charged %d bytes, want at least %d", charged, 2*len(huge))
	}
}

func TestStrings_FormatChargesAltHexLargeString(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	huge := strings.Repeat("x", 2<<20)
	tests := []struct {
		name   string
		format string
		want   int64
	}{
		{
			name:   "alternate hex",
			format: "%#x",
			want:   int64(len(huge))*2 + 2,
		},
		{
			name:   "alternate spaced hex",
			format: "%# x",
			want:   int64(len(huge))*5 - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := core.WithEvalResourceLimits(t.Context(), int(tt.want-1), 4096)
			_, err := fn.Fn(ctx, nil, []core.Value{
				core.String{V: tt.format},
				core.String{V: huge},
			}, env)
			requireResourceLimit(t, err)

			charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
			if charged < tt.want {
				t.Fatalf("charged %d bytes, want at least %d", charged, tt.want)
			}
		})
	}
}

func TestStrings_FormatChargesIndexedDynamicWidthAndIndexedValue(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)
	huge := strings.Repeat("x", 2<<20)
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)

	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "%[2]*[1]s"},
		core.String{V: huge},
		core.Int{V: 1},
	}, env)
	requireResourceLimit(t, err)

	charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	if charged < int64(len(huge)) {
		t.Fatalf("charged %d bytes, want at least %d", charged, len(huge))
	}
}

func TestStrings_FormatUnderBudgetChargesEstimate(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 1<<20)

	got, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: "hello %06d %.2f %% %s"},
		core.Int{V: 42},
		core.Float{V: 1.25},
		core.String{V: "ok"},
	}, env)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	want := core.String{V: "hello 000042 1.25 % ok"}
	if !got.Equals(want) {
		t.Fatalf("format = %v, want %v", got, want)
	}
	charged := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	if charged < core.ValueShallowBytes(want) {
		t.Fatalf("charged %d bytes, want at least %d", charged, core.ValueShallowBytes(want))
	}
}

func requireResourceLimit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected ResourceLimitError")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("error = %v, want %s", err, core.CodeResourceLimit)
	}
}

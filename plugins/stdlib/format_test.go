package stdlib

import (
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestStrings_Format(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"string placeholder", `(format "%s" "hi")`, core.String{V: "hi"}},
		{"int placeholder", `(format "%d" 42)`, core.String{V: "42"}},
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

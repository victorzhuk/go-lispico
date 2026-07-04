package stdlib

import (
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestComparison_Equals(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"single arg", "(= 1)", core.Bool{V: true}},
		{"equal ints", "(= 1 1)", core.Bool{V: true}},
		{"unequal ints", "(= 1 2)", core.Bool{V: false}},
		{"chain equal", "(= 2 2 2 2)", core.Bool{V: true}},
		{"chain one differs", "(= 2 2 3)", core.Bool{V: false}},
		{"int vs float is structural", "(= 1 1.0)", core.Bool{V: false}},
		{"equal floats", "(= 1.5 1.5)", core.Bool{V: true}},
		{"equal strings", `(= "a" "a")`, core.Bool{V: true}},
		{"unequal strings", `(= "a" "b")`, core.Bool{V: false}},
		{"equal keywords", "(= :cheap :cheap)", core.Bool{V: true}},
		{"unequal keywords", "(= :cheap :smart)", core.Bool{V: false}},
		{"cross type", `(= 1 "1")`, core.Bool{V: false}},
		{"nil equals nil", "(= nil nil)", core.Bool{V: true}},
		{"equal vectors", "(= [1 2] [1 2])", core.Bool{V: true}},
		{"unequal vectors", "(= [1 2] [1 3])", core.Bool{V: false}},
		{"equal maps", "(= {:a 1} {:a 1})", core.Bool{V: true}},
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

func TestComparison_Ordering(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"lt true", "(< 1 2)", core.Bool{V: true}},
		{"lt false", "(< 2 1)", core.Bool{V: false}},
		{"lt equal", "(< 1 1)", core.Bool{V: false}},
		{"lt chain monotonic", "(< 1 2 3)", core.Bool{V: true}},
		{"lt chain broken", "(< 1 3 2)", core.Bool{V: false}},
		{"lt single arg", "(< 5)", core.Bool{V: true}},
		{"lt int float mix", "(< 1 1.5)", core.Bool{V: true}},
		{"lt float int mix", "(< 2.5 2)", core.Bool{V: false}},
		{"lt big ints exact", "(< 9007199254740992 9007199254740993)", core.Bool{V: true}},
		{"gt true", "(> 2 1)", core.Bool{V: true}},
		{"gt false", "(> 1 2)", core.Bool{V: false}},
		{"gt chain", "(> 3 2 1)", core.Bool{V: true}},
		{"gt chain broken", "(> 3 1 2)", core.Bool{V: false}},
		{"le equal", "(<= 1 1)", core.Bool{V: true}},
		{"le chain with equal", "(<= 1 1 2)", core.Bool{V: true}},
		{"le chain broken", "(<= 2 1)", core.Bool{V: false}},
		{"ge equal", "(>= 1 1)", core.Bool{V: true}},
		{"ge chain with equal", "(>= 3 3 2)", core.Bool{V: true}},
		{"ge chain broken", "(>= 1 2)", core.Bool{V: false}},
		{"floats", "(< 1.1 1.2 1.3)", core.Bool{V: true}},
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

func TestComparison_Errors(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"= no args", "(=)", "requires at least 1 argument"},
		{"lt no args", "(<)", "requires at least 1 argument"},
		{"lt string arg", `(< 1 "a")`, "expected number"},
		{"lt single non-number", `(< "a")`, "expected number"},
		{"ge keyword arg", "(>= :a 1)", "expected number"},
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

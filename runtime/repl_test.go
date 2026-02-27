package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestREPL_SimpleExpression(t *testing.T) {
	input := "42\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "42")
}

func TestREPL_MultiLineExpression(t *testing.T) {
	input := "(do\n  1\n  2)\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "2")
}

func TestREPL_ExitCommand(t *testing.T) {
	input := "(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
}

func TestREPL_QuitCommand(t *testing.T) {
	input := ",quit\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
}

func TestREPL_CtrlD(t *testing.T) {
	input := "42"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
}

func TestREPL_ErrorRecovery(t *testing.T) {
	input := "undefined-var\n42\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "Error:")
	assert.Contains(t, output.String(), "42")
}

func TestREPL_EmptyLine(t *testing.T) {
	input := "\n42\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "42")
}

func TestREPL_WelcomeMessage(t *testing.T) {
	input := "(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "go-lispico REPL")
	assert.Contains(t, output.String(), "(exit) or Ctrl+D")
}

func TestREPL_WithArithmetic(t *testing.T) {
	input := "(+ 1 2)\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	bindPlus(eng)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "3")
}

func TestREPL_MultiLineWithArithmetic(t *testing.T) {
	input := "(+ 1\n  2\n  3)\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	bindPlus(eng)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "6")
}

func TestREPL_ExitInMultiLine(t *testing.T) {
	input := "(do\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
}

func TestREPL_EvalWithResult(t *testing.T) {
	input := "\"hello world\"\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "hello world")
}

func TestREPL_DefForm(t *testing.T) {
	input := "(def x 42)\nx\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "42")
}

func TestREPL_FnDefinition(t *testing.T) {
	input := "(defn add [a b] (+ a b))\n(add 2 3)\n(exit)\n"
	output := &bytes.Buffer{}

	eng, err := New(nil)
	require.NoError(t, err)
	bindPlus(eng)

	err = eng.REPL(strings.NewReader(input), output)
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "5")
}

func TestIsBalanced(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"simple list", "(+ 1 2)", true},
		{"unclosed list", "(+ 1", false},
		{"extra closing", "(+ 1 2))", false},
		{"vector", "[1 2 3]", true},
		{"hashmap", "{:a 1 :b 2}", true},
		{"parens in string", "(\"hello (world)\")", true},
		{"nested", "((nested))", true},
		{"mixed brackets", "([{}])", true},
		{"mismatched brackets", "([)]", false},
		{"unclosed string", "(\"hello", false},
		{"escaped quote in string", "(\"hello\\\"world\")", true},
		{"empty list", "()", true},
		{"multiple expressions", "(+ 1) (- 2)", true},
		{"empty string", "", true},
		{"just string", "\"hello\"", true},
		{"brackets in string", "\"[not a vector]\"", true},
		{"unclosed vector", "[1 2", false},
		{"unclosed hashmap", "{:a 1", false},
		{"wrong closing order", "[(])", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBalanced(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsExitCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"exit command", "(exit)", true},
		{"quit command", ",quit", true},
		{"exit with spaces", "  (exit)  ", true},
		{"quit with spaces", "  ,quit  ", true},
		{"not exit", "(+ 1 2)", false},
		{"not exit 2", "exit", false},
		{"not exit 3", "(exitt)", false},
		{"not exit 4", ",quitt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExitCommand(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func bindPlus(e Engine) {
	e.RootEnv().Set("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			sum := int64(0)
			for _, arg := range args {
				if i, ok := arg.(core.Int); ok {
					sum += i.V
				}
			}
			return core.Int{V: sum}, nil
		},
	})
}

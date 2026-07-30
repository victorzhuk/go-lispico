package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type tokenizeCountCase struct {
	name string
	src  string
}

// tokenizeCountCases covers every gold-set fixture, every core/bench_test.go
// reader benchmark source, and the branches nextToken can take that a
// fixture might not exercise: escapes, brackets and quote characters inside
// a string, comments, deep nesting, and quote/quasiquote/unquote/
// unquote-splicing sugar.
func tokenizeCountCases(t *testing.T) []tokenizeCountCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "internal", "goldset", "testdata", "*.lisp"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob goldset fixtures: %v (found %d)", err, len(paths))
	}
	sort.Strings(paths)

	cases := []tokenizeCountCase{
		{"escaped-string", `"line one\nline two\ttab"`},
		{"string-with-brackets-and-quotes", `"[a] {b} \"c\""`},
		{"comments", "; leading comment\n(+ 1 2) ; trailing comment\n; another\n"},
		{"deep-nesting", strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200)},
		{"quote-sugar", "'x `y ~z ~@w"},
		{"empty-input", ""},
		{"whitespace-only", "   \n\t\n  "},
		{"Read_Simple", "(+ 1 (* 2 3))"},
		{"Read_SmallListLiteral", "(1 2 3)"},
		{"Read_SmallVectorLiteral", "[1 2 3]"},
		{"Read_Representative", representativeSource},
	}

	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(p), ".lisp")
		cases = append(cases, tokenizeCountCase{name, string(src)})
	}
	return cases
}

// TestTokenize_CountMatchesLen locks countTokens to Tokenize's real pass: the
// pre-count must equal len(tokens), tokenEOF included, on every input below.
func TestTokenize_CountMatchesLen(t *testing.T) {
	t.Parallel()
	for _, tt := range tokenizeCountCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, err := NewReader(tt.src).countTokens()
			if err != nil {
				t.Fatalf("countTokens: %v", err)
			}
			tokens, err := NewReader(tt.src).Tokenize()
			if err != nil {
				t.Fatalf("Tokenize: %v", err)
			}
			if n != len(tokens) {
				t.Errorf("countTokens = %d, len(Tokenize) = %d", n, len(tokens))
			}
		})
	}
}

// TestTokenize_CountMatchesLen_Flags extends the parity check to the #
// sub-dispatch, the one place a reader flag changes the token count itself:
// with functionRef/readerVector on, #'x and #(...) collapse into a single
// token instead of the two tokenizeCountCases' default-flags reader emits.
func TestTokenize_CountMatchesLen_Flags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		src   string
		flags readerFlags
	}{
		{"function-ref-on", "#'x", readerFlags{functionRef: true}},
		{"reader-vector-on", "#(1 2)", readerFlags{readerVector: true}},
		{"without-bracket-literals", "(f 1 2)", readerFlags{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, err := NewReaderWithFlags(tt.src, tt.flags).countTokens()
			if err != nil {
				t.Fatalf("countTokens: %v", err)
			}
			tokens, err := NewReaderWithFlags(tt.src, tt.flags).Tokenize()
			if err != nil {
				t.Fatalf("Tokenize: %v", err)
			}
			if n != len(tokens) {
				t.Errorf("countTokens = %d, len(Tokenize) = %d", n, len(tokens))
			}
		})
	}
}

// TestTokenize_CountMatchesLen_Unterminated proves countTokens and Tokenize
// agree on the error case too: an unterminated string fails both passes
// rather than the count pass silently swallowing it.
func TestTokenize_CountMatchesLen_Unterminated(t *testing.T) {
	t.Parallel()
	src := `(foo "unterminated`

	if _, err := NewReader(src).countTokens(); err == nil {
		t.Fatal("countTokens: expected error for unterminated string")
	}
	if _, err := NewReader(src).Tokenize(); err == nil {
		t.Fatal("Tokenize: expected error for unterminated string")
	}
}

// TestTokenize_CountingPassDoesNotCorruptEscapeDecoding proves the
// countOnly-guarded scan countTokens runs before Tokenize's real pass
// leaves readString's escape decoding intact: every escaped string below
// goes through both passes (Read -> Tokenize -> countTokens then the real
// scan) and must still decode to its correct value.
func TestTokenize_CountingPassDoesNotCorruptEscapeDecoding(t *testing.T) {
	t.Parallel()
	src := `("a\tb" "c\nd" "e\\f" "g\"h")`
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	list, ok := forms[0].(List)
	if !ok {
		t.Fatalf("expected List, got %T", forms[0])
	}
	items := list.ToSlice()
	want := []string{"a\tb", "c\nd", "e\\f", "g\"h"}
	if len(items) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(items))
	}
	for i, w := range want {
		s, ok := items[i].(String)
		if !ok {
			t.Fatalf("item %d: expected String, got %T", i, items[i])
		}
		if s.V != w {
			t.Errorf("item %d: String.V = %q, want %q", i, s.V, w)
		}
	}
}

package core

import (
	"errors"
	"strings"
	"testing"
	"unsafe"
)

func TestTokenize_Atoms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src      string
		wantType tokenType
		wantVal  string
	}{
		{"42", tokenNumber, "42"},
		{"-7", tokenNumber, "-7"},
		{"3.14", tokenNumber, "3.14"},
		{`"hello"`, tokenString, "hello"},
		{`:model`, tokenKeyword, "model"},
		{`foo`, tokenSymbol, "foo"},
		{`my-fn`, tokenSymbol, "my-fn"},
		{`+`, tokenSymbol, "+"},
		{`nil`, tokenSymbol, "nil"},
		{`true`, tokenSymbol, "true"},
		{`false`, tokenSymbol, "false"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			r := NewReader(tt.src)
			tokens, err := r.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}
			if tokens[0].typ != tt.wantType {
				t.Errorf("token type = %v, want %v", tokens[0].typ, tt.wantType)
			}
			if tokens[0].val != tt.wantVal {
				t.Errorf("token val = %q, want %q", tokens[0].val, tt.wantVal)
			}
		})
	}
}

func TestTokenize_Delimiters(t *testing.T) {
	t.Parallel()
	src := "( ) [ ] { }"
	r := NewReader(src)
	tokens, err := r.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	expected := []tokenType{tokenLParen, tokenRParen, tokenLBracket, tokenRBracket, tokenLBrace, tokenRBrace, tokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("token count = %d, want %d", len(tokens), len(expected))
	}
	for i, tt := range expected {
		if tokens[i].typ != tt {
			t.Errorf("tokens[%d] = %v, want %v", i, tokens[i].typ, tt)
		}
	}
}

func TestTokenize_QuoteSyntax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want tokenType
	}{
		{"'x", tokenQuote},
		{"`x", tokenBacktick},
		{"~x", tokenTilde},
		{"~@x", tokenTildeAt},
	}
	for _, tt := range tests {
		r := NewReader(tt.src)
		tokens, err := r.Tokenize()
		if err != nil {
			t.Errorf("Tokenize(%q) error: %v", tt.src, err)
			continue
		}
		if tokens[0].typ != tt.want {
			t.Errorf("Tokenize(%q)[0] = %v, want %v", tt.src, tokens[0].typ, tt.want)
		}
	}
}

func TestTokenize_StringEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want string
	}{
		{`"hello\nworld"`, "hello\nworld"},
		{`"tab\there"`, "tab\there"},
		{`"quote\"here"`, `quote"here`},
		{`"back\\slash"`, `back\slash`},
	}
	for _, tt := range tests {
		r := NewReader(tt.src)
		tokens, err := r.Tokenize()
		if err != nil {
			t.Errorf("Tokenize(%q) error: %v", tt.src, err)
			continue
		}
		if tokens[0].val != tt.want {
			t.Errorf("string val = %q, want %q", tokens[0].val, tt.want)
		}
	}
}

func TestTokenize_Comments(t *testing.T) {
	t.Parallel()
	src := `; this is a comment
42`
	r := NewReader(src)
	tokens, err := r.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if tokens[0].typ != tokenNumber {
		t.Errorf("first non-comment token = %v, want number", tokens[0].typ)
	}
}

func TestTokenize_CommaAsWhitespace(t *testing.T) {
	t.Parallel()
	src := "1,2,3"
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(forms) != 3 {
		t.Errorf("forms count = %d, want 3 (commas ignored)", len(forms))
	}
}

func TestTokenize_UnterminatedString(t *testing.T) {
	t.Parallel()
	r := NewReader(`"no end`)
	_, err := r.Tokenize()
	if err == nil {
		t.Error("expected error for unterminated string")
	}
}

func TestTokenize_InvalidEscape(t *testing.T) {
	t.Parallel()
	r := NewReader(`"\z"`)
	_, err := r.Tokenize()
	if err == nil {
		t.Error("expected error for invalid escape")
	}
}

func TestTokenize_UnexpectedChar(t *testing.T) {
	t.Parallel()
	r := NewReader("@")
	tokens, err := r.Tokenize()
	if err != nil {
		t.Fatalf("@ should tokenize as tokenAt: %v", err)
	}
	if tokens[0].typ != tokenAt {
		t.Errorf("@ token = %v, want tokenAt", tokens[0].typ)
	}
}

func TestParse_Atoms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want Value
	}{
		{"42", Int{V: 42}},
		{"-7", Int{V: -7}},
		{"3.14", Float{V: 3.14}},
		{`"hello"`, String{V: "hello"}},
		{":key", Keyword{V: "key"}},
		{"nil", Nil{}},
		{"true", Bool{V: true}},
		{"false", Bool{V: false}},
		{"foo", Symbol{V: "foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			v, err := ReadOne(tt.src)
			if err != nil {
				t.Fatalf("ReadOne(%q) error: %v", tt.src, err)
			}
			if !v.Equals(tt.want) {
				t.Errorf("ReadOne(%q) = %v, want %v", tt.src, v, tt.want)
			}
		})
	}
}

func TestParse_List(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("(+ 1 2)")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l, ok := v.(List)
	if !ok {
		t.Fatalf("expected List, got %T", v)
	}
	if l.Len() != 3 {
		t.Errorf("len = %d, want 3", l.Len())
	}
	if !l.At(0).Equals(Symbol{V: "+"}) {
		t.Errorf("head = %v, want +", l.At(0))
	}
}

func TestParse_NestedList(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("(+ (* 2 3) 4)")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l := v.(List)
	inner, ok := l.At(1).(List)
	if !ok {
		t.Fatalf("expected nested List")
	}
	if !inner.At(0).Equals(Symbol{V: "*"}) {
		t.Errorf("inner head = %v, want *", inner.At(0))
	}
}

func TestParse_Vector(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("[1 2 3]")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	vec, ok := v.(Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", v)
	}
	if vec.Len() != 3 {
		t.Errorf("len = %d, want 3", vec.Len())
	}
}

func TestParse_HashMap(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("{:a 1 :b 2}")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	m, ok := v.(*HashMap)
	if !ok {
		t.Fatalf("expected *HashMap, got %T", v)
	}
	if m.Len() != 2 {
		t.Errorf("len = %d, want 2", m.Len())
	}
	val, found := m.Get(Keyword{V: "a"})
	if !found || !val.Equals(Int{V: 1}) {
		t.Errorf(":a = %v found=%v, want 1 true", val, found)
	}
}

func TestParse_QuoteExpansion(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("'foo")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l, ok := v.(List)
	if !ok || l.Len() != 2 {
		t.Fatalf("expected (quote foo), got %v", v)
	}
	if !l.At(0).Equals(Symbol{V: "quote"}) {
		t.Errorf("head = %v, want quote", l.At(0))
	}
}

func TestParse_QuasiquoteExpansion(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("`foo")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l := v.(List)
	if !l.At(0).Equals(Symbol{V: "quasiquote"}) {
		t.Errorf("head = %v, want quasiquote", l.At(0))
	}
}

func TestParse_UnquoteExpansion(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("~x")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l := v.(List)
	if !l.At(0).Equals(Symbol{V: "unquote"}) {
		t.Errorf("head = %v, want unquote", l.At(0))
	}
}

func TestParse_UnquoteSplicing(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("~@xs")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l := v.(List)
	if !l.At(0).Equals(Symbol{V: "unquote-splicing"}) {
		t.Errorf("head = %v, want unquote-splicing", l.At(0))
	}
}

func TestParse_MultipleFormsRead(t *testing.T) {
	t.Parallel()
	forms, err := Read("1 2 3")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(forms) != 3 {
		t.Errorf("len = %d, want 3", len(forms))
	}
}

func TestParse_EmptyList(t *testing.T) {
	t.Parallel()
	v, err := ReadOne("()")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l := v.(List)
	if l.Len() != 0 {
		t.Errorf("empty list len = %d, want 0", l.Len())
	}
}

func TestParse_UnclosedList(t *testing.T) {
	t.Parallel()
	_, err := ReadOne("(+ 1 2")
	if err == nil {
		t.Error("expected error for unclosed list")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	t.Parallel()
	_, err := ReadOne("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseParams_Fixed(t *testing.T) {
	t.Parallel()
	params := NewVector([]Value{Symbol{V: "a"}, Symbol{V: "b"}})
	fixed, variadic, err := parseParams(params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(fixed) != 2 || fixed[0].V != "a" || fixed[1].V != "b" {
		t.Errorf("fixed = %v", fixed)
	}
	if variadic.V != "" {
		t.Errorf("variadic should be empty, got %q", variadic.V)
	}
}

func TestParseParams_Variadic(t *testing.T) {
	t.Parallel()
	params := NewVector([]Value{Symbol{V: "a"}, Symbol{V: "&"}, Symbol{V: "rest"}})
	fixed, variadic, err := parseParams(params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(fixed) != 1 || fixed[0].V != "a" {
		t.Errorf("fixed = %v, want [a]", fixed)
	}
	if variadic.V != "rest" {
		t.Errorf("variadic = %q, want rest", variadic.V)
	}
}

func TestParseParams_NonSymbol(t *testing.T) {
	t.Parallel()
	params := NewVector([]Value{Int{V: 1}})
	_, _, err := parseParams(params)
	if err == nil {
		t.Error("expected error for non-symbol param")
	}
}

func TestParseParams_AmpersandWithoutRest(t *testing.T) {
	t.Parallel()
	params := NewVector([]Value{Symbol{V: "&"}})
	_, _, err := parseParams(params)
	if err == nil {
		t.Error("expected error for & without rest symbol")
	}
}

func TestTokenize_LineCol(t *testing.T) {
	t.Parallel()
	src := "foo\nbar"
	r := NewReader(src)
	tokens, err := r.Tokenize()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tokens[0].line != 1 {
		t.Errorf("first token line = %d, want 1", tokens[0].line)
	}
	if tokens[1].line != 2 {
		t.Errorf("second token line = %d, want 2", tokens[1].line)
	}
}

// TestTokenize_ColBeyond65535 proves token.col carries columns past a
// uint16's range: the layout choice (int32, not uint16) depends on it, since
// reader_limits_test.go's depth stress input alone produces 100,001 columns
// on one line.
func TestTokenize_ColBeyond65535(t *testing.T) {
	t.Parallel()
	src := strings.Repeat(" ", 70000) + "foo"
	r := NewReader(src)
	tokens, err := r.Tokenize()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tokens[0].col != 70001 {
		t.Errorf("col = %d, want 70001", tokens[0].col)
	}
}

func TestToken_Sizeof(t *testing.T) {
	t.Parallel()
	if unsafe.Sizeof(int(0)) != 8 {
		t.Skip("token layout is 64-bit specific; a 32-bit string header packs it differently")
	}
	if got := unsafe.Sizeof(token{}); got != 32 {
		t.Errorf("unsafe.Sizeof(token{}) = %d, want 32", got)
	}
}

// TestReadString_NoEscapeSharesBackingArray proves readString's fast path
// engaged: an escape-free literal's String.V aliases the source string
// instead of a copy, regression-locking the D2 aliasing contract.
func TestReadString_NoEscapeSharesBackingArray(t *testing.T) {
	t.Parallel()
	src := `"hello"`
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	s, ok := forms[0].(String)
	if !ok {
		t.Fatalf("expected String, got %T", forms[0])
	}
	wantData := unsafe.StringData(src[1:6])
	gotData := unsafe.StringData(s.V)
	if gotData != wantData {
		t.Error("String.V does not share backing memory with the source; fast path did not engage")
	}
}

// TestReadString_WithEscapeDecodesCorrectly proves the copy path still
// decodes escapes once one is present in the literal.
func TestReadString_WithEscapeDecodesCorrectly(t *testing.T) {
	t.Parallel()
	forms, err := Read(`"line one\nline two"`)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	s, ok := forms[0].(String)
	if !ok {
		t.Fatalf("expected String, got %T", forms[0])
	}
	if want := "line one\nline two"; s.V != want {
		t.Errorf("String.V = %q, want %q", s.V, want)
	}
}

// TestReadString_EmbeddedNewlineTracksPosition is the regression lock for the
// fast path's line/col bookkeeping: a raw newline inside a string literal
// (no escape, so the zero-copy path engages) must still advance r.line the
// same way the byte-by-byte builder path did, so a later error reports the
// right position.
func TestReadString_EmbeddedNewlineTracksPosition(t *testing.T) {
	t.Parallel()
	_, err := ReadOne("\"a\nb\"\n1.2.3")
	if err == nil {
		t.Fatal("expected error for invalid number")
	}
	var lispicoErr *LispicoError
	if !errors.As(err, &lispicoErr) {
		t.Fatalf("expected *LispicoError, got %T", err)
	}
	if lispicoErr.Line != 3 {
		t.Errorf("expected line 3, got %d", lispicoErr.Line)
	}
}

// TestReader_CountTokensZeroAllocsOnEscapedStrings locks the counting pass's
// core claim: scanning past an escaped string to find its end must not
// allocate, unlike the real pass's readStringEscaped which builds the
// decoded value. This is the invariant whose breakage cost a 53% allocs/op
// regression that no benchmark caught.
func TestReader_CountTokensZeroAllocsOnEscapedStrings(t *testing.T) {
	src := buildEscapeHeavySource(40)
	r := NewReader(src)
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := r.countTokens(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Errorf("countTokens on escape-heavy input allocated %.1f times per run, want 0", allocs)
	}
}

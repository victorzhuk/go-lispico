package core

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const defaultReaderDepth = 1024

type ReaderStats struct {
	Nodes int64
	Bytes int64
}

type tokenType uint8

const (
	tokenLParen      tokenType = iota // (
	tokenRParen                       // )
	tokenLBracket                     // [
	tokenRBracket                     // ]
	tokenLBrace                       // {
	tokenRBrace                       // }
	tokenQuote                        // '
	tokenBacktick                     // `
	tokenTilde                        // ~
	tokenTildeAt                      // ~@
	tokenAt                           // @
	tokenHash                         // #
	tokenFunctionRef                  // #'
	tokenHashParen                    // #(
	tokenString                       // "..."
	tokenNumber                       // 123, 3.14
	tokenSymbol                       // foo, my-fn, +
	tokenKeyword                      // :foo
	tokenEOF
)

type token struct {
	typ  tokenType
	val  string
	line int32
	col  int32
}

// readerFlags gates the reader syntax a Dialect turns on or off. Its zero value
// disables every flag, including bracket literals; NewReader instead applies
// defaultReaderFlags, which reproduces the pre-Dialect reader (bracket literals
// on, #' and #(...) off).
type readerFlags struct {
	bracketLiterals bool // [..]/{..} read as vector/map literals
	functionRef     bool // #'x reads as (function x)
	readerVector    bool // #(...) reads as a vector
}

func defaultReaderFlags() readerFlags {
	return readerFlags{bracketLiterals: true}
}

// Reader tokenizes a Lisp source string.
type Reader struct {
	input string
	pos   int
	line  int
	col   int
	flags readerFlags
	// countOnly is set while countTokens scans for the exact token count.
	// It tells readString's escape path to skip materializing a decoded
	// value — the counting pass only needs to know where the string ends.
	countOnly bool
}

func NewReader(input string) *Reader {
	return NewReaderWithFlags(input, defaultReaderFlags())
}

func NewReaderWithFlags(input string, flags readerFlags) *Reader {
	return &Reader{input: input, line: 1, col: 1, flags: flags}
}

func (r *Reader) next() byte {
	if r.pos >= len(r.input) {
		return 0
	}
	ch := r.input[r.pos]
	r.pos++
	if ch == '\n' {
		r.line++
		r.col = 1
	} else {
		r.col++
	}
	return ch
}

func (r *Reader) peek() byte {
	if r.pos >= len(r.input) {
		return 0
	}
	return r.input[r.pos]
}

func (r *Reader) peekNext() byte {
	if r.pos+1 >= len(r.input) {
		return 0
	}
	return r.input[r.pos+1]
}

func (r *Reader) skipWhitespace() {
	for {
		ch := r.peek()
		if ch == 0 || (!isWhitespace(ch) && ch != ',') {
			break
		}
		r.next()
	}
}

// Tokenize scans the whole input twice: once through countTokens to size the
// result exactly, once for real. Both passes drive the same nextToken, so the
// count can never drift from what the second pass actually emits.
func (r *Reader) Tokenize() ([]token, error) {
	return r.tokenizeInto(nil)
}

// tokenizeInto is Tokenize's real body, sized off buf instead of always
// making a fresh slice: reused capacity from a pooled scratch buffer means
// only a bigger input ever allocates. It always returns the buffer it ended
// up with alongside any error — never a bare nil — so a caller feeding it a
// pool slot's retained buffer never has that capacity discarded just because
// this particular read failed.
func (r *Reader) tokenizeInto(buf []token) ([]token, error) {
	n, err := r.countTokens()
	if err != nil {
		return buf, err
	}

	tokens := buf[:0]
	if cap(tokens) < n {
		tokens = make([]token, 0, n)
	}
	for {
		tok, err := r.nextToken()
		if err != nil {
			return tokens, err
		}
		tokens = append(tokens, tok)
		if tok.typ == tokenEOF {
			return tokens, nil
		}
	}
}

// countTokens runs nextToken to completion to get the exact token count,
// including the terminal EOF token, then rewinds r to where it started.
func (r *Reader) countTokens() (int, error) {
	pos, line, col := r.pos, r.line, r.col
	r.countOnly = true
	defer func() {
		r.pos, r.line, r.col = pos, line, col
		r.countOnly = false
	}()

	n := 0
	for {
		tok, err := r.nextToken()
		if err != nil {
			return 0, err
		}
		n++
		if tok.typ == tokenEOF {
			return n, nil
		}
	}
}

// nextToken scans and returns the next token, skipping whitespace and
// comments first. At end of input it returns a tokenEOF token rather than an
// error.
func (r *Reader) nextToken() (token, error) {
	for {
		r.skipWhitespace()

		if r.pos >= len(r.input) {
			return token{typ: tokenEOF, line: int32(r.line), col: int32(r.col)}, nil
		}

		ch := r.peek()
		if ch == ';' {
			r.readComment()
			continue
		}

		line, col := int32(r.line), int32(r.col)

		switch ch {
		case '(':
			r.next()
			return token{typ: tokenLParen, line: line, col: col}, nil
		case ')':
			r.next()
			return token{typ: tokenRParen, line: line, col: col}, nil
		case '[', ']', '{', '}':
			if !r.flags.bracketLiterals {
				return token{}, NewReadError(fmt.Sprintf("unexpected character: %c", ch), int(line), int(col))
			}
			r.next()
			return token{typ: bracketToken(ch), line: line, col: col}, nil
		case '\'':
			r.next()
			return token{typ: tokenQuote, line: line, col: col}, nil
		case '`':
			r.next()
			return token{typ: tokenBacktick, line: line, col: col}, nil
		case '~':
			r.next()
			if r.peek() == '@' {
				r.next()
				return token{typ: tokenTildeAt, line: line, col: col}, nil
			}
			return token{typ: tokenTilde, line: line, col: col}, nil
		case '@':
			r.next()
			return token{typ: tokenAt, line: line, col: col}, nil
		case '#':
			r.next()
			switch {
			case r.flags.functionRef && r.peek() == '\'':
				r.next()
				return token{typ: tokenFunctionRef, line: line, col: col}, nil
			case r.flags.readerVector && r.peek() == '(':
				r.next()
				return token{typ: tokenHashParen, line: line, col: col}, nil
			default:
				return token{typ: tokenHash, line: line, col: col}, nil
			}
		case '"':
			tok, err := r.readString()
			if err != nil {
				return token{}, err
			}
			tok.line, tok.col = line, col
			return tok, nil
		case ':':
			tok := r.readKeyword()
			tok.line, tok.col = line, col
			return tok, nil
		default:
			if isDigit(ch) || (ch == '-' && isDigit(r.peekNext())) {
				tok := r.readNumber()
				tok.line, tok.col = line, col
				return tok, nil
			}
			if isSymbolStart(ch) {
				tok := r.readSymbol()
				tok.line, tok.col = line, col
				return tok, nil
			}
			return token{}, NewReadError(fmt.Sprintf("unexpected character: %c", ch), r.line, r.col)
		}
	}
}

func (r *Reader) readString() (token, error) {
	r.next() // consume opening "
	start := r.pos

	for {
		ch := r.next()
		switch ch {
		case 0:
			return token{}, NewReadError("unterminated string", r.line, r.col)
		case '"':
			// Zero-copy: no escape in this literal, so the token aliases
			// r.input directly and is retained indefinitely — the same
			// aliasing contract readSymbol/readNumber/readKeyword already
			// carry into stored values (e.g. Lambda.Name).
			return token{typ: tokenString, val: r.input[start : r.pos-1]}, nil
		case '\\':
			return r.readStringEscaped(r.input[start : r.pos-1])
		}
	}
}

// readStringEscaped decodes the remainder of a string literal once an escape
// is found, falling back to a strings.Builder copy. prefix is the unescaped
// run already scanned before the backslash. On the countTokens pass
// (r.countOnly), it still walks the exact same escape/quote boundary this
// scans for a real read, just without writing into buf — the count needs
// only where the string ends, not its decoded value.
func (r *Reader) readStringEscaped(prefix string) (token, error) {
	var buf strings.Builder
	if !r.countOnly {
		buf.WriteString(prefix)
	}

	if err := r.appendEscape(&buf); err != nil {
		return token{}, err
	}

	for {
		ch := r.next()
		if ch == 0 {
			return token{}, NewReadError("unterminated string", r.line, r.col)
		}
		if ch == '"' {
			break
		}
		if ch == '\\' {
			if err := r.appendEscape(&buf); err != nil {
				return token{}, err
			}
			continue
		}
		if !r.countOnly {
			buf.WriteByte(ch)
		}
	}

	return token{typ: tokenString, val: buf.String()}, nil
}

// appendEscape consumes the character following a backslash, validates it,
// and writes its decoded byte to buf unless r.countOnly is set.
func (r *Reader) appendEscape(buf *strings.Builder) error {
	ch := r.next()
	var decoded byte
	switch ch {
	case 'n':
		decoded = '\n'
	case 't':
		decoded = '\t'
	case '"':
		decoded = '"'
	case '\\':
		decoded = '\\'
	case 'r':
		decoded = '\r'
	default:
		return NewReadError(fmt.Sprintf("invalid escape: \\%c", ch), r.line, r.col)
	}
	if !r.countOnly {
		buf.WriteByte(decoded)
	}
	return nil
}

func (r *Reader) readNumber() token {
	start := r.pos

	if r.peek() == '-' {
		r.next()
	}

	for isDigit(r.peek()) || r.peek() == '.' {
		r.next()
	}

	return token{typ: tokenNumber, val: r.input[start:r.pos]}
}

func (r *Reader) readSymbol() token {
	start := r.pos
	for isSymbolChar(r.peek()) {
		r.next()
	}
	return token{typ: tokenSymbol, val: r.input[start:r.pos]}
}

func (r *Reader) readKeyword() token {
	r.next() // consume :
	start := r.pos
	for isSymbolChar(r.peek()) {
		r.next()
	}
	return token{typ: tokenKeyword, val: r.input[start:r.pos]}
}

func bracketToken(ch byte) tokenType {
	switch ch {
	case '[':
		return tokenLBracket
	case ']':
		return tokenRBracket
	case '{':
		return tokenLBrace
	default:
		return tokenRBrace
	}
}

func (r *Reader) readComment() {
	for r.peek() != '\n' && r.peek() != 0 {
		r.next()
	}
}

// Parser converts a token slice into Value trees.
type Parser struct {
	tokens   []token
	pos      int
	maxDepth int
	depth    int
	stats    ReaderStats
	// nodes is a mark/truncate scratch stack for parseList/parseVector/
	// parseReaderVector: each holds its own mark (len(nodes) on entry),
	// appends children as it parses them, then copies out its own span and
	// truncates back to its mark before returning — safe under recursion
	// since every nested call fully unwinds before its caller's next append.
	// nil for a Parser built directly via NewParser/NewParserWithDepth, which
	// grows it from scratch like any other append-based slice.
	nodes []Value
}

func NewParser(tokens []token) *Parser {
	return NewParserWithDepth(tokens, defaultReaderDepth)
}

func NewParserWithDepth(tokens []token, maxDepth int) *Parser {
	if maxDepth <= 0 {
		maxDepth = defaultReaderDepth
	}
	return &Parser{tokens: tokens, maxDepth: maxDepth}
}

// readerScratch bundles a Reader, a Parser, and their token buffer as one
// pooled unit for Dialect.ReadWithMaxDepthStats. The pool is shared across
// every Dialect, so input/flags/maxDepth are per-call fields the checkout
// site must assign itself — Reset only clears the bookkeeping Reset can
// always get right regardless of which dialect or depth limit reads next.
type readerScratch struct {
	reader Reader
	parser Parser
	tokens []token
}

// Reset clears everything a subsequent Read must not observe, retaining
// slice capacity. It does not touch reader.input, reader.flags,
// parser.maxDepth, or parser.tokens — those are set at checkout, once the
// caller knows which source, dialect, and depth limit the read is for.
func (s *readerScratch) Reset() {
	s.reader.pos = 0
	s.reader.line = 1
	s.reader.col = 1
	s.reader.countOnly = false
	s.parser.pos = 0
	s.parser.depth = 0
	s.parser.stats = ReaderStats{}
	s.parser.nodes = s.parser.nodes[:0]
	s.tokens = s.tokens[:0]
}

var readerScratchPool = sync.Pool{
	New: func() any { return &readerScratch{} },
}

// read tokenizes and parses src into s, using flags and maxDepth for this one
// call — the per-call fields Reset cannot assign. Callers must Reset s first;
// read only ever assigns the checkout-time fields, never the bookkeeping ones.
func (s *readerScratch) read(src string, flags readerFlags, maxDepth int) ([]Value, ReaderStats, error) {
	if maxDepth <= 0 {
		maxDepth = defaultReaderDepth
	}
	s.reader.input = src
	s.reader.flags = flags
	s.parser.maxDepth = maxDepth

	tokens, err := s.reader.tokenizeInto(s.tokens)
	s.tokens = tokens
	if err != nil {
		return nil, ReaderStats{}, err
	}
	s.parser.tokens = s.tokens

	var forms []Value
	for s.parser.peek().typ != tokenEOF {
		form, err := s.parser.Parse()
		if err != nil {
			return nil, ReaderStats{}, err
		}
		forms = append(forms, form)
	}
	return forms, s.parser.Stats(), nil
}

func (p *Parser) Stats() ReaderStats { return p.stats }

func (p *Parser) addNode(bytes int64) {
	p.stats.Nodes++
	if bytes > 0 {
		p.stats.Bytes += bytes
	}
}

func (p *Parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{typ: tokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) next() token {
	tok := p.peek()
	p.pos++
	return tok
}

func (p *Parser) expect(tt tokenType) (token, error) {
	tok := p.next()
	if tok.typ != tt {
		return tok, NewReadError(
			fmt.Sprintf("expected %v, got %v", tt, tok.typ),
			int(tok.line), int(tok.col),
		)
	}
	return tok, nil
}

func (p *Parser) Parse() (Value, error) {
	if p.peek().typ == tokenEOF {
		return nil, NewReadError("unexpected EOF", 0, 0)
	}
	return p.parseForm()
}

func (p *Parser) parseForm() (Value, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > p.maxDepth {
		tok := p.peek()
		return nil, &LispicoError{
			Code:    CodeResourceLimit,
			Message: fmt.Sprintf("reader nesting depth limit %d exceeded", p.maxDepth),
			Line:    int(tok.line),
			Col:     int(tok.col),
		}
	}

	tok := p.peek()

	switch tok.typ {
	case tokenEOF:
		return nil, NewReadError("unexpected EOF", int(tok.line), int(tok.col))
	case tokenLParen:
		return p.parseList()
	case tokenLBracket:
		return p.parseVector()
	case tokenLBrace:
		return p.parseHashMap()
	case tokenFunctionRef:
		return p.parseFunctionRef()
	case tokenHashParen:
		return p.parseReaderVector()
	case tokenQuote:
		return p.parseQuote()
	case tokenBacktick:
		return p.parseQuasiquote()
	case tokenTilde:
		return p.parseUnquote()
	case tokenTildeAt:
		return p.parseUnquoteSplicing()
	case tokenString:
		p.next()
		p.addNode(int64(len(tok.val)))
		return String{V: tok.val}, nil
	case tokenNumber:
		p.next()
		p.addNode(0)
		return parseNumber(tok.val, int(tok.line), int(tok.col))
	case tokenSymbol:
		p.next()
		switch tok.val {
		case "nil":
			p.addNode(0)
			return Nil{}, nil
		case "true":
			p.addNode(0)
			return Bool{V: true}, nil
		case "false":
			p.addNode(0)
			return Bool{V: false}, nil
		}
		p.addNode(int64(len(tok.val)))
		return Symbol{V: tok.val}, nil
	case tokenKeyword:
		p.next()
		p.addNode(int64(len(tok.val)))
		return Keyword{V: tok.val}, nil
	default:
		return nil, NewReadError(
			fmt.Sprintf("unexpected token type %v", tok.typ),
			int(tok.line), int(tok.col),
		)
	}
}

func (p *Parser) parseList() (Value, error) {
	p.next() // consume (
	mark := len(p.nodes)

	for p.peek().typ != tokenRParen && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
		p.nodes = append(p.nodes, item)
	}

	if _, err := p.expect(tokenRParen); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items := make([]Value, len(p.nodes)-mark)
	copy(items, p.nodes[mark:])
	p.nodes = p.nodes[:mark]

	p.addNode(0)
	return NewList(items), nil
}

func (p *Parser) parseVector() (Value, error) {
	p.next() // consume [
	mark := len(p.nodes)

	for p.peek().typ != tokenRBracket && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
		p.nodes = append(p.nodes, item)
	}

	if _, err := p.expect(tokenRBracket); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items := make([]Value, len(p.nodes)-mark)
	copy(items, p.nodes[mark:])
	p.nodes = p.nodes[:mark]

	p.addNode(0)
	return NewVector(items), nil
}

func (p *Parser) parseHashMap() (Value, error) {
	p.next() // consume {
	m := NewHashMap()

	for p.peek().typ != tokenRBrace && p.peek().typ != tokenEOF {
		key, err := p.parseForm()
		if err != nil {
			return nil, err
		}

		if p.peek().typ == tokenEOF {
			return nil, NewReadError("map requires even number of forms", 0, 0)
		}

		val, err := p.parseForm()
		if err != nil {
			return nil, err
		}

		err = m.Set(key, val)
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}

	p.addNode(0)
	return m, nil
}

func (p *Parser) parseFunctionRef() (Value, error) {
	p.next() // consume #'
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	p.addNode(int64(len("function")))
	p.addNode(0)
	return NewList([]Value{Symbol{V: "function"}, form}), nil
}

func (p *Parser) parseReaderVector() (Value, error) {
	p.next() // consume #(
	mark := len(p.nodes)

	for p.peek().typ != tokenRParen && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
		p.nodes = append(p.nodes, item)
	}

	if _, err := p.expect(tokenRParen); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items := make([]Value, len(p.nodes)-mark)
	copy(items, p.nodes[mark:])
	p.nodes = p.nodes[:mark]

	p.addNode(0)
	return NewVector(items), nil
}

func (p *Parser) parseQuote() (Value, error) {
	p.next() // consume '
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	p.addNode(int64(len("quote")))
	p.addNode(0)
	return NewList([]Value{Symbol{V: "quote"}, form}), nil
}

func (p *Parser) parseQuasiquote() (Value, error) {
	p.next() // consume `
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	p.addNode(int64(len("quasiquote")))
	p.addNode(0)
	return NewList([]Value{Symbol{V: "quasiquote"}, form}), nil
}

func (p *Parser) parseUnquote() (Value, error) {
	p.next() // consume ~
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	p.addNode(int64(len("unquote")))
	p.addNode(0)
	return NewList([]Value{Symbol{V: "unquote"}, form}), nil
}

func (p *Parser) parseUnquoteSplicing() (Value, error) {
	p.next() // consume ~@
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	p.addNode(int64(len("unquote-splicing")))
	p.addNode(0)
	return NewList([]Value{Symbol{V: "unquote-splicing"}, form}), nil
}

func parseNumber(s string, line, col int) (Value, error) {
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, NewReadError(fmt.Sprintf("invalid number: %s", s), line, col)
		}
		return Float{V: f}, nil
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, NewReadError(fmt.Sprintf("invalid number: %s", s), line, col)
	}
	return BoxInt(i), nil
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isSymbolStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		ch == '_' || ch == '-' || ch == '+' || ch == '*' || ch == '/' ||
		ch == '!' || ch == '?' || ch == '<' || ch == '>' || ch == '=' ||
		ch == '%' || ch == '&' || ch == '^' || ch == '~' || ch == '.'
}

func isSymbolChar(ch byte) bool {
	return isSymbolStart(ch) || isDigit(ch) || ch == '#' || ch == '\''
}

// parseParams splits a parameter vector into fixed params and an optional variadic rest.
// Recognizes `&` as the variadic marker: `[a b & rest]` → fixed=[a,b], variadic=rest.
func parseParams(params Vector) (fixed []Symbol, variadic Symbol, err error) {
	items := params.ToSlice()
	for i := 0; i < len(items); i++ {
		s, ok := items[i].(Symbol)
		if !ok {
			return nil, Symbol{}, fmt.Errorf("param must be symbol, got %T", items[i])
		}
		if s.V == "&" {
			if i+1 >= len(items) {
				return nil, Symbol{}, fmt.Errorf("& requires a following symbol")
			}
			rest, ok := items[i+1].(Symbol)
			if !ok {
				return nil, Symbol{}, fmt.Errorf("variadic param must be symbol")
			}
			return fixed, rest, nil
		}
		fixed = append(fixed, s)
	}
	return fixed, Symbol{}, nil
}

// Read parses all forms from src under the default reader flags and returns them
// as a slice. It is the identity-dialect reader; callers that run a specific
// Dialect read through [Dialect.Read].
func Read(src string) ([]Value, error) {
	return FullDialect().Read(src)
}

// ReadOne parses the first form from src.
func ReadOne(src string) (Value, error) {
	forms, err := Read(src)
	if err != nil {
		return nil, err
	}
	if len(forms) == 0 {
		return nil, NewReadError("empty input", 0, 0)
	}
	return forms[0], nil
}

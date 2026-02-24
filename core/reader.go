package core

import (
	"fmt"
	"strconv"
	"strings"
)

type tokenType int

const (
	tokenLParen   tokenType = iota // (
	tokenRParen                    // )
	tokenLBracket                  // [
	tokenRBracket                  // ]
	tokenLBrace                    // {
	tokenRBrace                    // }
	tokenQuote                     // '
	tokenBacktick                  // `
	tokenTilde                     // ~
	tokenTildeAt                   // ~@
	tokenAt                        // @
	tokenHash                      // #
	tokenString                    // "..."
	tokenNumber                    // 123, 3.14
	tokenSymbol                    // foo, my-fn, +
	tokenKeyword                   // :foo
	tokenEOF
)

type token struct {
	typ  tokenType
	val  string
	line int
	col  int
}

// Reader tokenizes a Lisp source string.
type Reader struct {
	input string
	pos   int
	line  int
	col   int
}

func NewReader(input string) *Reader {
	return &Reader{input: input, line: 1, col: 1}
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

func (r *Reader) Tokenize() ([]token, error) {
	var tokens []token

	for {
		r.skipWhitespace()

		if r.pos >= len(r.input) {
			tokens = append(tokens, token{typ: tokenEOF, line: r.line, col: r.col})
			break
		}

		ch := r.peek()
		line, col := r.line, r.col

		switch ch {
		case '(':
			r.next()
			tokens = append(tokens, token{typ: tokenLParen, line: line, col: col})
		case ')':
			r.next()
			tokens = append(tokens, token{typ: tokenRParen, line: line, col: col})
		case '[':
			r.next()
			tokens = append(tokens, token{typ: tokenLBracket, line: line, col: col})
		case ']':
			r.next()
			tokens = append(tokens, token{typ: tokenRBracket, line: line, col: col})
		case '{':
			r.next()
			tokens = append(tokens, token{typ: tokenLBrace, line: line, col: col})
		case '}':
			r.next()
			tokens = append(tokens, token{typ: tokenRBrace, line: line, col: col})
		case '\'':
			r.next()
			tokens = append(tokens, token{typ: tokenQuote, line: line, col: col})
		case '`':
			r.next()
			tokens = append(tokens, token{typ: tokenBacktick, line: line, col: col})
		case '~':
			r.next()
			if r.peek() == '@' {
				r.next()
				tokens = append(tokens, token{typ: tokenTildeAt, line: line, col: col})
			} else {
				tokens = append(tokens, token{typ: tokenTilde, line: line, col: col})
			}
		case '@':
			r.next()
			tokens = append(tokens, token{typ: tokenAt, line: line, col: col})
		case '#':
			r.next()
			tokens = append(tokens, token{typ: tokenHash, line: line, col: col})
		case ';':
			r.readComment()
		case '"':
			tok, err := r.readString()
			if err != nil {
				return nil, err
			}
			tok.line, tok.col = line, col
			tokens = append(tokens, tok)
		case ':':
			tok := r.readKeyword()
			tok.line, tok.col = line, col
			tokens = append(tokens, tok)
		default:
			if isDigit(ch) || (ch == '-' && isDigit(r.peekNext())) {
				tok := r.readNumber()
				tok.line, tok.col = line, col
				tokens = append(tokens, tok)
			} else if isSymbolStart(ch) {
				tok := r.readSymbol()
				tok.line, tok.col = line, col
				tokens = append(tokens, tok)
			} else {
				return nil, NewReadError(fmt.Sprintf("unexpected character: %c", ch), r.line, r.col)
			}
		}
	}

	return tokens, nil
}

func (r *Reader) readString() (token, error) {
	r.next() // consume opening "
	var buf strings.Builder

	for {
		ch := r.next()
		if ch == 0 {
			return token{}, NewReadError("unterminated string", r.line, r.col)
		}
		if ch == '"' {
			break
		}
		if ch == '\\' {
			ch = r.next()
			switch ch {
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case '"':
				buf.WriteByte('"')
			case '\\':
				buf.WriteByte('\\')
			case 'r':
				buf.WriteByte('\r')
			default:
				return token{}, NewReadError(fmt.Sprintf("invalid escape: \\%c", ch), r.line, r.col)
			}
		} else {
			buf.WriteByte(ch)
		}
	}

	return token{typ: tokenString, val: buf.String()}, nil
}

func (r *Reader) readNumber() token {
	start := r.pos
	hasDot := false

	if r.peek() == '-' {
		r.next()
	}

	for isDigit(r.peek()) || r.peek() == '.' {
		if r.peek() == '.' {
			if hasDot {
				break
			}
			hasDot = true
		}
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

func (r *Reader) readComment() {
	for r.peek() != '\n' && r.peek() != 0 {
		r.next()
	}
}

// Parser converts a token slice into Value trees.
type Parser struct {
	tokens []token
	pos    int
}

func NewParser(tokens []token) *Parser {
	return &Parser{tokens: tokens}
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
			tok.line, tok.col,
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
	tok := p.peek()

	switch tok.typ {
	case tokenEOF:
		return nil, NewReadError("unexpected EOF", tok.line, tok.col)
	case tokenLParen:
		return p.parseList()
	case tokenLBracket:
		return p.parseVector()
	case tokenLBrace:
		return p.parseHashMap()
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
		return String{V: tok.val}, nil
	case tokenNumber:
		p.next()
		return parseNumber(tok.val)
	case tokenSymbol:
		p.next()
		switch tok.val {
		case "nil":
			return Nil{}, nil
		case "true":
			return Bool{V: true}, nil
		case "false":
			return Bool{V: false}, nil
		}
		return Symbol{V: tok.val}, nil
	case tokenKeyword:
		p.next()
		return Keyword{V: tok.val}, nil
	default:
		return nil, NewReadError(
			fmt.Sprintf("unexpected token type %v", tok.typ),
			tok.line, tok.col,
		)
	}
}

func (p *Parser) parseList() (Value, error) {
	start := p.next() // consume (
	_ = start
	var items []Value

	for p.peek().typ != tokenRParen && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if _, err := p.expect(tokenRParen); err != nil {
		return nil, err
	}

	return List{Items: items}, nil
}

func (p *Parser) parseVector() (Value, error) {
	p.next() // consume [
	var items []Value

	for p.peek().typ != tokenRBracket && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if _, err := p.expect(tokenRBracket); err != nil {
		return nil, err
	}

	return Vector{Items: items}, nil
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

		m, err = m.Assoc(key, val)
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}

	return m, nil
}

func (p *Parser) parseQuote() (Value, error) {
	p.next() // consume '
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return List{Items: []Value{Symbol{V: "quote"}, form}}, nil
}

func (p *Parser) parseQuasiquote() (Value, error) {
	p.next() // consume `
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return List{Items: []Value{Symbol{V: "quasiquote"}, form}}, nil
}

func (p *Parser) parseUnquote() (Value, error) {
	p.next() // consume ~
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return List{Items: []Value{Symbol{V: "unquote"}, form}}, nil
}

func (p *Parser) parseUnquoteSplicing() (Value, error) {
	p.next() // consume ~@
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return List{Items: []Value{Symbol{V: "unquote-splicing"}, form}}, nil
}

func parseNumber(s string) (Value, error) {
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, NewReadError(fmt.Sprintf("invalid number: %s", s), 0, 0)
		}
		return Float{V: f}, nil
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, NewReadError(fmt.Sprintf("invalid number: %s", s), 0, 0)
	}
	return Int{V: i}, nil
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
	for i := 0; i < len(params.Items); i++ {
		s, ok := params.Items[i].(Symbol)
		if !ok {
			return nil, Symbol{}, fmt.Errorf("param must be symbol, got %T", params.Items[i])
		}
		if s.V == "&" {
			if i+1 >= len(params.Items) {
				return nil, Symbol{}, fmt.Errorf("& requires a following symbol")
			}
			rest, ok := params.Items[i+1].(Symbol)
			if !ok {
				return nil, Symbol{}, fmt.Errorf("variadic param must be symbol")
			}
			return fixed, rest, nil
		}
		fixed = append(fixed, s)
	}
	return fixed, Symbol{}, nil
}

// Read parses all forms from src and returns them as a slice.
func Read(src string) ([]Value, error) {
	r := NewReader(src)
	tokens, err := r.Tokenize()
	if err != nil {
		return nil, err
	}
	p := NewParser(tokens)
	var forms []Value
	for p.peek().typ != tokenEOF {
		form, err := p.Parse()
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, nil
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

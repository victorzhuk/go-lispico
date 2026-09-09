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
	typ tokenType
	// copied marks a val the scanner decoded into fresh storage instead of
	// aliasing the input. The counting pass reserves that storage up front, so
	// the node built from such a token admits its node unit alone.
	copied bool
	val    string
	line   int32
	col    int32
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
	// budget is nil for a context-free read; a guarded read installs the
	// per-read work budget every advanced byte is charged against.
	budget *readerBudget
	// countOnly is set while countTokens scans for the exact token count.
	// It tells readString's escape path to skip materializing a decoded
	// value — the counting pass only needs to know where the string ends.
	countOnly bool
	// copiedPayload is the decoded storage the counting pass found the second
	// pass will have to copy, reserved before that pass runs.
	copiedPayload int64
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
	if r.budget != nil {
		if err := r.budget.work(1); err != nil {
			// A failed budget ends the scan the way end of input does, so
			// every byte loop above it unwinds at once instead of running
			// the rest of the source uninterrupted.
			r.pos = len(r.input)
			return 0
		}
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
	if err := r.budget.reservePlan(int64(n), r.copiedPayload); err != nil {
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
// including the terminal EOF token, then rewinds r to where it started. It also
// sizes the storage the second pass will need — the token plan and the payload
// escaped strings decode into — and stops at the token that overruns the
// allocation allowance rather than allocating the slice to discover it.
func (r *Reader) countTokens() (int, error) {
	pos, line, col := r.pos, r.line, r.col
	r.countOnly = true
	r.copiedPayload = 0
	defer func() {
		r.pos, r.line, r.col = pos, line, col
		r.countOnly = false
	}()

	headroom := r.budget.allocHeadroom()
	n := 0
	for {
		tok, err := r.nextToken()
		if err != nil {
			return 0, err
		}
		n++
		if err := r.budget.checkPlan(int64(n), r.copiedPayload, headroom); err != nil {
			return 0, err
		}
		if tok.typ == tokenEOF {
			return n, nil
		}
	}
}

// nextToken scans the next token and reports the read's terminal state ahead
// of any syntax error the scan produced: a budget that fails mid-token
// truncates the input, and the unterminated string or unexpected character
// that truncation reports must not mask the failure behind it.
func (r *Reader) nextToken() (token, error) {
	tok, err := r.scanToken()
	if failed := r.budget.err(); failed != nil {
		return token{}, failed
	}
	return tok, err
}

// scanToken scans and returns the next token, skipping whitespace and
// comments first. At end of input it returns a tokenEOF token rather than an
// error.
func (r *Reader) scanToken() (token, error) {
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
	decoded := int64(len(prefix))
	if !r.countOnly {
		// The decoded tail copies one byte per byte the loop below scans, so
		// next already charges it. This bulk prefix copy is the one that
		// advances with no scan behind it.
		if err := r.budget.work(int64(len(prefix))); err != nil {
			return token{}, err
		}
		buf.WriteString(prefix)
	}

	if err := r.appendEscape(&buf); err != nil {
		return token{}, err
	}
	decoded++

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
			decoded++
			continue
		}
		if !r.countOnly {
			buf.WriteByte(ch)
		}
		decoded++
	}

	if r.countOnly {
		r.copiedPayload += decoded
		return token{typ: tokenString}, nil
	}
	return token{typ: tokenString, copied: true, val: buf.String()}, nil
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
	// nodePlan is the one growth schedule nodes follows for a whole read. It
	// tracks the shared high-water mark across nesting, so a nested form's
	// children stack on its parent's rather than restarting the schedule.
	nodePlan growthPlan
	// budget is nil for a context-free read; a guarded read installs the same
	// per-read work budget the scanner charges against.
	budget *readerBudget
}

func NewParser(tokens []token) *Parser {
	return NewParserWithDepth(tokens, defaultReaderDepth)
}

func NewParserWithDepth(tokens []token, maxDepth int) *Parser {
	if maxDepth <= 0 {
		maxDepth = defaultReaderDepth
	}
	return &Parser{tokens: tokens, maxDepth: maxDepth, nodePlan: growthPlan{unit: MeterValueSlotBytes}}
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
	// budget is nil for a context-free read and holds the per-read work
	// budget for a guarded one; the shared scanner and parser run either way.
	budget *readerBudget
}

// Reset clears everything a subsequent Read must not observe, retaining
// slice capacity. It leaves reader.flags and parser.maxDepth alone — those are
// set at checkout, once the caller knows which dialect and depth limit the read
// is for — but drops the previous read's source and token view, so a scratch
// that never reached release pins neither.
func (s *readerScratch) Reset() {
	s.reader.input = ""
	s.reader.pos = 0
	s.reader.line = 1
	s.reader.col = 1
	s.reader.countOnly = false
	s.reader.copiedPayload = 0
	s.reader.budget = nil
	s.parser.tokens = nil
	s.parser.pos = 0
	s.parser.depth = 0
	s.parser.stats = ReaderStats{}
	s.parser.nodes = s.parser.nodes[:0]
	s.parser.nodePlan = growthPlan{unit: MeterValueSlotBytes}
	s.parser.budget = nil
	s.tokens = s.tokens[:0]
	s.budget = nil
}

// retained models the storage the scratch's buffers hold on to between reads,
// on the same units the reader admits them by: a planned token each and a value
// slot per node the parser stacked.
func (s *readerScratch) retained() int64 {
	return int64(cap(s.tokens))*readerTokenUnitBytes + ValueSlotsBytes(cap(s.parser.nodes))
}

// release readies s for the pool and reports whether it may go back in. It
// clears every reference this read or an earlier one left behind — the source
// string, the token vals, the node stack, and the budget the guarded entry
// point installed — so a pooled entry pins none of them.
//
// Two cases refuse the pool outright rather than clear. A scratch retaining
// more storage than ceiling, the allowance the read ran under, is dropped: the
// next read off this entry could be a far smaller one, and it must inherit
// neither the buffers nor the allowance this one was given. A read that ended
// terminally is dropped too — traversing input-sized storage to clear it is
// exactly the work a cancelled, expired or exhausted read must stop doing.
func (s *readerScratch) release(ceiling int64) bool {
	if s.budget.err() != nil || s.retained() > ceiling {
		return false
	}

	s.reader.input = ""
	s.parser.tokens = nil
	if !clearSlots(s.budget, s.tokens[:cap(s.tokens)]) {
		return false
	}
	if !clearSlots(s.budget, s.parser.nodes[:cap(s.parser.nodes)]) {
		return false
	}
	s.tokens = s.tokens[:0]
	s.parser.nodes = s.parser.nodes[:0]

	if err := s.budget.checkpoint(); err != nil {
		return false
	}
	s.reader.budget = nil
	s.parser.budget = nil
	s.budget = nil
	return true
}

// clearSlots zeroes buf in bounded batches, charging one work unit per cleared
// slot, and reports whether it got through the whole buffer: a batch that turns
// the read terminal leaves the rest to the garbage collector instead.
func clearSlots[T any](b *readerBudget, buf []T) bool {
	for len(buf) > 0 {
		n := min(len(buf), readerClearBatch)
		clear(buf[:n])
		if err := b.work(int64(n)); err != nil {
			return false
		}
		buf = buf[n:]
	}
	return true
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
	s.reader.budget = s.budget
	s.parser.maxDepth = maxDepth
	s.parser.budget = s.budget

	tokens, err := s.reader.tokenizeInto(s.tokens)
	s.tokens = tokens
	if err != nil {
		return nil, ReaderStats{}, s.budget.settle(err)
	}
	s.parser.tokens = s.tokens

	formPlan := growthPlan{unit: MeterValueSlotBytes}
	var forms []Value
	for s.parser.peek().typ != tokenEOF {
		form, err := s.parser.Parse()
		if err != nil {
			return nil, ReaderStats{}, s.budget.settle(err)
		}
		if err := formPlan.admit(s.budget, int64(len(forms)+1)); err != nil {
			return nil, ReaderStats{}, s.budget.settle(err)
		}
		forms = append(forms, form)
	}
	if err := s.budget.checkpoint(); err != nil {
		return nil, ReaderStats{}, err
	}
	return forms, s.parser.Stats(), nil
}

func (p *Parser) Stats() ReaderStats { return p.stats }

// addNode accounts for one output node before it is built: the node unit plus
// the payload it carries. Admission comes first, so a refusal never leaves
// ReaderStats describing a node the read never produced.
func (p *Parser) addNode(bytes int64) error {
	return p.admitNode(bytes, bytes)
}

// admitNode admits one node unit plus payload — the storage the ledger has not
// seen yet, which is zero for a decoded string the counting pass already
// reserved — while bytes stays the payload the output holds and the stats
// report.
func (p *Parser) admitNode(bytes, payload int64) error {
	if err := p.budget.admitOutputNode(payload); err != nil {
		return err
	}
	if err := p.budget.work(1); err != nil {
		return err
	}
	p.stats.Nodes++
	if bytes > 0 {
		p.stats.Bytes += bytes
	}
	return nil
}

// addChild admits the workspace slot a collection's next child occupies on the
// shared node scratch, then stacks it there.
func (p *Parser) addChild(item Value) error {
	if err := p.nodePlan.admit(p.budget, int64(len(p.nodes)+1)); err != nil {
		return err
	}
	p.nodes = append(p.nodes, item)
	return nil
}

// takeChildren copies the children stacked since mark into their own storage,
// admitted first and copied in bounded batches, then unwinds the scratch back
// to mark.
func (p *Parser) takeChildren(mark int) ([]Value, error) {
	n := len(p.nodes) - mark
	if err := p.budget.admitSlots(n); err != nil {
		return nil, err
	}
	items := make([]Value, n)
	for i := 0; i < n; i += readerLinkBatch {
		end := min(i+readerLinkBatch, n)
		if err := p.budget.work(int64(end - i)); err != nil {
			return nil, err
		}
		copy(items[i:end], p.nodes[mark+i:mark+end])
	}
	p.nodes = p.nodes[:mark]
	return items, nil
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
		payload := int64(len(tok.val))
		admit := payload
		if tok.copied {
			admit = 0
		}
		if err := p.admitNode(payload, admit); err != nil {
			return nil, err
		}
		return String{V: tok.val}, nil
	case tokenNumber:
		p.next()
		if err := p.addNode(0); err != nil {
			return nil, err
		}
		return p.parseNumberToken(tok)
	case tokenSymbol:
		p.next()
		switch tok.val {
		case "nil":
			if err := p.addNode(0); err != nil {
				return nil, err
			}
			return Nil{}, nil
		case "true":
			if err := p.addNode(0); err != nil {
				return nil, err
			}
			return Bool{V: true}, nil
		case "false":
			if err := p.addNode(0); err != nil {
				return nil, err
			}
			return Bool{V: false}, nil
		}
		if err := p.addNode(int64(len(tok.val))); err != nil {
			return nil, err
		}
		return Symbol{V: tok.val}, nil
	case tokenKeyword:
		p.next()
		if err := p.addNode(int64(len(tok.val))); err != nil {
			return nil, err
		}
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
		if err == nil {
			err = p.addChild(item)
		}
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
	}

	if _, err := p.expect(tokenRParen); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items, err := p.takeChildren(mark)
	if err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	if err := p.addNode(0); err != nil {
		return nil, err
	}
	return p.buildList(items)
}

// buildList finalizes a parsed list, admitting the shared-tail cells a list
// past listFlatThreshold links before any of them is allocated. At or below
// the threshold the list stays flat and links none.
func (p *Parser) buildList(items []Value) (Value, error) {
	if len(items) <= listFlatThreshold {
		return NewList(items), nil
	}
	if err := p.budget.admitListCells(len(items)); err != nil {
		return nil, err
	}
	chain, err := newGuardedListChain(items, p.budget)
	if err != nil {
		return nil, err
	}
	return List{shared: chain}, nil
}

func (p *Parser) parseVector() (Value, error) {
	p.next() // consume [
	mark := len(p.nodes)

	for p.peek().typ != tokenRBracket && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err == nil {
			err = p.addChild(item)
		}
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
	}

	if _, err := p.expect(tokenRBracket); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items, err := p.takeChildren(mark)
	if err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	if err := p.addNode(0); err != nil {
		return nil, err
	}
	return NewVector(items), nil
}

func (p *Parser) parseHashMap() (Value, error) {
	p.next() // consume {
	m := NewHashMap()
	entryPlan := growthPlan{unit: MeterHashMapEntryBytes}

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

		if err := p.mapSet(m, &entryPlan, key, val); err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}

	if err := p.addNode(0); err != nil {
		return nil, err
	}
	return m, nil
}

// mapSet inserts one literal pair into a map the read still owns exclusively,
// admitting the storage each insert claims before claiming it. Past
// hashMapSmallLimit it builds straight into the trie: the Go-map branch of Set
// hashes and rehashes a whole bucket table with no point at which the read can
// be interrupted or refused.
func (p *Parser) mapSet(m *HashMap, plan *growthPlan, key, val Value) error {
	hk, err := toHashKey(key)
	if err != nil {
		return err
	}
	if err := p.budget.work(int64(len(hk.str)) + 1); err != nil {
		return err
	}
	e := entry{hk: hk, k: key, v: val}

	if m.large != nil {
		root, added, err := m.large.root.assocGuarded(p.budget, e, hashOfKey(hk), 0)
		if err != nil {
			return err
		}
		m.large.root = root
		if added {
			m.large.count++
		}
		return nil
	}

	i, found := m.find(hk)
	if err := p.budget.work(int64(i) + 1); err != nil {
		return err
	}
	if found {
		m.entries[i] = e
		return nil
	}
	if len(m.entries) >= hashMapSmallLimit {
		root, err := newGuardedTrie(m.entries, e, p.budget)
		if err != nil {
			return err
		}
		m.large = &largeMap{root: root, count: len(m.entries) + 1}
		m.entries = nil
		return nil
	}
	if err := plan.admit(p.budget, int64(len(m.entries)+1)); err != nil {
		return err
	}
	m.entries = append(m.entries, entry{})
	copy(m.entries[i+1:], m.entries[i:len(m.entries)-1])
	m.entries[i] = e
	return nil
}

// wrapForm builds the (sym form) list a reader macro expands to, accounting
// for both the generated symbol node and the list node holding it.
func (p *Parser) wrapForm(sym string, form Value) (Value, error) {
	if err := p.addNode(int64(len(sym))); err != nil {
		return nil, err
	}
	if err := p.addNode(0); err != nil {
		return nil, err
	}
	if err := p.budget.admitSlots(2); err != nil {
		return nil, err
	}
	return NewList([]Value{Symbol{V: sym}, form}), nil
}

func (p *Parser) parseFunctionRef() (Value, error) {
	p.next() // consume #'
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return p.wrapForm("function", form)
}

func (p *Parser) parseReaderVector() (Value, error) {
	p.next() // consume #(
	mark := len(p.nodes)

	for p.peek().typ != tokenRParen && p.peek().typ != tokenEOF {
		item, err := p.parseForm()
		if err == nil {
			err = p.addChild(item)
		}
		if err != nil {
			p.nodes = p.nodes[:mark]
			return nil, err
		}
	}

	if _, err := p.expect(tokenRParen); err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	items, err := p.takeChildren(mark)
	if err != nil {
		p.nodes = p.nodes[:mark]
		return nil, err
	}

	if err := p.addNode(0); err != nil {
		return nil, err
	}
	return NewVector(items), nil
}

func (p *Parser) parseQuote() (Value, error) {
	p.next() // consume '
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return p.wrapForm("quote", form)
}

func (p *Parser) parseQuasiquote() (Value, error) {
	p.next() // consume `
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return p.wrapForm("quasiquote", form)
}

func (p *Parser) parseUnquote() (Value, error) {
	p.next() // consume ~
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return p.wrapForm("unquote", form)
}

func (p *Parser) parseUnquoteSplicing() (Value, error) {
	p.next() // consume ~@
	form, err := p.parseForm()
	if err != nil {
		return nil, err
	}
	return p.wrapForm("unquote-splicing", form)
}

// parseNumberToken admits the token into strconv before converting it. The
// conversion is the one span of reader work that cannot be interrupted, so it
// is charged and bounded up front and the terminal state is re-read the moment
// it returns.
func (p *Parser) parseNumberToken(tok token) (Value, error) {
	if err := p.budget.admitConversion(int64(len(tok.val))); err != nil {
		return nil, err
	}
	if err := p.budget.admitConversionStorage(int64(len(tok.val))); err != nil {
		return nil, err
	}
	shown := tok.val
	if p.budget != nil {
		shown = boundedSource(tok.val)
	}
	v, err := convertNumber(tok.val, int(tok.line), int(tok.col), shown)
	if err != nil {
		return nil, err
	}
	if err := p.budget.checkpoint(); err != nil {
		return nil, err
	}
	return v, nil
}

// convertNumber converts s, rendering a failure against shown rather than s:
// the guarded reader bounds how much of an arbitrarily long token a diagnostic
// carries, while the context-free reader keeps rendering the whole token.
// strconv's own error renderer is never called — it repeats the full input.
func convertNumber(s string, line, col int, shown string) (Value, error) {
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, NewReadError("invalid number: "+shown, line, col)
		}
		return Float{V: f}, nil
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, NewReadError("invalid number: "+shown, line, col)
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

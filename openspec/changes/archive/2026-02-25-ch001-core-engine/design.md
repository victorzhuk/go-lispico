# Design Document: Core Engine Foundation

**Change ID:** 001-core-engine  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Type System

### Value Interface

```go
type Value interface {
    Type() Keyword
    String() string
    Equals(other Value) bool
}
```

### Concrete Types

| Type | Go Struct | Lisp Literal | Notes |
|------|-----------|--------------|-------|
| Nil | `Nil{}` | `nil` | Singleton, only nil is falsy besides false |
| Bool | `Bool{V bool}` | `true`, `false` | Only nil and false are falsy |
| Int | `Int{V int64}` | `42`, `-7` | Fixed 64-bit signed |
| Float | `Float{V float64}` | `3.14`, `-0.5` | IEEE 754 double |
| String | `String{V string}` | `"hello"` | UTF-8, immutable |
| Symbol | `Symbol{V string}` | `foo`, `my-fn` | Resolves in env |
| Keyword | `Keyword{V string}` | `:model`, `:agent` | Self-evaluating |
| List | `List{Items []Value}` | `(1 2 3)` | Linked list (slice impl) |
| Vector | `Vector{Items []Value}` | `[1 2 3]` | Random-access sequence |
| HashMap | `*HashMap` (private fields; use `NewHashMap`) | `{:a 1 :b 2}` | Immutable associative; keys must be comparable |
| GoFunc | `GoFunc{Name string, Fn func(ctx, eval, args, env) (Value, error)}` | `#<builtin>` | Native Go function |
| Lambda | `Lambda{Params []Symbol, Variadic Symbol, Body []Value, Env *Env}` | `#<fn>` | User closure |
| Macro | `Macro{Params []Symbol, Variadic Symbol, Body []Value, Env *Env}` | `#<macro>` | Syntax transformer |

### Type Implementations

```go
// Nil
type Nil struct{}
func (n Nil) Type() Keyword    { return Keyword{V: "nil"} }
func (n Nil) String() string   { return "nil" }
func (n Nil) Equals(o Value) bool { _, ok := o.(Nil); return ok }

// Bool
type Bool struct{ V bool }
func (b Bool) Type() Keyword   { return Keyword{V: "bool"} }
func (b Bool) String() string  { return fmt.Sprintf("%t", b.V) }
func (b Bool) Equals(o Value) bool {
    if v, ok := o.(Bool); ok { return b.V == v.V }
    return false
}

// Int
type Int struct{ V int64 }
func (i Int) Type() Keyword    { return Keyword{V: "int"} }
func (i Int) String() string   { return strconv.FormatInt(i.V, 10) }
func (i Int) Equals(o Value) bool {
    if v, ok := o.(Int); ok { return i.V == v.V }
    return false
}

// Float
type Float struct{ V float64 }
func (f Float) Type() Keyword  { return Keyword{V: "float"} }
func (f Float) String() string { return strconv.FormatFloat(f.V, 'f', -1, 64) }
func (f Float) Equals(o Value) bool {
    if v, ok := o.(Float); ok { return f.V == v.V }
    return false
}

// String
type String struct{ V string }
func (s String) Type() Keyword { return Keyword{V: "string"} }
func (s String) String() string { 
    return fmt.Sprintf("%q", s.V) 
}
func (s String) Equals(o Value) bool {
    if v, ok := o.(String); ok { return s.V == v.V }
    return false
}

// Symbol
type Symbol struct{ V string }
func (s Symbol) Type() Keyword { return Keyword{V: "symbol"} }
func (s Symbol) String() string { return s.V }
func (s Symbol) Equals(o Value) bool {
    if v, ok := o.(Symbol); ok { return s.V == v.V }
    return false
}

// Keyword
type Keyword struct{ V string }
func (k Keyword) Type() Keyword { return Keyword{V: "keyword"} }
func (k Keyword) String() string { return fmt.Sprintf(":%s", k.V) }
func (k Keyword) Equals(o Value) bool {
    if v, ok := o.(Keyword); ok { return k.V == v.V }
    return false
}

// List
type List struct{ Items []Value }
func (l List) Type() Keyword   { return Keyword{V: "list"} }
func (l List) String() string {
    var parts []string
    for _, item := range l.Items {
        parts = append(parts, item.String())
    }
    return fmt.Sprintf("(%s)", strings.Join(parts, " "))
}
func (l List) Equals(o Value) bool {
    v, ok := o.(List)
    if !ok || len(l.Items) != len(v.Items) { return false }
    for i := range l.Items {
        if !l.Items[i].Equals(v.Items[i]) { return false }
    }
    return true
}

// Vector
type Vector struct{ Items []Value }
func (v Vector) Type() Keyword { return Keyword{V: "vector"} }
func (v Vector) String() string {
    var parts []string
    for _, item := range v.Items {
        parts = append(parts, item.String())
    }
    return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}
func (v Vector) Equals(o Value) bool {
    other, ok := o.(Vector)
    if !ok || len(v.Items) != len(other.Items) { return false }
    for i := range v.Items {
        if !v.Items[i].Equals(other.Items[i]) { return false }
    }
    return true
}

// hashKey is the internal key type for HashMap — only comparable Value types are allowed.
// Disambiguates equal string representations across types (e.g. symbol "true" vs bool true).
type hashKey struct {
	typ string
	val string
}

// toHashKey converts a comparable Value to a hashKey.
// Returns error for non-comparable types: List, Vector, HashMap, GoFunc, Lambda, Macro.
func toHashKey(v Value) (hashKey, error) {
	switch val := v.(type) {
	case Nil:
		return hashKey{"nil", ""}, nil
	case Bool:
		return hashKey{"bool", fmt.Sprintf("%t", val.V)}, nil
	case Int:
		return hashKey{"int", strconv.FormatInt(val.V, 10)}, nil
	case Float:
		return hashKey{"float", strconv.FormatFloat(val.V, 'f', -1, 64)}, nil
	case String:
		return hashKey{"string", val.V}, nil
	case Symbol:
		return hashKey{"symbol", val.V}, nil
	case Keyword:
		return hashKey{"keyword", val.V}, nil
	default:
		return hashKey{}, fmt.Errorf("unhashable type: %T", v)
	}
}

// HashMap - immutable associative map; keys must be comparable (Nil, Bool, Int, Float, String, Symbol, Keyword).
type HashMap struct {
	m    map[hashKey]Value // internal storage
	keys map[hashKey]Value // original key Values for display/iteration
}

func NewHashMap() *HashMap {
	return &HashMap{
		m:    make(map[hashKey]Value),
		keys: make(map[hashKey]Value),
	}
}
func (h *HashMap) Type() Keyword { return Keyword{V: "map"} }
func (h *HashMap) String() string {
	var parts []string
	for hk, v := range h.m {
		parts = append(parts, fmt.Sprintf("%s %s", h.keys[hk].String(), v.String()))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, " "))
}
func (h *HashMap) Equals(o Value) bool {
	v, ok := o.(*HashMap)
	if !ok || len(h.m) != len(v.m) { return false }
	for hk, val := range h.m {
		if other, ok := v.m[hk]; !ok || !val.Equals(other) {
			return false
		}
	}
	return true
}
func (h *HashMap) Assoc(key, val Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil { return nil, err }
	newMap := NewHashMap()
	for k, v := range h.m {
		newMap.m[k] = v
		newMap.keys[k] = h.keys[k]
	}
	newMap.m[hk] = val
	newMap.keys[hk] = key
	return newMap, nil
}
func (h *HashMap) Dissoc(key Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil { return nil, err }
	newMap := NewHashMap()
	for k, v := range h.m {
		if k != hk {
			newMap.m[k] = v
			newMap.keys[k] = h.keys[k]
		}
	}
	return newMap, nil
}
func (h *HashMap) Get(key Value) (Value, bool) {
	hk, err := toHashKey(key)
	if err != nil { return nil, false }
	v, ok := h.m[hk]
	return v, ok
}
func (h *HashMap) Len() int { return len(h.m) }

// Evaluator allows GoFunc implementations to recursively evaluate Lisp forms.
type Evaluator interface {
    Eval(ctx context.Context, form Value, env *Env) (Value, error)
}

// Compiler compiles a form to bytecode. Implemented by core/compiler package.
// The tree-walker Evaluator ignores this; the VM backend uses it.
// vm.Chunk is defined in core/vm package (ch008).
type Compiler interface {
    Compile(form Value) (*vm.Chunk, error)
    CompileAll(forms []Value) ([]*vm.Chunk, error)
}

// GoFunc - native Go function.
// Receives context, the evaluator (to call Lambdas), args, and current env.
type GoFunc struct {
    Name string
    Fn   func(ctx context.Context, eval Evaluator, args []Value, env *Env) (Value, error)
}
func (g GoFunc) Type() Keyword { return Keyword{V: "fn"} }
func (g GoFunc) String() string { return fmt.Sprintf("#<builtin:%s>", g.Name) }
func (g GoFunc) Equals(o Value) bool {
    v, ok := o.(GoFunc)
    return ok && g.Name == v.Name
}

// Lambda - user-defined function
type Lambda struct {
    Params   []Symbol
    Variadic Symbol   // non-empty V means variadic; bound as list to rest args after Params
    Body     []Value
    Env      *Env
    Name     string // optional, for recursion
}
func (l Lambda) Type() Keyword { return Keyword{V: "fn"} }
func (l Lambda) String() string { 
    if l.Name != "" { return fmt.Sprintf("#<fn:%s>", l.Name) }
    return "#<fn>" 
}
func (l Lambda) Equals(o Value) bool { return false }

// Macro - syntax transformer
type Macro struct {
    Params   []Symbol
    Variadic Symbol
    Body     []Value
    Env      *Env
    Name     string
}
func (m Macro) Type() Keyword { return Keyword{V: "macro"} }
func (m Macro) String() string { 
    if m.Name != "" { return fmt.Sprintf("#<macro:%s>", m.Name) }
    return "#<macro>" 
}
func (m Macro) Equals(o Value) bool { return false }

// Closure note (ch008 forward reference):
// For tree-walker: Lambda captures lexical env as *Env chain.
// For VM: Closure wraps *vm.Chunk + *Env (same Value interface).
// The bytecode compiler (ch008) emits a separate Closure struct; the tree-walker
// uses Lambda directly. Both satisfy the Value interface.
```

### Go Interop Helpers

```go
func FromGoValue(v any) (Value, error) {
    switch val := v.(type) {
    case nil:
        return Nil{}, nil
    case bool:
        return Bool{V: val}, nil
    case int:
        return Int{V: int64(val)}, nil
    case int64:
        return Int{V: val}, nil
    case float64:
        return Float{V: val}, nil
    case string:
        return String{V: val}, nil
    case []any:
        items := make([]Value, len(val))
        for i, item := range val {
            v, err := FromGoValue(item)
            if err != nil { return nil, err }
            items[i] = v
        }
        return Vector{Items: items}, nil
    case map[string]any:
        m := NewHashMap()
        for k, v := range val {
            key := Keyword{V: k}
            value, err := FromGoValue(v)
            if err != nil { return nil, err }
            m, err = m.Assoc(key, value)
            if err != nil { return nil, err }
        }
        return m, nil
    default:
        return nil, fmt.Errorf("unsupported Go type: %T", v)
    }
}

func ToGoValue(v Value) (any, error) {
    switch val := v.(type) {
    case Nil:
        return nil, nil
    case Bool:
        return val.V, nil
    case Int:
        return val.V, nil
    case Float:
        return val.V, nil
    case String:
        return val.V, nil
    case Keyword:
        return val.V, nil
    case Symbol:
        return val.V, nil
    case Vector:
        result := make([]any, len(val.Items))
        for i, item := range val.Items {
            v, err := ToGoValue(item)
            if err != nil { return nil, err }
            result[i] = v
        }
        return result, nil
    case *HashMap:
        result := make(map[string]any)
        for hk, v := range val.m {
            origKey := val.keys[hk]
            keyVal, err := ToGoValue(origKey)
            if err != nil { return nil, err }
            keyStr, ok := keyVal.(string)
            if !ok {
                return nil, fmt.Errorf("map key must convert to string, got %T", keyVal)
            }
            value, err := ToGoValue(v)
            if err != nil { return nil, err }
            result[keyStr] = value
        }
        return result, nil
    default:
        return nil, fmt.Errorf("unsupported Lisp type: %T", v)
    }
}
```

---

## 2. Environment

### Design

Thread-safe lexical scope with parent reference:

```go
type Env struct {
    mu     sync.RWMutex
    parent *Env
    vars   map[string]Value
    eval   Evaluator
}

func NewEnv(parent *Env) *Env {
    return &Env{
        parent: parent,
        vars:   make(map[string]Value),
    }
}

func (e *Env) Set(name string, val Value) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.vars[name] = val
}

func (e *Env) Get(name string) (Value, bool) {
    e.mu.RLock()
    defer e.mu.RUnlock()

    if val, ok := e.vars[name]; ok {
        return val, true
    }

    if e.parent != nil {
        return e.parent.Get(name)
    }

    return nil, false
}

func (e *Env) Find(name string) (*Env, bool) {
    e.mu.RLock()
    defer e.mu.RUnlock()
    
    if _, ok := e.vars[name]; ok {
        return e, true
    }
    
    if e.parent != nil {
        return e.parent.Find(name)
    }
    
    return nil, false
}
```

### Child Scope Creation

```go
func (e *Env) Child() *Env {
    return NewEnv(e)
}

func (e *Env) ChildVariadic(params []Symbol, args []Value, variadic Symbol) (*Env, error) {
    child := e.Child()
    
    if variadic.V != "" {
        if len(args) < len(params) {
            return nil, fmt.Errorf("expected at least %d args, got %d", len(params), len(args))
        }
        for i, param := range params {
            child.Set(param.V, args[i])
        }
        remaining := List{Items: args[len(params):]}
        child.Set(variadic.V, remaining)
    } else {
        if len(args) != len(params) {
            return nil, fmt.Errorf("expected %d args, got %d", len(params), len(args))
        }
        for i, param := range params {
            child.Set(param.V, args[i])
        }
    }
    
    return child, nil
}

// Evaluator returns the engine evaluator for use by plugins needing recursive eval.
func (e *Env) Evaluator() Evaluator {
    return e.eval
}
```

---

## 3. Reader

### Token Types

```go
type tokenType int

const (
    tokenLParen tokenType = iota    // (
    tokenRParen                     // )
    tokenLBracket                   // [
    tokenRBracket                   // ]
    tokenLBrace                     // {
    tokenRBrace                     // }
    tokenQuote                      // '
    tokenBacktick                   // `
    tokenTilde                      // ~
    tokenTildeAt                    // ~@
    tokenAt                         // @
    tokenHash                       // #
    tokenString                     // "..."
    tokenNumber                     // 123, 3.14
    tokenSymbol                     // foo, my-fn, +
    tokenKeyword                    // :foo
    tokenComment                    // ; ...
    tokenEOF
)

type token struct {
    typ   tokenType
    val   string
    line  int
    col   int
}
```

### Tokenizer

```go
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
        case "'":
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
            if err != nil { return nil, err }
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
                return nil, fmt.Errorf("unexpected character: %c at %d:%d", ch, r.line, r.col)
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
            return token{}, fmt.Errorf("unterminated string at %d:%d", r.line, r.col)
        }
        if ch == '"' {
            break
        }
        if ch == '\\' {
            ch = r.next()
            switch ch {
            case 'n': buf.WriteByte('\n')
            case 't': buf.WriteByte('\t')
            case '"': buf.WriteByte('"')
            case '\\': buf.WriteByte('\\')
            default:
                return token{}, fmt.Errorf("invalid escape sequence: \\%c", ch)
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
            if hasDot { break }
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
```

### Parser

```go
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
        return tok, fmt.Errorf("expected %v, got %v at %d:%d", tt, tok.typ, tok.line, tok.col)
    }
    return tok, nil
}

func (p *Parser) Parse() (Value, error) {
    return p.parseForm()
}

func (p *Parser) parseForm() (Value, error) {
    tok := p.peek()
    
    switch tok.typ {
    case tokenEOF:
        return nil, fmt.Errorf("unexpected EOF")
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
        return Symbol{V: tok.val}, nil
    case tokenKeyword:
        p.next()
        return Keyword{V: tok.val}, nil
    default:
        return nil, fmt.Errorf("unexpected token: %v at %d:%d", tok.typ, tok.line, tok.col)
    }
}

func (p *Parser) parseList() (Value, error) {
    p.next() // consume (
    var items []Value
    
    for p.peek().typ != tokenRParen && p.peek().typ != tokenEOF {
        item, err := p.parseForm()
        if err != nil { return nil, err }
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
        if err != nil { return nil, err }
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
        if err != nil { return nil, err }

        val, err := p.parseForm()
        if err != nil { return nil, err }

        m, err = m.Assoc(key, val)
        if err != nil { return nil, err }
    }

    if _, err := p.expect(tokenRBrace); err != nil {
        return nil, err
    }

    return m, nil
}

func (p *Parser) parseQuote() (Value, error) {
    p.next() // consume '
    form, err := p.parseForm()
    if err != nil { return nil, err }
    return List{Items: []Value{Symbol{V: "quote"}, form}}, nil
}

func (p *Parser) parseQuasiquote() (Value, error) {
    p.next() // consume `
    form, err := p.parseForm()
    if err != nil { return nil, err }
    return List{Items: []Value{Symbol{V: "quasiquote"}, form}}, nil
}

func (p *Parser) parseUnquote() (Value, error) {
    p.next() // consume ~
    form, err := p.parseForm()
    if err != nil { return nil, err }
    return List{Items: []Value{Symbol{V: "unquote"}, form}}, nil
}

func (p *Parser) parseUnquoteSplicing() (Value, error) {
    p.next() // consume ~@
    form, err := p.parseForm()
    if err != nil { return nil, err }
    return List{Items: []Value{Symbol{V: "unquote-splicing"}, form}}, nil
}

func parseNumber(s string) (Value, error) {
    if strings.Contains(s, ".") {
        f, err := strconv.ParseFloat(s, 64)
        if err != nil { return nil, err }
        return Float{V: f}, nil
    }
    i, err := strconv.ParseInt(s, 10, 64)
    if err != nil { return nil, err }
    return Int{V: i}, nil
}
```

### Helper Functions

```go
func isWhitespace(ch byte) bool {
    return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isDigit(ch byte) bool {
    return ch >= '0' && ch <= '9'
}

func isSymbolStart(ch byte) bool {
    return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || 
           ch == '_' || ch == '-' || ch == '+' || ch == '*' || ch == '/' ||
           ch == '!' || ch == '?' || ch == '<' || ch == '>' || ch == '='
}

func isSymbolChar(ch byte) bool {
    return isSymbolStart(ch) || isDigit(ch)
}

// parseParams parses a parameter vector into fixed params and optional variadic param.
// Recognizes `&` as variadic marker: `[a b & rest]` → fixed=[a,b], variadic=rest.
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

func (r *Reader) peekNext() byte {
    if r.pos+1 >= len(r.input) {
        return 0
    }
    return r.input[r.pos+1]
}
```

---

## 4. Evaluator

### Special Forms

```go
var specialForms = map[string]func(context.Context, *engine, []Value, *Env) (Value, error){
    "def":        evalDef,
    "defn":       evalDefn,
    "defmacro":   evalDefmacro,
    "fn":         evalFn,
    "if":         evalIf,
    "cond":       evalCond,
    "when":       evalWhen,
    "unless":     evalUnless,
    "and":        evalAnd,
    "or":         evalOr,
    "not":        evalNot,
    "let":        evalLet,
    "let*":       evalLetStar,
    "do":         evalDo,
    "quote":      evalQuote,
    "quasiquote": evalQuasiquote,
    "set!":       evalSet,
    "loop":       evalLoop,
    "recur":      evalRecur,
    "try":        evalTry,
    "catch":      evalCatch,
    "throw":      evalThrow,
}
```

Note: `macroexpand` is not a special form — it is registered as a stdlib function by the stdlib plugin.

### Evaluator Structure

```go
// engine is the concrete evaluator; implements the Evaluator interface from types.go.
//
// callDepth uses atomic.Int64 to be safe when multiple goroutines call Apply concurrently
// (e.g. parallel plugin invocations sharing one engine). A plain int field would be a data
// race because apply() increments/decrements it inside a hot loop with no lock.
type engine struct {
    macroDepth    int
    maxMacroDepth int
    MaxDepth      int
    callDepth     atomic.Int64
}

func NewEvaluator() *engine {
    return &engine{maxMacroDepth: 100, MaxDepth: 1000}
}

// Used by the runtime API (Engine.Call) and plugins that need to invoke Lambdas.
func (e *engine) Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
    return e.apply(ctx, fn, args, env)
}

func (e *engine) Eval(ctx context.Context, v Value, env *Env) (Value, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    switch val := v.(type) {
    case Nil, Bool, Int, Float, String, Keyword, *HashMap, Vector, GoFunc:
        return val, nil
    case Symbol:
        if r, ok := env.Get(val.V); ok {
            return r, nil
        }
        return nil, fmt.Errorf("undefined symbol: %s", val.V)
    case List:
        if len(val.Items) == 0 {
            return val, nil
        }
        return e.evalList(ctx, val.Items, env)
    default:
        return nil, fmt.Errorf("cannot evaluate: %v", v)
    }
}

func (e *engine) evalList(ctx context.Context, items []Value, env *Env) (Value, error) {
    head := items[0]

    if sym, ok := head.(Symbol); ok {
        if fn, ok := specialForms[sym.V]; ok {
            return fn(ctx, e, items[1:], env)
        }
    }

    fn, err := e.Eval(ctx, head, env)
    if err != nil { return nil, err }

    if macro, ok := fn.(Macro); ok {
        return e.expandMacro(ctx, macro, items[1:], env)
    }

    args := make([]Value, len(items)-1)
    for i, item := range items[1:] {
        arg, err := e.Eval(ctx, item, env)
        if err != nil { return nil, err }
        args[i] = arg
    }

    return e.apply(ctx, fn, args, env)
}

// Trampoline loop for TCO: evalBody returns a tailCall; apply loops without growing the stack.
func (e *engine) apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        if e.MaxDepth > 0 {
            depth := e.callDepth.Add(1)
            defer e.callDepth.Add(-1)
            if int(depth) > e.MaxDepth {
                return nil, fmt.Errorf("max call depth %d exceeded", e.MaxDepth)
            }
        }

        switch f := fn.(type) {
        case GoFunc:
            return f.Fn(ctx, e, args, env)
        case Lambda:
            child, err := f.Env.ChildVariadic(f.Params, args, f.Variadic)
            if err != nil { return nil, err }
            result, err := e.evalBody(ctx, f.Body, child)
            if err != nil { return nil, err }
            // Trampoline: tail calls loop instead of recursing
            if tc, ok := result.(tailCall); ok {
                fn, args, env = tc.fn, tc.args, tc.env
                continue
            }
            return result, nil
        case Keyword:
            if len(args) != 1 {
                return nil, fmt.Errorf("keyword requires exactly 1 argument, got %d", len(args))
            }
            m, ok := args[0].(*HashMap)
            if !ok {
                return Nil{}, nil
            }
            v, _ := m.Get(f)
            return v, nil
        default:
            return nil, fmt.Errorf("not a function: %v", fn)
        }
    }
}

func (e *engine) evalBody(ctx context.Context, body []Value, env *Env) (Value, error) {
    var result Value = Nil{}
    for _, form := range body {
        var err error
        result, err = e.Eval(ctx, form, env)
        if err != nil { return nil, err }
    }
    return result, nil
}

type tailCall struct {
    fn   Value
    args []Value
    env  *Env
}

func (t tailCall) Type() Keyword    { return Keyword{V: "tail-call"} }
func (t tailCall) String() string   { return "#<tail-call>" }
func (t tailCall) Equals(o Value) bool { return false }
```

> **Eval pipeline phases:** The tree-walker `engine.Eval()` above is Phase 1. The bytecode VM (ch008)
> implements the same `Evaluator` interface as Phase 2. Callers in the runtime API (ch003) select
> the backend via `WithBytecode()` — switching backends requires no changes at call sites.

### Special Form Implementations

```go
func evalDef(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("def requires 2 arguments, got %d", len(args))
    }

    name, ok := args[0].(Symbol)
    if !ok {
        return nil, fmt.Errorf("def requires symbol as first argument")
    }

    val, err := e.Eval(ctx, args[1], env)
    if err != nil { return nil, err }

    env.Set(name.V, val)
    return val, nil
}

func evalDefn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 3 {
        return nil, fmt.Errorf("defn requires at least 3 arguments")
    }

    name, ok := args[0].(Symbol)
    if !ok {
        return nil, fmt.Errorf("defn requires symbol as first argument")
    }

    params, ok := args[1].(Vector)
    if !ok {
        return nil, fmt.Errorf("defn requires vector as second argument")
    }

    fixed, variadic, err := parseParams(params)
    if err != nil { return nil, fmt.Errorf("defn %s: %w", name.V, err) }

    lambda := Lambda{
        Name:     name.V,
        Params:   fixed,
        Variadic: variadic,
        Body:     args[2:],
        Env:      env,
    }

    env.Set(name.V, lambda)
    return lambda, nil
}

func evalDefmacro(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 3 {
        return nil, fmt.Errorf("defmacro requires at least 3 arguments")
    }

    name, ok := args[0].(Symbol)
    if !ok {
        return nil, fmt.Errorf("defmacro requires symbol as first argument")
    }

    params, ok := args[1].(Vector)
    if !ok {
        return nil, fmt.Errorf("defmacro requires vector as second argument")
    }

    fixed, variadic, err := parseParams(params)
    if err != nil { return nil, fmt.Errorf("defmacro %s: %w", name.V, err) }

    macro := Macro{
        Name:     name.V,
        Params:   fixed,
        Variadic: variadic,
        Body:     args[2:],
        Env:      env,
    }

    env.Set(name.V, macro)
    return macro, nil
}

func evalFn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("fn requires at least 2 arguments")
    }

    params, ok := args[0].(Vector)
    if !ok {
        return nil, fmt.Errorf("fn requires vector as first argument")
    }

    fixed, variadic, err := parseParams(params)
    if err != nil { return nil, fmt.Errorf("fn: %w", err) }

    return Lambda{
        Params:   fixed,
        Variadic: variadic,
        Body:     args[1:],
        Env:      env,
    }, nil
}

func evalIf(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 2 && len(args) != 3 {
        return nil, fmt.Errorf("if requires 2 or 3 arguments")
    }

    cond, err := e.Eval(ctx, args[0], env)
    if err != nil { return nil, err }

    if isTruthy(cond) {
        return e.Eval(ctx, args[1], env)
    } else if len(args) == 3 {
        return e.Eval(ctx, args[2], env)
    }
    return Nil{}, nil
}

func evalCond(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    for _, clause := range args {
        list, ok := clause.(List)
        if !ok || len(list.Items) != 2 {
            return nil, fmt.Errorf("cond clauses must be pairs")
        }

        test := list.Items[0]
        if sym, ok := test.(Symbol); ok && sym.V == "else" {
            return e.Eval(ctx, list.Items[1], env)
        }

        result, err := e.Eval(ctx, test, env)
        if err != nil { return nil, err }

        if isTruthy(result) {
            return e.Eval(ctx, list.Items[1], env)
        }
    }
    return Nil{}, nil
}

func evalLet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("let requires at least 2 arguments")
    }

    bindings, ok := args[0].(Vector)
    if !ok || len(bindings.Items)%2 != 0 {
        return nil, fmt.Errorf("let requires even-length vector of bindings")
    }

    child := env.Child()

    for i := 0; i < len(bindings.Items); i += 2 {
        name, ok := bindings.Items[i].(Symbol)
        if !ok {
            return nil, fmt.Errorf("let binding names must be symbols")
        }
        val, err := e.Eval(ctx, bindings.Items[i+1], env)
        if err != nil { return nil, err }
        child.Set(name.V, val)
    }

    return e.evalBody(ctx, args[1:], child)
}

func evalLetStar(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("let* requires at least 2 arguments")
    }

    bindings, ok := args[0].(Vector)
    if !ok || len(bindings.Items)%2 != 0 {
        return nil, fmt.Errorf("let* requires even-length vector of bindings")
    }

    child := env.Child()

    for i := 0; i < len(bindings.Items); i += 2 {
        name, ok := bindings.Items[i].(Symbol)
        if !ok {
            return nil, fmt.Errorf("let* binding names must be symbols")
        }
        val, err := e.Eval(ctx, bindings.Items[i+1], child)
        if err != nil { return nil, err }
        child.Set(name.V, val)
    }

    return e.evalBody(ctx, args[1:], child)
}

func evalDo(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    return e.evalBody(ctx, args, env)
}

func evalQuote(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("quote requires 1 argument")
    }
    return args[0], nil
}

func evalQuasiquote(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("quasiquote requires 1 argument")
    }
    return e.expandQuasiquote(ctx, args[0], env)
}

func (e *engine) expandQuasiquote(ctx context.Context, v Value, env *Env) (Value, error) {
    switch val := v.(type) {
    case List:
        if len(val.Items) > 0 {
            if sym, ok := val.Items[0].(Symbol); ok {
                switch sym.V {
                case "unquote":
                    if len(val.Items) != 2 {
                        return nil, fmt.Errorf("unquote requires 1 argument")
                    }
                    return e.Eval(ctx, val.Items[1], env)
                case "unquote-splicing":
                    return nil, fmt.Errorf("unquote-splicing must be in list context")
                }
            }
        }
        var result []Value
        for _, item := range val.Items {
            if list, ok := item.(List); ok && len(list.Items) > 0 {
                if sym, ok := list.Items[0].(Symbol); ok && sym.V == "unquote-splicing" {
                    if len(list.Items) != 2 {
                        return nil, fmt.Errorf("unquote-splicing requires 1 argument")
                    }
                    expanded, err := e.Eval(ctx, list.Items[1], env)
                    if err != nil { return nil, err }
                    if seq, ok := expanded.(List); ok {
                        result = append(result, seq.Items...)
                    } else if vec, ok := expanded.(Vector); ok {
                        result = append(result, vec.Items...)
                    } else {
                        return nil, fmt.Errorf("unquote-splicing requires sequence")
                    }
                    continue
                }
            }
            expanded, err := e.expandQuasiquote(ctx, item, env)
            if err != nil { return nil, err }
            result = append(result, expanded)
        }
        return List{Items: result}, nil
    case Vector:
        var result []Value
        for _, item := range val.Items {
            expanded, err := e.expandQuasiquote(ctx, item, env)
            if err != nil { return nil, err }
            result = append(result, expanded)
        }
        return Vector{Items: result}, nil
    default:
        return val, nil
    }
}

func evalSet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("set! requires 2 arguments")
    }

    name, ok := args[0].(Symbol)
    if !ok {
        return nil, fmt.Errorf("set! requires symbol as first argument")
    }

    defEnv, ok := env.Find(name.V)
    if !ok {
        return nil, fmt.Errorf("cannot set! undefined variable: %s", name.V)
    }

    val, err := e.Eval(ctx, args[1], env)
    if err != nil { return nil, err }

    defEnv.Set(name.V, val)
    return val, nil
}

func evalLoop(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("loop requires at least 2 arguments")
    }

    bindings, ok := args[0].(Vector)
    if !ok || len(bindings.Items)%2 != 0 {
        return nil, fmt.Errorf("loop requires even-length vector of bindings")
    }

    loopEnv := env.Child()

    loopVars := make([]Symbol, 0, len(bindings.Items)/2)
    for i := 0; i < len(bindings.Items); i += 2 {
        name, ok := bindings.Items[i].(Symbol)
        if !ok {
            return nil, fmt.Errorf("loop binding names must be symbols")
        }
        val, err := e.Eval(ctx, bindings.Items[i+1], env)
        if err != nil { return nil, err }
        loopEnv.Set(name.V, val)
        loopVars = append(loopVars, name)
    }

    for {
        result, err := e.evalBody(ctx, args[1:], loopEnv)
        if err != nil { return nil, err }

        if r, ok := result.(recurVal); ok {
            if len(r.args) != len(loopVars) {
                return nil, fmt.Errorf("recur expected %d args, got %d", len(loopVars), len(r.args))
            }
            for i, v := range loopVars {
                loopEnv.Set(v.V, r.args[i])
            }
            continue
        }

        return result, nil
    }
}

type recurVal struct{ args []Value }
func (r recurVal) Type() Keyword  { return Keyword{V: "recur"} }
func (r recurVal) String() string { return "#<recur>" }
func (r recurVal) Equals(o Value) bool { return false }

func evalRecur(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    vals := make([]Value, len(args))
    for i, arg := range args {
        v, err := e.Eval(ctx, arg, env)
        if err != nil { return nil, err }
        vals[i] = v
    }
    return recurVal{args: vals}, nil
}

func (e *engine) macroexpand(ctx context.Context, form Value, env *Env) (Value, error) {
    list, ok := form.(List)
    if !ok || len(list.Items) == 0 {
        return form, nil
    }

    fn, err := e.Eval(ctx, list.Items[0], env)
    if err != nil { return nil, err }

    macro, ok := fn.(Macro)
    if !ok {
        return form, nil
    }

    expanded, err := e.expandMacro(ctx, macro, list.Items[1:], env)
    if err != nil { return nil, err }

    return e.macroexpand(ctx, expanded, env)
}

func (e *engine) expandMacro(ctx context.Context, m Macro, args []Value, env *Env) (Value, error) {
    if e.macroDepth >= e.maxMacroDepth {
        return nil, fmt.Errorf("macro expansion depth exceeded")
    }
    e.macroDepth++
    defer func() { e.macroDepth-- }()

    macroEnv, err := m.Env.ChildVariadic(m.Params, args, m.Variadic)
    if err != nil {
        return nil, fmt.Errorf("macro %s: %w", m.Name, err)
    }

    result, err := e.evalBody(ctx, m.Body, macroEnv)
    if err != nil { return nil, err }

    return e.Eval(ctx, result, env)
}

// MacroExpand expands all macros in form without evaluating.
// Used by the bytecode compiler (ch008) to pre-expand before code generation.
// This must be a standalone pass callable by the compiler, not only embedded in Eval.
func (e *engine) MacroExpand(ctx context.Context, form Value, env *Env) (Value, error) {
    return e.macroexpand(ctx, form, env)
}

// when / unless — sugar over if, but kept as core special forms because they
// need early evaluation (a macro would require stdlib to be loaded first).

func evalWhen(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("when requires at least 2 arguments")
    }
    cond, err := e.Eval(ctx, args[0], env)
    if err != nil { return nil, err }
    if !isTruthy(cond) {
        return Nil{}, nil
    }
    return e.evalBody(ctx, args[1:], env)
}

func evalUnless(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("unless requires at least 2 arguments")
    }
    cond, err := e.Eval(ctx, args[0], env)
    if err != nil { return nil, err }
    if isTruthy(cond) {
        return Nil{}, nil
    }
    return e.evalBody(ctx, args[1:], env)
}

// and / or / not — logical operators with short-circuit evaluation.
// and returns true if all args are truthy, short-circuits on first falsy.
// or returns first truthy value, or last value if all falsy.
// not returns the logical negation.

func evalAnd(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) == 0 {
        return Bool{V: true}, nil
    }
    var last Value = Bool{V: true}
    for _, arg := range args {
        v, err := e.Eval(ctx, arg, env)
        if err != nil { return nil, err }
        last = v
        if !isTruthy(v) {
            return v, nil
        }
    }
    return last, nil
}

func evalOr(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) == 0 {
        return Nil{}, nil
    }
    var last Value = Nil{}
    for _, arg := range args {
        v, err := e.Eval(ctx, arg, env)
        if err != nil { return nil, err }
        last = v
        if isTruthy(v) {
            return v, nil
        }
    }
    return last, nil
}

func evalNot(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("not requires exactly 1 argument")
    }
    v, err := e.Eval(ctx, args[0], env)
    if err != nil { return nil, err }
    return Bool{V: !isTruthy(v)}, nil
}

// try / catch / throw — structured error handling.
// try evaluates body; on error, catch binds the error string and evaluates handler.
// throw raises a Go error from Lisp code.
//
// Syntax:
//   (try body-form (catch err handler-form))
//   (throw message)

func evalTry(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("try requires a body and a catch clause")
    }

    body := args[0]
    catchClause, ok := args[len(args)-1].(List)
    if !ok || len(catchClause.Items) < 3 {
        return nil, fmt.Errorf("try requires (catch <sym> <handler>) as last clause")
    }
    catchSym, ok := catchClause.Items[0].(Symbol)
    if !ok || catchSym.V != "catch" {
        return nil, fmt.Errorf("try: expected catch clause, got %v", catchClause.Items[0])
    }
    errSym, ok := catchClause.Items[1].(Symbol)
    if !ok {
        return nil, fmt.Errorf("catch: error binding must be a symbol")
    }
    handler := catchClause.Items[2]

    result, err := e.Eval(ctx, body, env)
    if err != nil {
        catchEnv := env.Child()
        catchEnv.Set(errSym.V, String{V: err.Error()})
        return e.Eval(ctx, handler, catchEnv)
    }
    return result, nil
}

func evalCatch(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    // catch is only valid inside try; reaching here directly is an error.
    return nil, fmt.Errorf("catch used outside of try")
}

func evalThrow(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("throw requires 1 argument")
    }
    val, err := e.Eval(ctx, args[0], env)
    if err != nil { return nil, err }
    return nil, fmt.Errorf("%v", val)
}

func isTruthy(v Value) bool {
    if _, ok := v.(Nil); ok { return false }
    if b, ok := v.(Bool); ok { return b.V }
    return true
}
```

---

## 5. Plugin Interface

```go
type Plugin interface {
    Name() string
    Init(env *Env) error
    Metadata() PluginMeta
}

type PluginMeta struct {
    Version     string
    Description string
    Author      string
    Deps        []string // Go module paths this plugin requires
}

type Registry struct {
    mu      sync.RWMutex
    plugins map[string]Plugin
}

func NewRegistry() *Registry {
    return &Registry{plugins: make(map[string]Plugin)}
}

func (r *Registry) Register(p Plugin) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    name := p.Name()
    if _, ok := r.plugins[name]; ok {
        return fmt.Errorf("plugin %s already registered", name)
    }
    
    r.plugins[name] = p
    return nil
}

func (r *Registry) Get(name string) (Plugin, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    p, ok := r.plugins[name]
    return p, ok
}

func (r *Registry) Namespaces() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()

    names := make([]string, 0, len(r.plugins))
    for name := range r.plugins {
        names = append(names, name)
    }
    sort.Strings(names)
    return names
}

func (r *Registry) Unregister(name string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.plugins, name)
}

// HasPrefix reports whether name conflicts with any registered plugin namespace.
// e.g. HasPrefix("llm/complete") returns true if "llm" plugin is registered.
func (r *Registry) HasPrefix(name string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()
    for ns := range r.plugins {
        if strings.HasPrefix(name, ns+"/") || name == ns {
            return true
        }
    }
    return false
}
```

---

## 6. Error Types

```go
type LispicoError struct {
    Code   string
    Message string
    Source string
    Line   int
    Col    int
    Cause  error
}

func (e *LispicoError) Error() string {
    if e.Source != "" {
        return fmt.Sprintf("%s at %s:%d:%d: %s", e.Code, e.Source, e.Line, e.Col, e.Message)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *LispicoError) Unwrap() error { return e.Cause }

func NewReadError(msg string, line, col int) *LispicoError {
    return &LispicoError{Code: "ReadError", Message: msg, Line: line, Col: col}
}

func NewEvalError(msg string, form Value) *LispicoError {
    return &LispicoError{Code: "EvalError", Message: fmt.Sprintf("%s: %v", msg, form)}
}

func NewTypeError(expected string, got Value) *LispicoError {
    return &LispicoError{Code: "TypeError", Message: fmt.Sprintf("expected %s, got %T", expected, got)}
}

func NewArityError(expected, got int) *LispicoError {
    return &LispicoError{Code: "ArityError", Message: fmt.Sprintf("expected %d args, got %d", expected, got)}
}

func NewUndefinedError(name string) *LispicoError {
    return &LispicoError{Code: "UndefinedError", Message: fmt.Sprintf("undefined: %s", name)}
}
```

---

## 7. File Organization

```
core/
├── types.go          # Value interface and implementations
├── env.go            # Environment chain
├── reader.go         # Tokenizer and parser
├── eval.go           # Evaluator and special forms
├── plugin.go         # Plugin interface and registry
└── error.go          # Error types
```

---

## 8. Testing Strategy

### Unit Tests

- Type equality and string representation
- Environment lookup and shadowing
- Reader tokenization edge cases
- Parser nested structures
- Each special form individually
- Error propagation

### Integration Tests

- Full read-eval-print cycle
- Recursive function with TCO
- Macro expansion depth limit
- Plugin registration and namespace isolation
- Concurrent environment access

### Property-Based Tests

- Round-trip: parse → print → parse
- Determinism: same input → same output
- Immutability: operations don't mutate originals

---

**Next Step:** Create tasks document (03-tasks.md) with implementation phases and acceptance criteria.

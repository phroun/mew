// Package protocol implements the KittyTK display-protocol command
// language (plan decisions D10-D17): named properties, alias
// dictionaries, correlation keys with hierarchical scoping and
// explicit surfacing, three-valued boolean flags, children blocks,
// macro templates, and the six-type value system (flag, enum,
// numeric, identifier, {}, "string").
//
// A value may also be written with no name, as an operand of the verb:
// a target reference, a filter's field and what it is matched against.
// D10's rule is about PROPERTIES -- a property always travels under its
// name, which is what makes the alias dictionaries worth having -- and
// a property statement refuses an operand (Session.applyArgs).
//
// This package is transport-agnostic: it parses command text into
// records. Sessions, sockets, alias application, and template
// expansion belong to the interpretation layers above.
//
// Provisional syntax details pending O6 (marked in the plan):
// '#' starts a comment running to end of line; string escapes are
// \\ \" \n \t \r.
package wire

import (
	"fmt"
	"strconv"
	"strings"
)

// FlagState describes a bare-name argument's assertion (D12/D16).
type FlagState int

const (
	// FlagNone means the argument carries a value (name=value).
	FlagNone FlagState = iota
	// FlagTrue is a bare name: `wrap`.
	FlagTrue
	// FlagFalse is a negated name: `!enabled`.
	FlagFalse
	// FlagIndeterminate is an asserted-indeterminate name: `?checked`.
	FlagIndeterminate
)

// ValueKind is the lexical class of a value (D17). Bare words may be
// enums or identifiers - typing is per-property and happens above the
// parser (the tokenizer is schema-free).
type ValueKind int

const (
	WordValue   ValueKind = iota // enum or identifier (bare token)
	NumberValue                  // int or float; property declares domain
	StringValue                  // quotes required on the wire
	BlockValue                   // {} collection of statements
)

// Value is a parsed property value.
type Value struct {
	Kind   ValueKind
	Word   string  // WordValue
	Number float64 // NumberValue: as a float, which is exact only below 2^53
	Int    int64   // NumberValue: the exact value, when IsInt says there is one
	IsInt  bool    // NumberValue: written with no fractional part, and it fits
	Str    string  // StringValue (unescaped)
	Block  *Script // BlockValue

	// Blob marks a StringValue built from arbitrary bytes rather than text,
	// so encoding escapes every one of them (QuoteBlob) instead of writing
	// the printable ones through. It is set by the builder, never by the
	// parser: what comes off the wire is already the bytes that were sent.
	Blob bool
}

// Arg is one argument of a statement: either a flag (bare name with
// FlagTrue/FlagFalse/FlagIndeterminate and nil Value) or a named
// value (FlagNone and non-nil Value). Interpreters for specific verbs
// may treat a leading FlagTrue arg as positional (e.g. the type word
// after `new`).
type Arg struct {
	Name  string
	Flag  FlagState
	Value *Value
}

// Statement is one command. Forms:
//
//	verb args...             (Key="", Verb=verb)
//	key=verb args...         (Key=key, Verb=verb)
//	key=path                 (Key=key, Verb="", Ref=path - D15 surfacing)
//	key=id                   (Key=key, Verb="", RefID=id - D15 surfacing)
//
// The two surfacing forms name the same thing from the two directions a
// client can already reach an object: a key path it built, or an id the
// host handed it (the app and the store arrive in the handshake as bare
// numbers). Either way the name goes in the session's key table, so what
// follows addresses it the way it addresses anything else.
type Statement struct {
	Key   string
	Verb  string
	Ref   string
	RefID uint64
	Args  []*Arg
}

// Script is a sequence of statements (a request body or a {} block).
type Script struct {
	Statements []*Statement
}

// ParseError reports a syntax error with 1-based position.
type ParseError struct {
	Line, Col int
	Msg       string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}

// Parse parses protocol command text into a Script.
func Parse(src string) (*Script, error) {
	p := &parser{src: []rune(src), line: 1, col: 1}
	script, err := p.parseScript(true)
	if err != nil {
		return nil, err
	}
	return script, nil
}

type parser struct {
	src  []rune
	pos  int
	line int
	col  int
}

func (p *parser) errf(format string, args ...interface{}) error {
	return &ParseError{Line: p.line, Col: p.col, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) advance() rune {
	ch := p.src[p.pos]
	p.pos++
	if ch == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	return ch
}

// skipInline consumes spaces, tabs, and comments, but NOT newlines
// (newlines terminate statements).
func (p *parser) skipInline() {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\r':
			p.advance()
		case '#':
			for !p.eof() && p.peek() != '\n' {
				p.advance()
			}
		default:
			return
		}
	}
}

// skipSeparators consumes statement separators: whitespace including
// newlines, semicolons, and comments.
func (p *parser) skipSeparators() {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\r', '\n', ';':
			p.advance()
		case '#':
			for !p.eof() && p.peek() != '\n' {
				p.advance()
			}
		default:
			return
		}
	}
}

func hexVal(ch rune) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10
	}
	return -1
}

// A word may begin with a dot, which is how a name says it is a member of
// something rather than a word in its own right: `.size` is the member called
// size, and `size` is the word size.
func isWordStart(ch rune) bool {
	return ch == '_' || ch == '.' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

// A name carries digits and hyphens after its first character, so `kebab-case`
// is one name rather than a name, a minus and a number.
func isWordRune(ch rune) bool {
	return isWordStart(ch) || ch == '-' || (ch >= '0' && ch <= '9')
}

// isTokenRune is what a bare token is made of -- a number or a symbol, which
// are one run of characters and told apart by what they say rather than by what
// they start with. The plus is in for the exponent's sign, `1e+21`.
func isTokenRune(ch rune) bool {
	return isWordRune(ch) || ch == '+'
}

func isNumberStart(ch rune) bool {
	return ch == '-' || (ch >= '0' && ch <= '9')
}

// atStatementEnd reports whether the next content (after inline space)
// terminates the current statement.
func (p *parser) atStatementEnd(inBlock bool) bool {
	p.skipInline()
	if p.eof() {
		return true
	}
	switch p.peek() {
	case '\n', ';':
		return true
	case '}':
		return inBlock
	}
	return false
}

// parseHead reads a statement's head, which is a run of word runes and may
// begin with any of them -- a digit included.
//
// A block is not always a list of commands. A record is written as one, and a
// record's field names belong to the data rather than to this grammar: a
// positional member's name is its index, so `{ 0 "a"; 1 "b" }` is a record of
// two of them. EncodeStatement has always written that, and until now nothing
// could read it back.
//
// The digits are taken as they stand rather than read as a number. `007` and
// `7` are different names, and a name that went out one way has to come back
// the same way.
func (p *parser) parseHead() (string, error) {
	if p.eof() || !isWordRune(p.peek()) {
		return "", p.errf("expected a name")
	}
	var sb strings.Builder
	for !p.eof() && isWordRune(p.peek()) {
		sb.WriteRune(p.advance())
	}
	return sb.String(), nil
}

func (p *parser) parseWord() (string, error) {
	if p.eof() || !isWordStart(p.peek()) {
		return "", p.errf("expected a name")
	}
	var sb strings.Builder
	for !p.eof() && isWordRune(p.peek()) {
		sb.WriteRune(p.advance())
	}
	return sb.String(), nil
}

// parseProtectedSymbol reads a symbol the grammar has no bare spelling for:
// `(objectLibrary/figaro/3)`.
//
// Parentheses because that is what they already mean. PawScript evaluates a
// block written in braces and preserves what is written in parentheses --
// literal content, held unparsed -- and the wire's block is braces too. So the
// two languages say the same thing with the same brackets: braces for what is
// read, parentheses for what is kept as it stands.
//
// There are no escapes inside, and none are needed: a symbol cannot contain a
// closing parenthesis in PawScript either, so nothing that can be written there
// is unwritable here. A newline is refused for the same reason it ends a
// statement -- an identifier does not hold one.
func (p *parser) parseProtectedSymbol() (string, error) {
	p.advance() // '('
	var sb strings.Builder
	for {
		if p.eof() || p.peek() == '\n' {
			return "", p.errf("unterminated symbol: expected ')'")
		}
		if p.peek() == ')' {
			p.advance()
			break
		}
		sb.WriteRune(p.advance())
	}
	if sb.Len() == 0 {
		return "", p.errf("a symbol is a name, and () is not one")
	}
	return sb.String(), nil
}

func (p *parser) parseString() (string, error) {
	if p.peek() != '"' {
		return "", p.errf("expected string")
	}
	p.advance() // opening quote
	var sb strings.Builder
	for {
		if p.eof() {
			return "", p.errf("unterminated string")
		}
		ch := p.advance()
		switch ch {
		case '"':
			return sb.String(), nil
		case '\\':
			if p.eof() {
				return "", p.errf("unterminated escape")
			}
			esc := p.advance()
			switch esc {
			case '\\':
				sb.WriteRune('\\')
			case '"':
				sb.WriteRune('"')
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case 'e':
				// ESC - terminal traffic's most common byte.
				sb.WriteByte(0x1b)
			case 'x':
				// \xNN arbitrary byte (two hex digits). With this,
				// any byte stream travels as a quoted string; the O6
				// bulk frame is a later transport-phase encoding of
				// the same value.
				var v byte
				for i := 0; i < 2; i++ {
					if p.eof() {
						return "", p.errf("unterminated \\x escape")
					}
					d := hexVal(p.advance())
					if d < 0 {
						return "", p.errf("malformed \\x escape (two hex digits required)")
					}
					v = v<<4 | byte(d)
				}
				sb.WriteByte(v)
			default:
				return "", p.errf("unknown escape \\%c", esc)
			}
		case '\n':
			return "", p.errf("unterminated string (newline)")
		default:
			sb.WriteRune(ch)
		}
	}
}

// scanToken takes the whole run of a bare token. What it is is decided after
// it has been read, not from the character it starts with.
func (p *parser) scanToken() string {
	var sb strings.Builder
	for !p.eof() && isTokenRune(p.peek()) {
		sb.WriteRune(p.advance())
	}
	return sb.String()
}

func (p *parser) parseNumber() (*Value, error) {
	text := p.scanToken()
	v := numberValue(text)
	if v == nil {
		return nil, p.errf("malformed number %q", text)
	}
	return v, nil
}

// parseNumberOrWord reads one bare token and says what it is.
//
// A number and a symbol are the same run of characters, so which one it is
// cannot be decided from the first character: `2026-09-13` starts like a number
// and is a date, and `1e+21` starts like a date and is a number. The token is
// read whole and then measured against the numeric form; anything that is not
// one is a symbol.
//
// A token written with a leading sign is a number and nothing else, so one that
// does not measure up is refused rather than quietly becoming a symbol -- `-`
// on its own is a mistake, not an identifier.
func (p *parser) parseNumberOrWord() (*Value, error) {
	text := p.scanToken()
	if v := numberValue(text); v != nil {
		return v, nil
	}
	if text[0] == '+' || text[0] == '-' {
		return nil, p.errf("malformed number %q", text)
	}
	return &Value{Kind: WordValue, Word: text}, nil
}

// numberValue reads a bare token as a number, and is nil where the token is not
// one:
//
//	[+-]? digits ( "." digits )? ( [eE] [+-]? digits )?
//
// The form is written out rather than handed to the language's own parser,
// because three implementations have to agree on exactly where a number stops
// and a symbol begins -- and each language's parser accepts a different set of
// extras: infinities, not-a-numbers, hexadecimal floats, digit separators.
func numberValue(text string) *Value {
	i, n := 0, len(text)
	digits := func() bool {
		start := i
		for i < n && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		return i > start
	}
	if i < n && (text[i] == '+' || text[i] == '-') {
		i++
	}
	if !digits() {
		return nil
	}
	dot := false
	if i < n && text[i] == '.' {
		i++
		if !digits() {
			return nil
		}
		dot = true
	}
	exp := false
	if i < n && (text[i] == 'e' || text[i] == 'E') {
		i++
		if i < n && (text[i] == '+' || text[i] == '-') {
			i++
		}
		if !digits() {
			return nil
		}
		exp = true
	}
	if i != n {
		return nil
	}
	// A whole number is read as an integer first, so an id or a nanosecond
	// stamp arrives with every digit it was sent with -- a float64 stops being
	// able to tell two integers apart at 2^53. One too large even for an
	// int64 is a float, and says so: IsInt means the exact value is there.
	if !dot && !exp {
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			return &Value{Kind: NumberValue, Number: float64(v), Int: v, IsInt: true}
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	return &Value{Kind: NumberValue, Number: f}
}

func (p *parser) parseValue(inBlock bool) (*Value, error) {
	p.skipInline()
	if p.eof() {
		return nil, p.errf("expected a value")
	}
	switch {
	case p.peek() == '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &Value{Kind: StringValue, Str: s}, nil
	case p.peek() == '(':
		w, err := p.parseProtectedSymbol()
		if err != nil {
			return nil, err
		}
		return &Value{Kind: WordValue, Word: w}, nil
	case p.peek() == '{':
		p.advance() // '{'
		block, err := p.parseScript(false)
		if err != nil {
			return nil, err
		}
		if p.eof() || p.peek() != '}' {
			return nil, p.errf("unterminated block: expected '}'")
		}
		p.advance() // '}'
		return &Value{Kind: BlockValue, Block: block}, nil
	case isTokenRune(p.peek()):
		return p.parseNumberOrWord()
	default:
		return nil, p.errf("unexpected character %q in value position", p.peek())
	}
}

// parseArgs parses a statement's arguments until the statement ends.
func (p *parser) parseArgs(inBlock bool) ([]*Arg, error) {
	var args []*Arg
	for !p.atStatementEnd(inBlock) {
		ch := p.peek()
		switch {
		case ch == '!' || ch == '?':
			p.advance()
			name, err := p.parseWord()
			if err != nil {
				return nil, p.errf("expected flag name after %q", ch)
			}
			state := FlagFalse
			if ch == '?' {
				state = FlagIndeterminate
			}
			args = append(args, &Arg{Name: name, Flag: state})
		case isWordStart(ch):
			name, err := p.parseWord()
			if err != nil {
				return nil, err
			}
			p.skipInline()
			if !p.eof() && p.peek() == '=' {
				p.advance() // '='
				val, err := p.parseValue(inBlock)
				if err != nil {
					return nil, err
				}
				args = append(args, &Arg{Name: name, Value: val})
			} else {
				args = append(args, &Arg{Name: name, Flag: FlagTrue})
			}
		default:
			// An operand: a value with no name, in the order it was
			// written. A verb that takes operands reads them by
			// position -- a target reference (`set 1042 caption=...`),
			// a filter's field and value, a block to nest. One that
			// does not refuses them, which is where D10's
			// named-properties rule holds: see Session.applyArgs.
			val, err := p.parseValue(inBlock)
			if err != nil {
				return nil, err
			}
			args = append(args, &Arg{Value: val})
		}
	}
	return args, nil
}

func (p *parser) parseStatement(inBlock bool) (*Statement, error) {
	first, err := p.parseHead()
	if err != nil {
		return nil, err
	}
	p.skipInline()

	// key=... form?
	if !p.eof() && p.peek() == '=' {
		p.advance() // '='
		p.skipInline()
		// key=id: a name for an object the host handed over as a number.
		// Nothing follows it - an id is not a verb, so there is nothing
		// for arguments to apply to.
		if !p.eof() && isNumberStart(p.peek()) {
			v, err := p.parseNumber()
			if err != nil {
				return nil, err
			}
			if !v.IsInt || v.Int < 0 {
				return nil, p.errf("%q= expected an object id", first)
			}
			if !p.atStatementEnd(inBlock) {
				return nil, p.errf("%q=%d takes nothing after it: an id is not a command",
					first, uint64(v.Int))
			}
			return &Statement{Key: first, RefID: uint64(v.Int)}, nil
		}
		if p.eof() || !isWordStart(p.peek()) {
			return nil, p.errf("expected command or reference after %q=", first)
		}
		second, err := p.parseWord()
		if err != nil {
			return nil, err
		}
		if p.atStatementEnd(inBlock) {
			// key=path surfacing/reference (D15)
			return &Statement{Key: first, Ref: second}, nil
		}
		args, err := p.parseArgs(inBlock)
		if err != nil {
			return nil, err
		}
		return &Statement{Key: first, Verb: second, Args: args}, nil
	}

	// verb args... form
	args, err := p.parseArgs(inBlock)
	if err != nil {
		return nil, err
	}
	return &Statement{Verb: first, Args: args}, nil
}

// parseScript parses statements until EOF (top level) or '}' (block).
func (p *parser) parseScript(topLevel bool) (*Script, error) {
	script := &Script{}
	for {
		p.skipSeparators()
		if p.eof() {
			if !topLevel {
				return nil, p.errf("unterminated block: expected '}'")
			}
			return script, nil
		}
		if p.peek() == '}' {
			if topLevel {
				return nil, p.errf("unexpected '}'")
			}
			return script, nil
		}
		if !isWordRune(p.peek()) {
			return nil, p.errf("expected a statement, found %q", p.peek())
		}
		stmt, err := p.parseStatement(!topLevel)
		if err != nil {
			return nil, err
		}
		script.Statements = append(script.Statements, stmt)
	}
}

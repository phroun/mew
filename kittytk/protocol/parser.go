// Package protocol implements the KittyTK display-protocol command
// language (plan decisions D10-D17): named properties (nothing
// positional), alias dictionaries, correlation keys with hierarchical
// scoping and explicit surfacing, three-valued boolean flags,
// children blocks, macro templates, and the six-type value system
// (flag, enum, numeric, identifier, {}, "string").
//
// This package is transport-agnostic: it parses command text into
// records. Sessions, sockets, alias application, and template
// expansion belong to the interpretation layers above.
//
// Provisional syntax details pending O6 (marked in the plan):
// '#' starts a comment running to end of line; string escapes are
// \\ \" \n \t \r.
package protocol

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
	Number float64 // NumberValue
	IsInt  bool    // NumberValue: no fractional part written
	Str    string  // StringValue (unescaped)
	Block  *Script // BlockValue
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
type Statement struct {
	Key  string
	Verb string
	Ref  string
	Args []*Arg
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

func isWordStart(ch rune) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isWordRune(ch rune) bool {
	return isWordStart(ch) || ch == '.' || (ch >= '0' && ch <= '9')
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

func (p *parser) parseNumber() (*Value, error) {
	var sb strings.Builder
	if p.peek() == '-' {
		sb.WriteRune(p.advance())
	}
	digits := 0
	dot := false
	for !p.eof() {
		ch := p.peek()
		if ch >= '0' && ch <= '9' {
			digits++
			sb.WriteRune(p.advance())
		} else if ch == '.' && !dot {
			dot = true
			sb.WriteRune(p.advance())
		} else {
			break
		}
	}
	if digits == 0 {
		return nil, p.errf("malformed number")
	}
	f, err := strconv.ParseFloat(sb.String(), 64)
	if err != nil {
		return nil, p.errf("malformed number %q", sb.String())
	}
	return &Value{Kind: NumberValue, Number: f, IsInt: !dot}, nil
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
	case isNumberStart(p.peek()):
		return p.parseNumber()
	case isWordStart(p.peek()):
		w, err := p.parseWord()
		if err != nil {
			return nil, err
		}
		return &Value{Kind: WordValue, Word: w}, nil
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
		case isNumberStart(ch):
			// A bare number is an anonymous argument. The only legal
			// use is as a verb's target reference (D19: `set 1042
			// caption=...`); interpreters reject it anywhere else, so
			// D10's nothing-positional rule still holds for
			// properties.
			val, err := p.parseNumber()
			if err != nil {
				return nil, err
			}
			args = append(args, &Arg{Value: val})
		default:
			// D10: nothing positional - bare values are not allowed.
			return nil, p.errf("unexpected %q: values must be named (name=value)", ch)
		}
	}
	return args, nil
}

func (p *parser) parseStatement(inBlock bool) (*Statement, error) {
	first, err := p.parseWord()
	if err != nil {
		return nil, err
	}
	p.skipInline()

	// key=... form?
	if !p.eof() && p.peek() == '=' {
		p.advance() // '='
		p.skipInline()
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
		if !isWordStart(p.peek()) {
			return nil, p.errf("expected a statement, found %q", p.peek())
		}
		stmt, err := p.parseStatement(!topLevel)
		if err != nil {
			return nil, err
		}
		script.Statements = append(script.Statements, stmt)
	}
}

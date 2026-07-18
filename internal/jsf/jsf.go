// Package jsf implements syntax highlighting driven by grammar files in the
// jsf format (the deterministic-state-machine format popularized by JOE and
// documented in its manual). The format is a tiny assembly language for
// lexers: a grammar is a set of named states, each with a color class and a
// list of transition rules keyed by character; rules can repaint recent
// characters, collect words for keyword lookup, remember a delimiter, mark
// regions, and call other grammars as subroutines. mew implements the format
// from its specification with its own engine design; grammars written for
// other jsf implementations load unchanged.
//
// Highlighting is line-oriented: the machine state carried across a line
// boundary (current state, saved delimiter, call stack) is a small comparable
// LineState, so callers cache per-line entry states and can tell exactly when
// a re-highlight after an edit has converged.
package jsf

import (
	"fmt"
	"sort"
	"strings"
)

// wordCap bounds the keyword-collection buffer. Longer words simply never
// match a keyword table (grammar authors keep keywords short).
const wordCap = 63

// ColorDef is one "=Name attrs..." color class from a grammar file.
type ColorDef struct {
	Name string
	SGR  string // ANSI escape built from the file's attributes ("" if none)
}

// action is everything one transition rule can instruct the engine to do.
type action struct {
	target *state
	noeat  bool // re-examine the character in the target state
	repaint int // recolor=-N: negative count of recent chars to repaint

	beginWord bool // buffer: start collecting a word at this character
	holdWord  bool // hold: stop collecting; keep the word for later lookup
	savePair  bool // save_c: remember this character (and its partner)
	saveWord  bool // save_s: remember the collected word as the delimiter
	pushDelim bool // push_c/push_s: stack the current delimiter first
	popDelim  bool // pop_c/pop_s: restore the stacked delimiter

	beginMark     bool // mark
	endMark       bool // markend
	recolorMarked bool // recolormark

	returns bool // return: pop the subroutine call stack
	reset   bool // reset: jump to the root grammar's initial state
	call    *Instance

	// strings/istrings table: collected word -> replacement action.
	words     map[string]*action
	wordSaved *action // the "&" entry: fires when the word equals the saved delimiter
	foldCase  bool    // istrings
}

// span is an inclusive rune interval of a rule's character list.
type span struct{ lo, hi rune }

// rule pairs a character list with its action.
type rule struct {
	spans []span
	act   *action
}

// Context flags: whether a character is part of a comment or a string. A
// state declares them (":comment Comment comment"); when it declares none,
// they are derived from its color class's conventional name, so grammars
// predating context annotations still report usefully. Like color, context
// rides recolor/recolormark repaints — the delimiters of a string take the
// string's context.
const (
	CtxComment uint8 = 1 << iota
	CtxString
)

// state is one ":name Class" state of a grammar.
type state struct {
	name     string
	inst     *Instance
	color    int     // index into inst.Colors; -1 = no/unknown class
	ctx      uint8   // CtxComment/CtxString flags
	rules    []rule  // quoted-list rules, in file order
	fallback *action // the "*" rule
	onPartner *action // the "&" rule: matches the saved delimiter's partner
	onSaved   *action // the "%" rule: matches the saved delimiter itself
}

// Instance is one loaded (file, subroutine, parameter-set) instantiation of a
// grammar. Subroutine calls with different parameters instantiate separately,
// so .ifdef blocks resolve per call site.
type Instance struct {
	Name   string
	Subr   string
	Params []string

	Colors []ColorDef
	refs   []*ColorRef // interned, parallel to Colors
	states []*state
	first  *state
}

// ColorIndex returns the index of a color class by name, or -1.
func (in *Instance) ColorIndex(name string) int {
	for i := range in.Colors {
		if in.Colors[i].Name == name {
			return i
		}
	}
	return -1
}

// frame is one interned subroutine-call frame. Interning makes frames
// pointer-comparable, which keeps LineState a cheap comparable value.
type frame struct {
	parent   *frame
	instance *Instance
	ret      *state
	depth    int
	children map[frameKey]*frame
}

type frameKey struct {
	instance *Instance
	ret      *state
}

// delimFrame is one interned delimiter-stack frame (push_c/push_s nest the
// saved delimiter, e.g. perl's s{...}{...} inside another pair). Interning
// keeps LineState comparable.
type delimFrame struct {
	parent   *delimFrame
	saved    string
	children map[string]*delimFrame
}

// Loader loads grammars by name through a caller-supplied resolver (which
// implements the search path) and interns instances and call frames.
type Loader struct {
	// Resolve returns the contents of <name>.jsf.
	Resolve func(name string) ([]byte, error)

	instances  map[string]*Instance
	roots      map[frameKey]*frame
	delimRoots map[string]*delimFrame
	stateCount int // total states across instances; bounds noeat chains
}

// NewLoader creates a Loader with the given file resolver.
func NewLoader(resolve func(name string) ([]byte, error)) *Loader {
	return &Loader{
		Resolve:    resolve,
		instances:  make(map[string]*Instance),
		roots:      make(map[frameKey]*frame),
		delimRoots: make(map[string]*delimFrame),
	}
}

// delimPush interns the delimiter frame stacking saved beneath parent.
func (l *Loader) delimPush(parent *delimFrame, saved string) *delimFrame {
	m := l.delimRoots
	if parent != nil {
		if parent.children == nil {
			parent.children = make(map[string]*delimFrame)
		}
		m = parent.children
	}
	if f, ok := m[saved]; ok {
		return f
	}
	f := &delimFrame{parent: parent, saved: saved}
	m[saved] = f
	return f
}

// Load loads the top-level grammar of a file (its states outside any .subr).
func (l *Loader) Load(name string) (*Instance, error) {
	return l.load(name, "", nil)
}

func instKey(name, subr string, params []string) string {
	return name + "\x00" + subr + "\x00" + strings.Join(params, "\x00")
}

func (l *Loader) load(name, subr string, params []string) (*Instance, error) {
	key := instKey(name, subr, params)
	if in, ok := l.instances[key]; ok {
		return in, nil
	}
	src, err := l.Resolve(name)
	if err != nil {
		return nil, fmt.Errorf("syntax %q: %w", name, err)
	}
	in := &Instance{Name: name, Subr: subr, Params: params}
	// Register before parsing so mutually-recursive call= references resolve
	// to the partially-built instance (states are patched by pointer).
	l.instances[key] = in
	if err := l.parse(in, src); err != nil {
		delete(l.instances, key)
		return nil, fmt.Errorf("syntax %q: %w", name, err)
	}
	if in.first == nil {
		delete(l.instances, key)
		if subr != "" {
			return nil, fmt.Errorf("syntax %q: no states in subroutine %q", name, subr)
		}
		return nil, fmt.Errorf("syntax %q: no states defined", name)
	}
	return in, nil
}

// callFrame interns the frame for calling `called` returning to ret, beneath
// parent (nil = top level).
func (l *Loader) callFrame(parent *frame, called *Instance, ret *state) *frame {
	key := frameKey{called, ret}
	m := l.roots
	if parent != nil {
		if parent.children == nil {
			parent.children = make(map[frameKey]*frame)
		}
		m = parent.children
	}
	if f, ok := m[key]; ok {
		return f
	}
	depth := 1
	if parent != nil {
		depth = parent.depth + 1
	}
	f := &frame{parent: parent, instance: called, ret: ret, depth: depth}
	m[key] = f
	return f
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type parser struct {
	l     *Loader
	in    *Instance
	lines []string
	idx   int // next line to read
	line  int // current 1-based line number (for errors)

	states map[string]*state

	// .ifdef nesting
	ifs []ifLevel
	// .subr section tracking
	insideSubr bool
	wantedSubr bool

	err error
}

type ifLevel struct {
	ignore   bool // lines in the current branch are skipped
	dead     bool // enclosing branch already skipped: both branches ignore
	elsePart bool
}

func (l *Loader) parse(in *Instance, src []byte) error {
	p := &parser{
		l:      l,
		in:     in,
		lines:  strings.Split(string(src), "\n"),
		states: make(map[string]*state),
	}
	p.run()
	return p.err
}

func (p *parser) errorf(format string, args ...interface{}) {
	if p.err == nil {
		p.err = fmt.Errorf("line %d: "+format, append([]interface{}{p.line}, args...)...)
	}
}

func (p *parser) next() (string, bool) {
	if p.idx >= len(p.lines) {
		return "", false
	}
	s := p.lines[p.idx]
	p.idx++
	p.line = p.idx
	return strings.TrimSuffix(s, "\r"), true
}

// findState returns (creating on first reference) the named state, so rules
// may jump forward to states defined later in the file.
func (p *parser) findState(name string) *state {
	if s, ok := p.states[name]; ok {
		return s
	}
	s := &state{name: name, inst: p.in, color: -1}
	p.states[name] = s
	p.in.states = append(p.in.states, s)
	p.l.stateCount++
	return s
}

func (p *parser) ignoring() bool {
	return len(p.ifs) > 0 && p.ifs[len(p.ifs)-1].ignore
}

func (p *parser) hasParam(name string) bool {
	for _, q := range p.in.Params {
		if q == name {
			return true
		}
	}
	return false
}

// skipSection reports whether state/rule lines are currently outside the
// section this instance wants: a subroutine instantiation only reads its own
// .subr block, and a whole-file load skips every .subr block. Color classes
// and directives are always processed.
func (p *parser) skipSection() bool {
	if p.in.Subr != "" {
		return !(p.insideSubr && p.wantedSubr)
	}
	return p.insideSubr
}

func (p *parser) run() {
	var cur *state
	for p.err == nil {
		raw, ok := p.next()
		if !ok {
			break
		}
		t := newTok(raw)
		t.ws() // leading whitespace; '#' comments end the line inside ws()
		c := t.peek()
		switch {
		case c == 0:
			// blank or comment-only line
		case c == '.':
			t.get()
			p.directive(t)
		case p.ignoring():
			// inside a false .ifdef branch
		case c == '=':
			t.get()
			p.colorDef(t)
		case p.skipSection():
			// state/rule text for a different .subr section
		case c == ':':
			t.get()
			cur = p.stateDef(t)
		case c == '-':
			// obsolete sync-lines directive: ignored
		case c == '"' || c == '*' || c == '&' || c == '%':
			if cur == nil {
				p.errorf("rule before any state definition")
				return
			}
			p.rule(t, cur)
		default:
			p.errorf("unrecognized line %q", raw)
		}
	}
	if p.err == nil && len(p.ifs) > 0 {
		p.errorf("ifdef with no matching endif")
	}
}

func (p *parser) directive(t *tok) {
	name := t.ident()
	switch name {
	case "ifdef":
		lvl := ifLevel{ignore: true, dead: true}
		if !p.ignoring() {
			t.ws()
			param := t.ident()
			if param == "" {
				p.errorf("missing parameter for ifdef")
				return
			}
			lvl.dead = false
			lvl.ignore = !p.hasParam(param)
		}
		p.ifs = append(p.ifs, lvl)
	case "else":
		if n := len(p.ifs); n > 0 && !p.ifs[n-1].elsePart {
			p.ifs[n-1].elsePart = true
			if !p.ifs[n-1].dead {
				p.ifs[n-1].ignore = !p.ifs[n-1].ignore
			}
		} else {
			p.errorf("else with no matching ifdef")
		}
	case "endif":
		if len(p.ifs) > 0 {
			p.ifs = p.ifs[:len(p.ifs)-1]
		} else {
			p.errorf("endif with no matching ifdef")
		}
	case "subr":
		if p.ignoring() {
			return
		}
		t.ws()
		sub := t.ident()
		if sub == "" {
			p.errorf("missing subroutine name")
			return
		}
		p.insideSubr = true
		p.wantedSubr = sub == p.in.Subr
	case "end":
		if p.ignoring() {
			return
		}
		p.insideSubr = false
		p.wantedSubr = false
	case "":
		p.errorf("missing control statement name")
	default:
		p.errorf("unknown control statement .%s", name)
	}
}

// colorDef parses "=Name attrs...". Classes are collected file-wide (even
// inside skipped .subr sections) and the first definition of a name wins.
func (p *parser) colorDef(t *tok) {
	name := t.tows()
	if name == "" {
		p.errorf("missing color class name")
		return
	}
	if p.in.ColorIndex(name) >= 0 {
		return
	}
	var attrs []string
	for {
		t.ws()
		a := t.tows()
		if a == "" {
			break
		}
		attrs = append(attrs, a)
	}
	p.in.Colors = append(p.in.Colors, ColorDef{Name: name, SGR: attrSGR(attrs)})
	p.in.refs = append(p.in.refs, &ColorRef{Syntax: p.in.Name, Class: name, SGR: p.in.Colors[len(p.in.Colors)-1].SGR})
}

func (p *parser) stateDef(t *tok) *state {
	name := t.ident()
	if name == "" {
		p.errorf("missing state name")
		return nil
	}
	s := p.findState(name)
	if p.in.first == nil {
		p.in.first = s
	}
	t.ws()
	class := t.tows()
	if class == "" {
		p.errorf("missing color class for state %q", name)
		return s
	}
	s.color = p.in.ColorIndex(class)
	// Optional context words (comment/string) follow.
	for {
		t.ws()
		word := t.ident()
		if word == "" {
			break
		}
		switch word {
		case "comment":
			s.ctx |= CtxComment
		case "string":
			s.ctx |= CtxString
		}
	}
	if s.ctx == 0 {
		s.ctx = classContext(class)
	}
	return s
}

// classContext derives default context flags from a conventional color-class
// name, for grammars without explicit state annotations.
func classContext(class string) uint8 {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "comment"):
		return CtxComment
	case strings.Contains(c, "string"), strings.Contains(c, "regexp"),
		c == "char", c == "here":
		return CtxString
	}
	return 0
}

func (p *parser) rule(t *tok, cur *state) {
	act := &action{}
	switch t.peek() {
	case '*':
		t.get()
		cur.fallback = act
	case '&':
		t.get()
		cur.onPartner = act
	case '%':
		t.get()
		cur.onSaved = act
	case '"':
		spans := p.charClass(t)
		if p.err != nil {
			return
		}
		cur.rules = append(cur.rules, rule{spans: spans, act: act})
	}
	t.ws()
	target := t.ident()
	if target == "" {
		p.errorf("missing jump target in state %q", cur.name)
		return
	}
	act.target = p.findState(target)
	p.options(act, t, false)
}

// charClass parses a quoted character list: literal runes, "a-z" ranges, and
// backslash escapes.
func (p *parser) charClass(t *tok) []span {
	if t.get() != '"' {
		p.errorf("expected quoted character list")
		return nil
	}
	var out []span
	prev := rune(-1) // last literal, candidate for a range start
	emitPrev := func() {
		if prev >= 0 {
			out = append(out, span{prev, prev})
		}
	}
	for {
		c := t.get()
		switch c {
		case 0:
			p.errorf("unterminated character list")
			return nil
		case '"':
			emitPrev()
			return out
		case '\\':
			emitPrev()
			prev = t.escape()
		case '-':
			if prev < 0 || t.peek() == '"' || t.peek() == 0 {
				// leading or trailing '-': a literal
				emitPrev()
				prev = '-'
				continue
			}
			hi := t.get()
			if hi == '\\' {
				hi = t.escape()
			}
			lo := prev
			if hi < lo {
				lo, hi = hi, lo
			}
			out = append(out, span{lo, hi})
			prev = -1
		default:
			emitPrev()
			prev = c
		}
	}
}

// options parses a rule's option list; strings/istrings consume the
// following file lines until "done".
func (p *parser) options(act *action, t *tok, inStrings bool) {
	for {
		t.ws()
		name := t.ident()
		if name == "" {
			return
		}
		switch name {
		case "noeat":
			act.noeat = true
		case "buffer":
			act.beginWord = true
		case "hold":
			act.holdWord = true
		case "save_c":
			act.savePair = true
		case "save_s":
			act.saveWord = true
		case "push_c":
			act.pushDelim = true
			act.savePair = true
		case "push_s":
			act.pushDelim = true
			act.saveWord = true
		case "pop_c", "pop_s":
			act.popDelim = true
		case "mark":
			act.beginMark = true
		case "markend":
			act.endMark = true
		case "recolormark":
			act.recolorMarked = true
		case "return":
			act.returns = true
		case "reset":
			act.reset = true
		case "recolor":
			t.ws()
			if t.get() != '=' {
				p.errorf("recolor needs =-N")
				return
			}
			t.ws()
			n, ok := t.int()
			if !ok || n >= 0 {
				p.errorf("recolor needs a negative count")
				return
			}
			act.repaint = n
		case "call":
			t.ws()
			if t.get() != '=' {
				p.errorf("call needs =target")
				return
			}
			t.ws()
			p.callOption(act, t)
			if p.err != nil {
				return
			}
		case "strings", "istrings":
			if inStrings {
				p.errorf("nested %s", name)
				return
			}
			act.foldCase = name == "istrings"
			p.stringsBlock(act)
			return
		default:
			p.errorf("unknown option %q", name)
			return
		}
	}
}

// callOption parses call=file.subr(params), call=file(params) or
// call=.subr(params).
func (p *parser) callOption(act *action, t *tok) {
	file := p.in.Name
	sub := ""
	if t.peek() == '.' {
		t.get()
		sub = t.ident()
		if sub == "" {
			p.errorf("missing subroutine name in call")
			return
		}
	} else {
		file = t.ident()
		if file == "" {
			p.errorf("missing target in call")
			return
		}
		if t.peek() == '.' {
			t.get()
			sub = t.ident()
			if sub == "" {
				p.errorf("missing subroutine name in call")
				return
			}
		}
	}
	params := p.callParams(t)
	inst, err := p.l.load(file, sub, params)
	if err != nil {
		p.errorf("call: %v", err)
		return
	}
	act.call = inst
}

// callParams parses "(p1 p2 -p3)": starting from the calling instance's
// parameters, bare names add and -name removes. The set is kept sorted so
// parameter order never distinguishes two identical instantiations.
func (p *parser) callParams(t *tok) []string {
	set := map[string]bool{}
	for _, q := range p.in.Params {
		set[q] = true
	}
	t.ws()
	if t.peek() == '(' {
		t.get()
		for {
			t.ws()
			if t.peek() == ')' {
				t.get()
				break
			}
			neg := t.peek() == '-'
			if neg {
				t.get()
			}
			name := t.ident()
			if name == "" {
				p.errorf("missing parameter name in call")
				return nil
			}
			if neg {
				delete(set, name)
			} else {
				set[name] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for q := range set {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

// stringsBlock reads `"word" target [options]` lines until "done". Each entry
// implies noeat; the special "&" word matches the saved delimiter.
func (p *parser) stringsBlock(act *action) {
	for {
		raw, ok := p.next()
		if !ok {
			p.errorf("unterminated strings block")
			return
		}
		t := newTok(raw)
		t.ws()
		switch {
		case t.peek() == 0:
			// blank/comment line inside the block
		case t.peek() == '"':
			word := p.quoted(t)
			if p.err != nil {
				return
			}
			t.ws()
			target := t.ident()
			if target == "" {
				p.errorf("missing state name in strings entry")
				return
			}
			entry := &action{noeat: true, target: p.findState(target)}
			p.options(entry, t, true)
			if p.err != nil {
				return
			}
			if word == "&" {
				act.wordSaved = entry
				continue
			}
			if act.foldCase {
				word = strings.ToLower(word)
			}
			if act.words == nil {
				act.words = make(map[string]*action)
			}
			if _, dup := act.words[word]; !dup {
				act.words[word] = entry
			}
		case t.ident() == "done":
			return
		default:
			p.errorf("expected \"word\" or done in strings block")
			return
		}
	}
}

// quoted parses a quoted literal (escapes allowed, no ranges).
func (p *parser) quoted(t *tok) string {
	if t.get() != '"' {
		p.errorf("expected string")
		return ""
	}
	var b strings.Builder
	for {
		c := t.get()
		switch c {
		case 0:
			p.errorf("unterminated string")
			return ""
		case '"':
			return b.String()
		case '\\':
			b.WriteRune(t.escape())
		default:
			b.WriteRune(c)
		}
	}
}

// ---------------------------------------------------------------------------
// Line tokenizer
// ---------------------------------------------------------------------------

// tok is a cursor over one line's runes.
type tok struct {
	r []rune
	i int
}

func newTok(s string) *tok { return &tok{r: []rune(s)} }

func (t *tok) peek() rune {
	if t.i < len(t.r) {
		return t.r[t.i]
	}
	return 0
}

func (t *tok) get() rune {
	c := t.peek()
	if c != 0 {
		t.i++
	}
	return c
}

// ws skips spaces and tabs; a '#' seen here starts a comment running to the
// end of the line (comments are valid wherever whitespace is).
func (t *tok) ws() {
	for {
		switch t.peek() {
		case ' ', '\t':
			t.i++
		case '#':
			t.i = len(t.r)
			return
		default:
			return
		}
	}
}

func isIdentRune(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (t *tok) ident() string {
	start := t.i
	for isIdentRune(t.peek()) {
		t.i++
	}
	return string(t.r[start:t.i])
}

// tows reads up to the next whitespace or comment (color class names may
// contain dots, so they are not plain idents).
func (t *tok) tows() string {
	start := t.i
	for {
		c := t.peek()
		if c == 0 || c == ' ' || c == '\t' || c == '#' {
			break
		}
		t.i++
	}
	return string(t.r[start:t.i])
}

func (t *tok) int() (int, bool) {
	neg := t.peek() == '-'
	if neg {
		t.get()
	}
	start := t.i
	n := 0
	for t.peek() >= '0' && t.peek() <= '9' {
		n = n*10 + int(t.get()-'0')
	}
	if t.i == start {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// escape decodes the character after a backslash.
func (t *tok) escape() rune {
	c := t.get()
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'a':
		return '\a'
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'e':
		return 0x1b
	case '0':
		return 0
	case 'x':
		n := rune(0)
		for i := 0; i < 2; i++ {
			d := t.peek()
			switch {
			case d >= '0' && d <= '9':
				n = n*16 + (d - '0')
			case d >= 'a' && d <= 'f':
				n = n*16 + (d - 'a' + 10)
			case d >= 'A' && d <= 'F':
				n = n*16 + (d - 'A' + 10)
			default:
				return n
			}
			t.get()
		}
		return n
	case 0:
		return '\\'
	default:
		// \" \' \- \\ and any other escaped literal
		return c
	}
}

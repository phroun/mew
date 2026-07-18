package jsf

import "strings"

// ColorRef identifies the color class assigned to a character: the grammar
// file it came from (subroutine calls may cross files), the class name within
// it, and the SGR sequence built from the file's own "=Class attrs" line.
// Refs are interned per class, so hosts can cache resolved colors by pointer.
type ColorRef struct {
	Syntax string
	Class  string
	SGR    string
}

// LineState is the machine state carried across a line boundary: the current
// state, the saved delimiter, and the subroutine call stack (an interned
// frame pointer). It is a small comparable value; the zero LineState is the
// entry to the first line of a file.
type LineState struct {
	state *state
	saved string
	frame *frame
	delim *delimFrame
}

// Valid reports whether the state came from HighlightLine (the zero state is
// valid only as the entry to line 0 — caches use !Valid as "unknown").
func (st LineState) Valid() bool { return st.state != nil }

// fallbackAction handles a state with no "*" rule: eat the character and
// start over from the root grammar's initial state.
var fallbackAction = &action{reset: true}

// partner returns the closing partner of an opening delimiter (for save_c's
// pair matching), or the character itself.
func partner(c rune) rune {
	switch c {
	case '<':
		return '>'
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	case '`':
		return '\''
	}
	return c
}

func colorOf(s *state) *ColorRef {
	if s.color >= 0 {
		return s.inst.refs[s.color]
	}
	return nil
}

// run is the per-line highlighting context: the paint buffer, the cursor, the
// word being collected for keyword lookup, and the marked region — all in
// absolute line indices.
type run struct {
	l     *Loader
	root  *Instance
	attrs []*ColorRef
	ctxs  []uint8 // CtxComment/CtxString per index, painted alongside attrs

	// Point-query tracing (ContextAt): the state and frame that examined
	// each character last (nil slices when not tracing).
	traceStates []*state
	traceFrames []*frame

	pos   int // index of the character being examined
	cur   *state
	saved string
	fr    *frame
	dl    *delimFrame

	word       []rune // collected word (keyword lookup)
	wordAt     int    // index where collection began
	collecting bool

	markAt  int // marked-region start
	markTo  int // marked-region end (once closed)
	marking bool
}

// HighlightLine runs the machine over one line (without its line terminator;
// a '\n' is fed at the end, since grammars transition on it). It returns one
// *ColorRef per rune (nil = unstyled) and the entry state for the next line.
// Pass the zero LineState for the first line; root is the top-level grammar
// that reset jumps back to.
func (l *Loader) HighlightLine(root *Instance, st LineState, runes []rune) ([]*ColorRef, LineState) {
	attrs, _, next := l.HighlightLineFull(root, st, runes)
	return attrs, next
}

// HighlightLineFull is HighlightLine, also returning each rune's context
// flags (CtxComment/CtxString) — the "is this character part of a comment or
// a string" signal used for context-aware matching.
func (l *Loader) HighlightLineFull(root *Instance, st LineState, runes []rune) ([]*ColorRef, []uint8, LineState) {
	r := l.newRun(root, st, len(runes))
	for _, c := range runes {
		r.step(c)
	}
	r.step('\n')
	return r.attrs[:len(runes)], r.ctxs[:len(runes)], LineState{state: r.cur, saved: r.saved, frame: r.fr, delim: r.dl}
}

// Context describes what the machine was doing at one character: the grammar
// (innermost, through subroutine calls), its color class and state, the
// comment/string flags, and the embedded-language stack from innermost to the
// root grammar.
type Context struct {
	Syntax  string
	Class   string
	State   string
	Comment bool
	String  bool
	Stack   []string
}

// ContextAt reports the machine's context at rune idx of a line entered in
// state st (idx may equal len(runes): the end-of-line position). The
// comment/string flags follow color semantics (repaints carry them); the
// state, grammar and stack are the machine's when it examined the character.
func (l *Loader) ContextAt(root *Instance, st LineState, runes []rune, idx int) Context {
	r := l.newRun(root, st, len(runes))
	r.traceStates = make([]*state, len(runes)+1)
	r.traceFrames = make([]*frame, len(runes)+1)
	for _, c := range runes {
		r.step(c)
	}
	r.step('\n')

	if idx < 0 {
		idx = 0
	}
	if idx > len(runes) {
		idx = len(runes)
	}
	s := r.traceStates[idx]
	if s == nil {
		s = root.first
	}
	out := Context{
		Syntax:  s.inst.Name,
		State:   s.name,
		Comment: r.ctxs[idx]&CtxComment != 0,
		String:  r.ctxs[idx]&CtxString != 0,
	}
	if s.color >= 0 {
		out.Class = s.inst.Colors[s.color].Name
	}
	for fr := r.traceFrames[idx]; fr != nil; fr = fr.parent {
		out.Stack = append(out.Stack, fr.instance.Name)
	}
	out.Stack = append(out.Stack, root.Name)
	return out
}

func (l *Loader) newRun(root *Instance, st LineState, n int) *run {
	r := &run{
		l:    l,
		root: root,
		// One extra slot absorbs the synthetic newline's own color.
		attrs: make([]*ColorRef, n+1),
		ctxs:  make([]uint8, n+1),
		cur:   st.state,
		saved: st.saved,
		fr:    st.frame,
		dl:    st.delim,
	}
	if r.cur == nil {
		r.cur = root.first
	}
	return r
}

// step feeds one character through the machine: a chain of noeat hops ending
// in the state that eats it. Each hop paints the character with its state's
// color (so the eventual eater wins) and applies the matched rule's repaint
// effects with the color of the state being entered.
func (r *run) step(c rune) {
	// A noeat chain longer than every state in every loaded grammar is a
	// grammar bug (an infinite loop); force the eat instead of hanging.
	maxHops := r.l.stateCount + 8

	for hop := 0; ; hop++ {
		r.paint(r.pos, r.pos+1, colorOf(r.cur), r.cur.ctx)
		if r.traceStates != nil {
			r.traceStates[r.pos] = r.cur
			r.traceFrames[r.pos] = r.fr
		}

		act := r.actionFor(c)
		act, wordMatched := r.wordLookup(act)
		next := r.enter(act)
		entered := colorOf(next)

		if wordMatched {
			// A matched keyword takes the color of the state it jumps to —
			// this is how a bare ":kw Keyword" state colors keywords.
			r.paint(r.wordAt, r.wordAt+len(r.word), entered, next.ctx)
		}
		if act.repaint < 0 {
			// recolor=-N: the last N characters, ending with this one.
			r.paint(r.pos+1+act.repaint, r.pos+1, entered, next.ctx)
		}
		if act.recolorMarked {
			lo, hi := r.marked()
			r.paint(lo, hi, entered, next.ctx)
		}

		// Delimiter and word side effects (saves read the word as it was
		// before this rule restarts collection). push_c/push_s stack the
		// CURRENT delimiter before their save overwrites it; pop restores
		// the stacked one afterwards.
		if act.pushDelim {
			r.dl = r.l.delimPush(r.dl, r.saved)
		}
		if act.saveWord {
			r.saved = string(r.word)
		}
		if act.savePair {
			r.saved = string([]rune{c, partner(c)})
		}
		if act.popDelim {
			if r.dl != nil {
				r.saved = r.dl.saved
				r.dl = r.dl.parent
			}
		}
		if act.beginWord {
			r.word = r.word[:0]
			r.wordAt = r.pos
			r.collecting = true
		}
		if act.holdWord {
			r.collecting = false
		}
		if act.beginMark {
			r.markAt = r.pos
			r.marking = true
		}
		if act.endMark {
			r.markTo = r.pos
			r.marking = false
		}

		r.cur = next
		if !act.noeat || hop >= maxHops {
			break
		}
	}

	// Eat: the character joins the collected word, and the cursor advances.
	if r.collecting && len(r.word) < wordCap {
		r.word = append(r.word, c)
	}
	r.pos++
}

// paint colors the half-open index range [lo, hi), clipped to the line.
// Context flags travel with the color ("the context is part of the color"),
// so recolored delimiters take their string's or comment's context.
func (r *run) paint(lo, hi int, ref *ColorRef, ctx uint8) {
	if lo < 0 {
		lo = 0
	}
	if hi > len(r.attrs) {
		hi = len(r.attrs)
	}
	for i := lo; i < hi; i++ {
		r.attrs[i] = ref
		r.ctxs[i] = ctx
	}
}

// marked is the marked region: closed by markend, or still growing (up to,
// not including, the current character).
func (r *run) marked() (int, int) {
	if r.marking {
		return r.markAt, r.pos
	}
	return r.markAt, r.markTo
}

// actionFor picks the current state's rule for c: the saved-delimiter rules
// ("%" the delimiter itself, "&" its partner — live only while the saved
// delimiter is a save_c pair), then the quoted character lists, then "*".
func (r *run) actionFor(c rune) *action {
	s := r.cur
	if s.onSaved != nil || s.onPartner != nil {
		if pair := []rune(r.saved); len(pair) == 2 {
			if s.onSaved != nil && c == pair[0] {
				return s.onSaved
			}
			if s.onPartner != nil && c == pair[1] {
				return s.onPartner
			}
		}
	}
	for i := range s.rules {
		for _, sp := range s.rules[i].spans {
			if c >= sp.lo && c <= sp.hi {
				return s.rules[i].act
			}
		}
	}
	if s.fallback != nil {
		return s.fallback
	}
	return fallbackAction
}

// wordLookup replaces act with its strings-table entry when the collected
// word matches — a keyword, or (for the "&" entry) the saved delimiter.
func (r *run) wordLookup(act *action) (*action, bool) {
	if act.words == nil && act.wordSaved == nil {
		return act, false
	}
	word := string(r.word)
	saved := r.saved
	if act.foldCase {
		word = strings.ToLower(word)
		saved = strings.ToLower(saved)
	}
	if act.wordSaved != nil && r.saved != "" && word == saved {
		return act.wordSaved, true
	}
	if entry, ok := act.words[word]; ok {
		return entry, true
	}
	return act, false
}

// enter resolves the state an action transitions to, maintaining the
// subroutine call stack.
func (r *run) enter(act *action) *state {
	switch {
	case act.call != nil:
		if r.fr != nil && r.fr.depth >= 100 {
			break // runaway recursion: fall through to a plain jump
		}
		r.fr = r.l.callFrame(r.fr, act.call, act.target)
		return r.fr.instance.first
	case act.returns:
		if r.fr != nil {
			ret := r.fr.ret
			r.fr = r.fr.parent
			if ret != nil {
				return ret
			}
			return r.root.first
		}
	case act.reset:
		return r.root.first
	}
	if act.target != nil {
		return act.target
	}
	return r.root.first
}

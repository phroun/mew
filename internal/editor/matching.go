package editor

import (
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/jsf"
	"github.com/phroun/mew/internal/window"
)

// gotoMatchingBracket (go_match) jumps the caret to the counterpart of the
// construct under it. The matchers, in order:
//
//  1. Bracket characters () [] {} — context-aware when a grammar applies:
//     candidates count only when their comment/string context equals the
//     start bracket's, so a ")" inside a string never answers a code "(",
//     while matching FROM inside a string stays inside strings.
//  2. HTML-style tags (grammars with tags=true): on "<" or ">" the tag's
//     own delimiters pair with each other (jump between the ends of the
//     tag); anywhere else inside the tag, <name ...> pairs with </name> —
//     landing one rune past the "<", on the name (or on the "/" of a
//     closing tag) — skipping self-closing and void elements, counting
//     only same-name tags in the same context.
//  3. Token pairs from [match.<grammar>]: if/fi, do/done, \begin/\end,
//     #if/#endif, ... — openers sharing a closer count as one nesting
//     family (lua's if/do/function all close with end).
//  4. In ANY document, tag matching runs as a last resort: an HTML tag in a
//     string or comment of some other language matches locally, because
//     the context filter confines candidates to the same comment/string
//     region the caret is in.
func (e *Editor) gotoMatchingBracket() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return false
	}
	s := &matchScanner{e: e, b: w.Buffer, lineCount: w.Buffer.GetLineCount(), ctx: map[int][]uint8{}, lines: map[int][]rune{}}
	s.mi = e.resolvedMatchIgnores(w)
	s.fallbackCtx = e.wantFallbackCtx(w)
	pos := w.CursorPos()
	runes := s.runesAt(pos.Line)
	var c rune
	if pos.Rune < len(runes) {
		c = runes[pos.Rune]
	}

	if _, isBracket := bracketPairs[c]; isBracket {
		if line, r, ok := s.bracketMatch(pos.Line, pos.Rune); ok {
			return e.jumpTo(w, line, r)
		}
		return false // bracketMatch showed its own warning
	}

	// A quote at either end of a string region matches its mate — inside a
	// tag's attributes this supersedes the tag jump, and it works in any
	// grammar whose strings carry context (C strings, Python triples, ...).
	if c == '"' || c == '\'' || c == '`' {
		if line, r, ok, applies := s.quoteMatch(pos.Line, pos.Rune); applies {
			if ok {
				return e.jumpTo(w, line, r)
			}
			e.ShowWarning("No matching quote found")
			return false
		}
	}

	// Tag matching: the "<"/">" delimiters pair with each other,
	// superseding the begin/end-tag jump, which runs from the name.
	tryTags := func() (handled, jumped bool) {
		if c == '<' || c == '>' {
			if line, r, ok := s.angleMatch(pos.Line, pos.Rune); ok {
				return true, e.jumpTo(w, line, r)
			}
			e.ShowWarning("No matching bracket found")
			return true, false
		}
		if line, r, ok, applies := s.tagMatch(pos.Line, pos.Rune); applies {
			if ok {
				return true, e.jumpTo(w, line, r)
			}
			e.ShowWarning("No matching tag found")
			return true, false
		}
		return false, false
	}

	pairs := e.matchPairsFor(w.Buffer)
	tagsMode := truthy(pairs["tags"])
	if tagsMode {
		if handled, jumped := tryTags(); handled {
			return jumped
		}
	}
	if line, r, ok, applies := s.tokenMatch(pos.Line, pos.Rune, pairs); applies {
		if ok {
			return e.jumpTo(w, line, r)
		}
		e.ShowWarning("No matching token found")
		return false
	}
	if !tagsMode {
		// The local fallback: embedded tags match in any document.
		if handled, jumped := tryTags(); handled {
			return jumped
		}
	}

	e.ShowWarning("Nothing to match here")
	return false
}

func (e *Editor) jumpTo(w *window.Window, line, r int) bool {
	w.SetCursorPos(window.Position{Line: line, Rune: r})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// matchPairsFor returns the [match.<grammar>] table for the buffer's grammar
// (empty when no grammar or no table applies).
func (e *Editor) matchPairsFor(b *buffer.Buffer) map[string]string {
	c := e.ensureSynCache(b, 0)
	if c == nil || c.grammar == nil {
		return nil
	}
	return e.LoadedConfig.MatchPairs[c.grammar.Name]
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

// matchScanner walks buffer lines with memoized rune slices and syntax
// context flags for one match operation.
type matchScanner struct {
	e         *Editor
	b         *buffer.Buffer
	lineCount int
	ctx       map[int][]uint8
	lines     map[int][]rune

	// Fallback pseudo-context for grammar-less buffers, per the
	// matchIgnores* options: computed sequentially (block comments carry
	// across lines) and memoized per operation. mi holds the flags resolved for
	// the window (base overlaid by its class/grammar/type cascade).
	fallbackCtx bool
	mi          matchIgnores
	pctx        [][]uint8
	pctxInBlock bool
}

// matchIgnores holds the resolved matchIgnores* fallback flags for a window:
// the base [options] overlaid by the window's class/grammar/type cascade.
type matchIgnores struct {
	singleQuote, doubleQuote, slashStar, slashSlash, hash, doubleHyphen, semicolon, percent bool
}

func (e *Editor) resolvedMatchIgnores(w *window.Window) matchIgnores {
	return matchIgnores{
		singleQuote:  e.optBool(w, "matchignoressinglequote", e.Config.MatchIgnoresSingleQuote),
		doubleQuote:  e.optBool(w, "matchignoresdoublequote", e.Config.MatchIgnoresDoubleQuote),
		slashStar:    e.optBool(w, "matchignoresslashstar", e.Config.MatchIgnoresSlashStar),
		slashSlash:   e.optBool(w, "matchignoresslashslash", e.Config.MatchIgnoresSlashSlash),
		hash:         e.optBool(w, "matchignoreshash", e.Config.MatchIgnoresHash),
		doubleHyphen: e.optBool(w, "matchignoresdoublehyphen", e.Config.MatchIgnoresDoubleHyphen),
		semicolon:    e.optBool(w, "matchignoressemicolon", e.Config.MatchIgnoresSemicolon),
		percent:      e.optBool(w, "matchignorespercent", e.Config.MatchIgnoresPercent),
	}
}

func (m matchIgnores) any() bool {
	return m.singleQuote || m.doubleQuote || m.slashStar || m.slashSlash ||
		m.hash || m.doubleHyphen || m.semicolon || m.percent
}

// wantFallbackCtx reports whether the window's buffer needs the pseudo-context
// scanner: no grammar applies and at least one (resolved) matchIgnores flag is
// on.
func (e *Editor) wantFallbackCtx(w *window.Window) bool {
	if c := e.ensureSynCache(w.Buffer, 0); c != nil && c.grammar != nil {
		return false // the real highlighter context supersedes the flags
	}
	return e.resolvedMatchIgnores(w).any()
}

// pseudoCtxLine lexes one line under the matchIgnores* flags, carrying the
// block-comment state in and out. Delimiters take their construct's context,
// matching the highlighter's convention.
func (s *matchScanner) pseudoCtxLine(runes []rune, inBlock bool) ([]uint8, bool) {
	mi := s.mi
	ctx := make([]uint8, len(runes))
	const (
		normal = iota
		sq
		dq
	)
	mode := normal
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch {
		case inBlock:
			ctx[i] = jsf.CtxComment
			if c == '*' && next == '/' {
				ctx[i+1] = jsf.CtxComment
				i++
				inBlock = false
			}
		case mode == sq:
			ctx[i] = jsf.CtxString
			if c == '\\' && i+1 < len(runes) {
				ctx[i+1] = jsf.CtxString
				i++
			} else if c == '\'' {
				mode = normal
			}
		case mode == dq:
			ctx[i] = jsf.CtxString
			if c == '\\' && i+1 < len(runes) {
				ctx[i+1] = jsf.CtxString
				i++
			} else if c == '"' {
				mode = normal
			}
		default:
			lineComment := (mi.slashSlash && c == '/' && next == '/') ||
				(mi.doubleHyphen && c == '-' && next == '-') ||
				(mi.hash && c == '#') ||
				(mi.semicolon && c == ';') ||
				(mi.percent && c == '%')
			switch {
			case mi.slashStar && c == '/' && next == '*':
				ctx[i] = jsf.CtxComment
				ctx[i+1] = jsf.CtxComment
				i++
				inBlock = true
			case lineComment:
				for j := i; j < len(runes); j++ {
					ctx[j] = jsf.CtxComment
				}
				i = len(runes)
			case mi.singleQuote && c == '\'':
				ctx[i] = jsf.CtxString
				mode = sq
			case mi.doubleQuote && c == '"':
				ctx[i] = jsf.CtxString
				mode = dq
			}
		}
	}
	// Quotes do not span lines; only the block comment state carries.
	return ctx, inBlock
}

// pseudoCtxAt extends the pseudo-context sequentially through line and
// returns the flags at (line, r).
func (s *matchScanner) pseudoCtxAt(line, r int) uint8 {
	for ln := len(s.pctx); ln <= line && ln < s.lineCount; ln++ {
		ctx, inBlock := s.pseudoCtxLine(s.runesAt(ln), s.pctxInBlock)
		s.pctx = append(s.pctx, ctx)
		s.pctxInBlock = inBlock
	}
	if line < len(s.pctx) && r >= 0 && r < len(s.pctx[line]) {
		return s.pctx[line][r]
	}
	return 0
}

func (s *matchScanner) runesAt(line int) []rune {
	if r, ok := s.lines[line]; ok {
		return r
	}
	r := []rune(strings.TrimRight(s.b.GetLine(line), "\n\r"))
	s.lines[line] = r
	return r
}

// ctxAt is the comment/string flags of a position: the highlighter's when a
// grammar applies, else the matchIgnores* pseudo-context (0 when neither).
func (s *matchScanner) ctxAt(line, r int) uint8 {
	if s.fallbackCtx {
		return s.pseudoCtxAt(line, r)
	}
	c, ok := s.ctx[line]
	if !ok {
		c = s.e.syntaxCtxLine(s.b, line)
		s.ctx[line] = c
	}
	if r >= 0 && r < len(c) {
		return c[r]
	}
	return 0
}

// ---------------------------------------------------------------------------
// Bracket characters
// ---------------------------------------------------------------------------

var bracketPairs = map[rune]struct {
	match   rune
	forward bool
}{
	'(': {')', true}, '[': {']', true}, '{': {'}', true},
	')': {'(', false}, ']': {'[', false}, '}': {'{', false},

	// Typographic prose pairs: unlike ASCII quotes these are directional,
	// so they match like brackets (with nesting and context filtering).
	// Curly quotes follow the English/French conventions; note that ’ is
	// also the apostrophe, so go_match on an apostrophe scans for a ‘ it
	// may not have. German-style „…“ matches forward from the „.
	'‘': {'’', true},  // ‘ … ’
	'’': {'‘', false}, // ’
	'“': {'”', true},  // “ … ”
	'”': {'“', false}, // ”
	'„': {'“', true},  // „ … “ (forward only; “ pairs back to “’s opener role)
	'«': {'»', true},  // « … »
	'»': {'«', false}, // »
	'‹': {'›', true},  // ‹ … ›
	'›': {'‹', false}, // ›

	// CJK quotation and bracket forms.
	'「': {'」', true}, '」': {'「', false},
	'『': {'』', true}, '』': {'『', false},
	'《': {'》', true}, '》': {'《', false},
	'〈': {'〉', true}, '〉': {'〈', false},
	'【': {'】', true}, '】': {'【', false},
	'〔': {'〕', true}, '〕': {'〔', false},

	// Fullwidth ASCII bracket forms.
	'（': {'）', true}, '）': {'（', false},
	'［': {'］', true}, '］': {'［', false},
	'｛': {'｝', true}, '｝': {'｛', false},

	// Mathematical angle brackets.
	'⟨': {'⟩', true}, '⟩': {'⟨', false},
}

// bracketMatch matches the bracket character at (line, r), counting only
// same-context candidates.
func (s *matchScanner) bracketMatch(line, r int) (int, int, bool) {
	open := s.runesAt(line)[r]
	pair, isBracket := bracketPairs[open]
	if !isBracket {
		return 0, 0, false
	}
	startCtx := s.ctxAt(line, r)
	depth := 1
	step := 1
	if !pair.forward {
		step = -1
	}
	pos := r
	for {
		pos += step
		runes := s.runesAt(line)
		if step > 0 && pos >= len(runes) {
			line++
			if line >= s.lineCount {
				break
			}
			pos = -1
			continue
		}
		if step < 0 && pos < 0 {
			line--
			if line < 0 {
				break
			}
			pos = len(s.runesAt(line))
			continue
		}
		c := runes[pos]
		if (c == open || c == pair.match) && s.ctxAt(line, pos) == startCtx {
			if c == open {
				depth++
			} else if depth--; depth == 0 {
				return line, pos, true
			}
		}
	}
	s.e.ShowWarning("No matching bracket found")
	return 0, 0, false
}

// ---------------------------------------------------------------------------
// Quote mates (string-region ends)
// ---------------------------------------------------------------------------

func isQuoteRune(c rune) bool { return c == '"' || c == '\'' || c == '`' }

// stepPos advances one text position in direction dir (+1/-1), crossing line
// boundaries and skipping empty lines (bounded, so a runaway walk on a huge
// file stays cheap).
func (s *matchScanner) stepPos(line, r, dir int) (int, int, bool) {
	if dir > 0 {
		if r+1 < len(s.runesAt(line)) {
			return line, r + 1, true
		}
		for ln := line + 1; ln < s.lineCount && ln <= line+1000; ln++ {
			if len(s.runesAt(ln)) > 0 {
				return ln, 0, true
			}
		}
		return 0, 0, false
	}
	if r > 0 {
		return line, r - 1, true
	}
	for ln := line - 1; ln >= 0 && ln >= line-1000; ln-- {
		if n := len(s.runesAt(ln)); n > 0 {
			return ln, n - 1, true
		}
	}
	return 0, 0, false
}

// stringRegionEnd walks to the far end of the contiguous string-context
// region containing (line, r).
func (s *matchScanner) stringRegionEnd(line, r, dir int) (int, int) {
	for steps := 0; steps < 1<<20; steps++ {
		nl, nr, ok := s.stepPos(line, r, dir)
		if !ok || s.ctxAt(nl, nr)&jsf.CtxString == 0 {
			break
		}
		line, r = nl, nr
	}
	return line, r
}

// quoteMatch matches a quote character sitting at either END of a string
// region (by context) to the quote at the other end. applies=false when the
// caret's quote has no string context, or is neither end (an escaped quote
// mid-string): other matchers then take over.
func (s *matchScanner) quoteMatch(line, r int) (int, int, bool, bool) {
	if s.ctxAt(line, r)&jsf.CtxString == 0 {
		return 0, 0, false, false
	}
	pl, pr, pok := s.stepPos(line, r, -1)
	nl, nr, nok := s.stepPos(line, r, +1)
	atStart := !pok || s.ctxAt(pl, pr)&jsf.CtxString == 0
	atEnd := !nok || s.ctxAt(nl, nr)&jsf.CtxString == 0
	switch {
	case atStart && atEnd:
		return 0, 0, false, false // a lone quote: nothing to mate with
	case atStart:
		el, er := s.stringRegionEnd(line, r, +1)
		return el, er, isQuoteRune(s.runesAt(el)[er]), true
	case atEnd:
		sl, sr := s.stringRegionEnd(line, r, -1)
		return sl, sr, isQuoteRune(s.runesAt(sl)[sr]), true
	}
	return 0, 0, false, false
}

// ---------------------------------------------------------------------------
// Token pairs ([match.<grammar>])
// ---------------------------------------------------------------------------

// tokenAt reports whether one of tokens covers position r of runes, with
// word boundaries respected. Longer tokens win.
func tokenAt(runes []rune, r int, tokens []string) (string, int) {
	best, bestStart := "", -1
	for _, tok := range tokens {
		t := []rune(tok)
		for start := r - len(t) + 1; start <= r; start++ {
			if tokenMatches(runes, start, t) && len(tok) > len(best) {
				best, bestStart = tok, start
			}
		}
	}
	return best, bestStart
}

// tokenMatches tests a boundary-respecting occurrence of t at start: an edge
// that is a word rune must not touch another word rune.
func tokenMatches(runes []rune, start int, t []rune) bool {
	if start < 0 || start+len(t) > len(runes) {
		return false
	}
	for i, c := range t {
		if runes[start+i] != c {
			return false
		}
	}
	if isWordRune(t[0]) && start > 0 && isWordRune(runes[start-1]) {
		return false
	}
	if isWordRune(t[len(t)-1]) && start+len(t) < len(runes) && isWordRune(runes[start+len(t)]) {
		return false
	}
	return true
}

// tokenMatch matches a [match.<grammar>] token at the caret. applies=false
// means the caret is on no known token (fall through to the final warning).
func (s *matchScanner) tokenMatch(line, r int, pairs map[string]string) (int, int, bool, bool) {
	if len(pairs) == 0 {
		return 0, 0, false, false
	}
	var all []string
	closerOf := map[string]string{}
	isCloser := map[string]bool{}
	for open, close := range pairs {
		if open == "tags" {
			continue
		}
		closerOf[open] = close
		isCloser[close] = true
		all = append(all, open, close)
	}
	tok, start := tokenAt(s.runesAt(line), r, all)
	if tok == "" {
		return 0, 0, false, false
	}
	startCtx := s.ctxAt(line, start)

	// The token's nesting family: matching from an opener counts every
	// opener that shares its closer; from a closer, every opener closing
	// with it.
	var closer string
	forward := false
	if c, isOpen := closerOf[tok]; isOpen {
		closer, forward = c, true
	} else {
		closer = tok
	}
	var openers []string
	for open, c := range closerOf {
		if c == closer {
			openers = append(openers, open)
		}
	}
	family := append(append([]string{}, openers...), closer)

	depth := 1
	advance := func(ln, from, to int) (int, int, bool) {
		runes := s.runesAt(ln)
		occs := tokenOccurrences(runes, family)
		if !forward {
			for i := len(occs) - 1; i >= 0; i-- {
				o := occs[i]
				if o.start < from || o.start > to || s.ctxAt(ln, o.start) != startCtx {
					continue
				}
				if o.tok == closer && !contains(openers, o.tok) {
					depth++
				} else if depth--; depth == 0 {
					return ln, o.start, true
				}
			}
			return 0, 0, false
		}
		for _, o := range occs {
			if o.start < from || o.start > to || s.ctxAt(ln, o.start) != startCtx {
				continue
			}
			if o.tok == closer {
				if depth--; depth == 0 {
					return ln, o.start, true
				}
			} else {
				depth++
			}
		}
		return 0, 0, false
	}

	if forward {
		from := start + len([]rune(tok))
		for ln := line; ln < s.lineCount; ln++ {
			if l, p, ok := advance(ln, from, 1<<30); ok {
				return l, p, true, true
			}
			from = 0
		}
		return 0, 0, false, true
	}
	to := start - 1
	for ln := line; ln >= 0; ln-- {
		if l, p, ok := advance(ln, 0, to); ok {
			return l, p, true, true
		}
		to = 1 << 30
	}
	return 0, 0, false, true
}

type tokenOcc struct {
	tok   string
	start int
}

// tokenOccurrences lists boundary-respecting occurrences in order, longest
// token winning at each position.
func tokenOccurrences(runes []rune, tokens []string) []tokenOcc {
	var out []tokenOcc
	for i := 0; i < len(runes); {
		best := ""
		for _, tok := range tokens {
			if len(tok) > len(best) && tokenMatches(runes, i, []rune(tok)) {
				best = tok
			}
		}
		if best == "" {
			i++
			continue
		}
		out = append(out, tokenOcc{best, i})
		i += len([]rune(best))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// HTML-style tags (tags = true)
// ---------------------------------------------------------------------------

// angleMatch pairs a "<" with the next ">" in the same context (and a ">"
// with the nearest "<" before it): the two ends of one tag.
func (s *matchScanner) angleMatch(line, r int) (int, int, bool) {
	c := s.runesAt(line)[r]
	startCtx := s.ctxAt(line, r)
	if c == '<' {
		from := r + 1
		for ln := line; ln < s.lineCount; ln++ {
			runes := s.runesAt(ln)
			for i := from; i < len(runes); i++ {
				if runes[i] == '>' && s.ctxAt(ln, i) == startCtx {
					return ln, i, true
				}
			}
			from = 0
		}
		return 0, 0, false
	}
	to := r - 1
	for ln := line; ln >= 0; ln-- {
		runes := s.runesAt(ln)
		if to >= len(runes) {
			to = len(runes) - 1
		}
		for i := to; i >= 0; i-- {
			if runes[i] == '<' && s.ctxAt(ln, i) == startCtx {
				return ln, i, true
			}
			// A same-context ">" before any "<" means another tag ended
			// first: unbalanced. (A quoted attribute ">" has string
			// context and does not count.)
			if runes[i] == '>' && s.ctxAt(ln, i) == startCtx && !(ln == line && i == r) {
				return 0, 0, false
			}
		}
		if ln > 0 {
			to = len(s.runesAt(ln-1)) - 1
		}
	}
	return 0, 0, false
}

// voidElements never pair with a closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}

// tagParse reads a tag at a "<" position: its name, whether it opens or
// closes, and whether it self-closes ("/>" before the line's end).
func (s *matchScanner) tagParse(line, lt int) (name string, closing, selfClosing bool) {
	runes := s.runesAt(line)
	i := lt + 1
	if i < len(runes) && runes[i] == '/' {
		closing = true
		i++
	}
	start := i
	for i < len(runes) && (isWordRune(runes[i]) || runes[i] == '-') {
		i++
	}
	name = strings.ToLower(string(runes[start:i]))
	for j := i; j < len(runes); j++ {
		if runes[j] == '>' {
			selfClosing = j > lt && runes[j-1] == '/'
			break
		}
		if runes[j] == '<' {
			break
		}
	}
	return name, closing, selfClosing
}

// tagMatch matches the tag surrounding (line, r). applies=false when the
// caret is not inside a tag.
func (s *matchScanner) tagMatch(line, r int) (int, int, bool, bool) {
	runes := s.runesAt(line)
	if len(runes) == 0 {
		return 0, 0, false, false
	}
	if r >= len(runes) {
		r = len(runes) - 1
	}
	// The enclosing tag's "<": scan left without crossing a ">" (the caret
	// may sit on the tag's own ">").
	lt := -1
	for i := r; i >= 0; i-- {
		if runes[i] == '<' {
			lt = i
			break
		}
		if runes[i] == '>' && i != r {
			return 0, 0, false, false
		}
	}
	if lt < 0 {
		return 0, 0, false, false
	}
	name, closing, selfClosing := s.tagParse(line, lt)
	if name == "" || voidElements[name] || (!closing && selfClosing) {
		return 0, 0, false, false
	}
	startCtx := s.ctxAt(line, lt)

	depth := 1
	scanLine := func(ln, from, to int, backward bool) (int, bool) {
		runes := s.runesAt(ln)
		var hits []int
		for i, c := range runes {
			if c == '<' && i >= from && i <= to && s.ctxAt(ln, i) == startCtx {
				hits = append(hits, i)
			}
		}
		if backward {
			for k := len(hits) - 1; k >= 0; k-- {
				if p, ok := s.tagStep(ln, hits[k], name, closing, &depth); ok {
					return p, true
				}
			}
		} else {
			for _, h := range hits {
				if p, ok := s.tagStep(ln, h, name, closing, &depth); ok {
					return p, true
				}
			}
		}
		return 0, false
	}

	// Jumps land one rune past the matched "<": on the tag name of an
	// opening tag, or on the "/" of a closing one.
	if !closing {
		from := lt + 1
		for ln := line; ln < s.lineCount; ln++ {
			if p, ok := scanLine(ln, from, 1<<30, false); ok {
				return ln, p + 1, true, true
			}
			from = 0
		}
		return 0, 0, false, true
	}
	to := lt - 1
	for ln := line; ln >= 0; ln-- {
		if p, ok := scanLine(ln, 0, to, true); ok {
			return ln, p + 1, true, true
		}
		to = 1 << 30
	}
	return 0, 0, false, true
}

// tagStep updates the nesting depth for the tag at (line, lt); reports the
// match position when depth reaches zero. fromClosing flips which side
// increments.
func (s *matchScanner) tagStep(line, lt int, name string, fromClosing bool, depth *int) (int, bool) {
	n, closing, selfClosing := s.tagParse(line, lt)
	if n != name || (!closing && selfClosing) {
		return 0, false
	}
	if closing != fromClosing {
		*depth--
		if *depth == 0 {
			return lt, true
		}
	} else {
		*depth++
	}
	return 0, false
}

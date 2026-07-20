package editor

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// farCol is a column sentinel meaning "the far end of the line"; search
// bounds clip it to the actual line length.
const farCol = 1 << 30

// findOptions is the parsed form of the find command's option letters,
// following JOE's search options.
type findOptions struct {
	ignoreCase bool // i - case-insensitive match
	backwards  bool // b - search toward the start of the document
	allBuffers bool // a - continue across all main buffers
	replace    bool // r - replace mode (prompts for the replacement)
	stdSyntax  bool // x - standard regular-expression syntax
	joeSyntax  bool // y - JOE regex syntax (overrides the searchRegex default)
	verbose    bool // v - send regex debug information to the verbose log
	count      int  // nnn - find the Nth occurrence / limit replacements to N
}

// parseFindOptions parses the option-letter argument of the find command.
// Letters are case-insensitive; digits accumulate into the count; spaces and
// commas are ignored.
func parseFindOptions(s string) (findOptions, error) {
	var o findOptions
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			o.count = o.count*10 + int(r-'0')
			continue
		}
		switch unicode.ToLower(r) {
		case 'i':
			o.ignoreCase = true
		case 'b':
			o.backwards = true
		case 'a':
			o.allBuffers = true
		case 'r':
			o.replace = true
		case 'x':
			o.stdSyntax = true
		case 'y':
			o.joeSyntax = true
		case 'v':
			o.verbose = true
		case ' ', ',':
			// separators are tolerated
		default:
			return o, fmt.Errorf("unknown find option %q (valid: i, b, a, r, x, y, v, nnn)", string(r))
		}
	}
	return o, nil
}

// resolveTargetMain returns the main-buffer window a command should operate
// on: the focused main buffer, the focused prompt's spawning main, else the
// last main buffer.
func (e *Editor) resolveTargetMain() *window.Window {
	w := e.WindowManager.GetFocusedWindow()
	if w != nil {
		if w.Type == window.MainBuffer {
			return w
		}
		if w.ParentMain != nil {
			return w.ParentMain
		}
	}
	return e.WindowManager.GetLastMainBufferWindow()
}

// matchInfo describes one located match.
type matchInfo struct {
	w      *window.Window
	line   int
	col    int      // rune column of the match start
	length int      // rune length of the matched text
	text   string   // the matched text
	groups []string // capture groups \1-\9 (regex matches)
	// wrapped records that this match was found only after the scan started
	// over from the far end of the buffer (or, in all-buffers mode, revisited
	// the origin buffer) — the trigger for the "continued from top/bottom"
	// notification.
	wrapped bool
}

// matcher matches a search term against lines: a fast literal scan when the
// term has no special sequences, else a compiled regular expression built
// from JOE or standard syntax.
type matcher struct {
	literal    []rune         // non-nil selects the literal fast path
	re         *regexp.Regexp // compiled pattern otherwise
	ignoreCase bool
	debug      string // translated Go pattern, for the verbose log
}

// buildMatcher compiles the search term under the effective syntax: the x
// option selects standard regex syntax, y selects JOE syntax (overriding the
// searchRegex default), and otherwise the default applies. In JOE syntax a
// term containing no backslash sequences is matched literally.
func buildMatcher(term string, opts findOptions, stdDefault bool) (matcher, error) {
	useStd := stdDefault
	if opts.stdSyntax {
		useStd = true
	}
	if opts.joeSyntax {
		useStd = false
	}

	if !useStd && !strings.Contains(term, "\\") && !strings.HasPrefix(term, "^") && !strings.HasSuffix(term, "$") {
		// JOE syntax with no special sequences: plain literal scan.
		return matcher{literal: []rune(term), ignoreCase: opts.ignoreCase, debug: "(literal)"}, nil
	}

	var pattern string
	var err error
	if useStd {
		pattern, err = translateStdPattern(term)
	} else {
		pattern, err = translateJoePattern(term)
	}
	if err != nil {
		return matcher{}, err
	}
	if opts.ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return matcher{}, fmt.Errorf("bad search pattern: %v", err)
	}
	return matcher{re: re, ignoreCase: opts.ignoreCase, debug: pattern}, nil
}

// translateJoePattern converts a JOE-syntax pattern to Go regexp syntax:
// operators are backslash-escaped in JOE (\* \+ \? \{m,n\} \| \( \) \[..\]
// \.) and become the bare Go operators; ^ and $ anchor at the pattern's
// start/end; \< and \> become word boundaries; everything else is literal.
func translateJoePattern(term string) (string, error) {
	var b strings.Builder
	runes := []rune(term)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			n := runes[i+1]
			i++
			switch n {
			case '*', '+', '?', '|', '(', ')', '{', '}', '.':
				b.WriteRune(n)
			case '[':
				// Character class: copy content raw until \].
				b.WriteRune('[')
				i++
				closed := false
				for ; i < len(runes); i++ {
					if runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == ']' {
						b.WriteRune(']')
						i++
						closed = true
						break
					}
					b.WriteRune(runes[i])
				}
				if !closed {
					return "", fmt.Errorf(`unterminated \[...\] character class`)
				}
			case ']':
				b.WriteString(`\]`)
			case '\\':
				b.WriteString(`\\`)
			case '<', '>':
				b.WriteString(`\b`)
			case 'n':
				return "", fmt.Errorf(`\n in search patterns is not supported (matches cannot span lines)`)
			case '!':
				return "", fmt.Errorf(`\! (balanced expression) is not supported`)
			default:
				b.WriteString(regexp.QuoteMeta(string(n)))
			}
			continue
		}
		switch {
		case r == '^' && i == 0:
			b.WriteRune('^')
		case r == '$' && i == len(runes)-1:
			b.WriteRune('$')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String(), nil
}

// translateStdPattern converts a standard-syntax pattern (the x option:
// . * + ? { } ( ) | ^ $ [ are special unescaped) to Go regexp syntax, which
// it already largely is: JOE's \< \> word boundaries map to \b, and the
// unsupported \n and \! are rejected.
func translateStdPattern(term string) (string, error) {
	var b strings.Builder
	runes := []rune(term)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			n := runes[i+1]
			switch n {
			case '<', '>':
				b.WriteString(`\b`)
			case 'n':
				return "", fmt.Errorf(`\n in search patterns is not supported (matches cannot span lines)`)
			case '!':
				return "", fmt.Errorf(`\! (balanced expression) is not supported`)
			default:
				b.WriteRune(r)
				b.WriteRune(n)
			}
			i++
			continue
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// findInLine locates a match whose start column lies within [minCol, maxCol]:
// the first such match scanning forward, the last scanning backwards.
func (m matcher) findInLine(lineRunes []rune, minCol, maxCol int, backwards bool) (int, int, string, []string, bool) {
	if minCol < 0 {
		minCol = 0
	}
	if m.literal != nil {
		if limit := len(lineRunes) - len(m.literal); maxCol > limit {
			maxCol = limit
		}
		if backwards {
			for col := maxCol; col >= minCol; col-- {
				if literalMatchAt(lineRunes, m.literal, col, m.ignoreCase) {
					return col, len(m.literal), string(lineRunes[col : col+len(m.literal)]), nil, true
				}
			}
		} else {
			for col := minCol; col <= maxCol; col++ {
				if literalMatchAt(lineRunes, m.literal, col, m.ignoreCase) {
					return col, len(m.literal), string(lineRunes[col : col+len(m.literal)]), nil, true
				}
			}
		}
		return 0, 0, "", nil, false
	}

	lineStr := string(lineRunes)
	// Byte offset of each rune, plus the end sentinel, for byte->rune mapping.
	offsets := make([]int, 0, len(lineRunes)+1)
	for idx := range lineStr {
		offsets = append(offsets, idx)
	}
	offsets = append(offsets, len(lineStr))
	byteToRune := func(b int) int {
		for i, off := range offsets {
			if off == b {
				return i
			}
			if off > b {
				return i - 1
			}
		}
		return len(lineRunes)
	}

	all := m.re.FindAllStringSubmatchIndex(lineStr, -1)
	var best []int
	bestCol := -1
	for _, sm := range all {
		col := byteToRune(sm[0])
		if col < minCol || col > maxCol {
			continue
		}
		if !backwards {
			best, bestCol = sm, col
			break
		}
		best, bestCol = sm, col // keep scanning: last one wins
	}
	if best == nil {
		return 0, 0, "", nil, false
	}
	endCol := byteToRune(best[1])
	text := lineStr[best[0]:best[1]]
	var groups []string
	for g := 1; g*2 < len(best); g++ {
		if best[g*2] >= 0 {
			groups = append(groups, lineStr[best[g*2]:best[g*2+1]])
		} else {
			groups = append(groups, "")
		}
	}
	return bestCol, endCol - bestCol, text, groups, true
}

// literalMatchAt reports whether lit matches lineRunes at start col.
func literalMatchAt(lineRunes, lit []rune, col int, ignoreCase bool) bool {
	for i, tr := range lit {
		lr := lineRunes[col+i]
		if ignoreCase {
			if unicode.ToLower(lr) != unicode.ToLower(tr) {
				return false
			}
		} else if lr != tr {
			return false
		}
	}
	return true
}

// searchBufferDir scans buf for the matcher. (fromLine, fromCol) is the
// first allowed match-start position in the scan direction (inclusive); the
// scan runs to the corresponding end of the buffer without wrapping.
func searchBufferDir(buf *buffer.Buffer, m matcher, fromLine, fromCol int, backwards bool) (matchInfo, bool) {
	lineCount := buf.GetLineCount()
	if backwards {
		if fromLine >= lineCount {
			fromLine = lineCount - 1
			fromCol = farCol
		}
		for line := fromLine; line >= 0; line-- {
			lineRunes := []rune(strings.TrimRight(buf.GetLine(line), "\n\r"))
			maxCol := farCol
			if line == fromLine {
				maxCol = fromCol
			}
			if maxCol < 0 {
				continue
			}
			if col, length, text, groups, ok := m.findInLine(lineRunes, 0, maxCol, true); ok {
				return matchInfo{line: line, col: col, length: length, text: text, groups: groups}, true
			}
		}
		return matchInfo{}, false
	}
	if fromLine < 0 {
		fromLine = 0
		fromCol = 0
	}
	for line := fromLine; line < lineCount; line++ {
		lineRunes := []rune(strings.TrimRight(buf.GetLine(line), "\n\r"))
		minCol := 0
		if line == fromLine {
			minCol = fromCol
		}
		if col, length, text, groups, ok := m.findInLine(lineRunes, minCol, farCol, false); ok {
			return matchInfo{line: line, col: col, length: length, text: text, groups: groups}, true
		}
	}
	return matchInfo{}, false
}

// findFrom locates the next match at or after (line, col) in the scan
// direction, starting in startW. In all-buffers mode the search continues
// through the other main buffers (and, when wrapping, back around to the
// origin); otherwise it stays in startW, restarting from the far end when
// allowWrap is set.
func (e *Editor) findFrom(m matcher, opts findOptions, startW *window.Window, line, col int, allowWrap bool) (matchInfo, bool) {
	if !opts.allBuffers {
		if startW == nil || startW.Buffer == nil {
			return matchInfo{}, false
		}
		if mi, ok := searchBufferDir(startW.Buffer, m, line, col, opts.backwards); ok {
			mi.w = startW
			return mi, true
		}
		if allowWrap {
			fl, fc := farStart(startW.Buffer, opts.backwards)
			if mi, ok := searchBufferDir(startW.Buffer, m, fl, fc, opts.backwards); ok {
				mi.w = startW
				mi.wrapped = true
				return mi, true
			}
		}
		return matchInfo{}, false
	}

	// All-buffers mode: walk the ring of main buffers starting at startW.
	mains := e.getMainBuffers()
	if len(mains) == 0 {
		return matchInfo{}, false
	}
	start := 0
	if startW != nil {
		for i, w := range mains {
			if w.ID == startW.ID {
				start = i
				break
			}
		}
	}
	n := len(mains)
	segments := n
	if allowWrap {
		segments = n + 1 // revisit the origin buffer once when wrapping
	}
	for i := 0; i < segments; i++ {
		var idx int
		if opts.backwards {
			idx = ((start-i)%n + n) % n
		} else {
			idx = (start + i) % n
		}
		w := mains[idx]
		if w.Buffer == nil {
			continue
		}
		fromLine, fromCol := line, col
		if i != 0 {
			fromLine, fromCol = farStart(w.Buffer, opts.backwards)
		}
		if mi, ok := searchBufferDir(w.Buffer, m, fromLine, fromCol, opts.backwards); ok {
			mi.w = w
			mi.wrapped = i == n // the extra revisit segment: back in the origin buffer
			return mi, true
		}
	}
	return matchInfo{}, false
}

// farStart returns the scan start position for a fresh pass over a buffer:
// its beginning for forward searches, its end for backward ones.
func farStart(buf *buffer.Buffer, backwards bool) (int, int) {
	if backwards {
		return buf.GetLineCount() - 1, farCol
	}
	return 0, 0
}

// moveToMatch places the cursor on a match, switching focus first when the
// match lives in another window (all-buffers searches).
func (e *Editor) moveToMatch(w *window.Window, line, col int) {
	if fw := e.WindowManager.GetFocusedWindow(); fw == nil || fw.ID != w.ID {
		e.WindowManager.SetFocus(w.ID)
		e.announceFocusedWindow()
	}
	w.SetCursorPos(window.Position{Line: line, Rune: col})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// findStep advances the cursor to the count'th match after the current
// cursor position (before it, for backwards searches). A single-occurrence
// step wraps when allowWrap and the searchWrap option permit; occurrence
// counting (count > 1) never wraps.
func (e *Editor) findStep(state window.FindState, count int, allowWrap bool) bool {
	opts, err := parseFindOptions(state.Options)
	if err != nil {
		e.ShowWarning(err.Error())
		return false
	}
	w := e.resolveTargetMain()
	if w == nil || w.Buffer == nil {
		return false
	}
	opts.ignoreCase = opts.ignoreCase || e.optBool(w, "searchignorecase", e.Config.SearchIgnoreCase)
	m, err := buildMatcher(state.Term, opts, e.optBool(w, "searchregex", e.Config.SearchRegex))
	if err != nil {
		e.ShowWarning(err.Error())
		return false
	}
	if count < 1 {
		count = 1
	}

	line, col := w.CursorPos().Line, w.CursorPos().Rune+1
	if opts.backwards {
		col = w.CursorPos().Rune - 1
		if col < 0 {
			line, col = line-1, farCol
		}
	}

	var last matchInfo
	cur := w
	searchWrap := e.optBool(w, "searchwrap", e.Config.SearchWrap)
	for k := 0; k < count; k++ {
		wrapThis := allowWrap && searchWrap && count == 1
		mi, ok := e.findFrom(m, opts, cur, line, col, wrapThis)
		if !ok {
			return false
		}
		last = mi
		cur = mi.w
		if opts.backwards {
			line, col = mi.line, mi.col-1
			if col < 0 {
				line, col = line-1, farCol
			}
		} else {
			adv := mi.length
			if adv < 1 {
				adv = 1
			}
			line, col = mi.line, mi.col+adv
		}
	}
	e.moveToMatch(last.w, last.line, last.col)
	e.announceFindWrap(last, opts.backwards)
	return true
}

// announceFindWrap surfaces the wrap-around state of a search after the caret
// has landed on a match. When the scan started over from the far end of the
// buffer it says so; once wrapped, the first match at or beyond the search's
// origin cursor (in the scan direction) announces that the search has looped a
// full circle, and the cycle resets so another full revolution announces
// again. Pre-wrap matches never trigger the crossing check — before wrapping,
// every forward match is trivially past the origin.
func (e *Editor) announceFindWrap(last matchInfo, backwards bool) {
	w := last.w
	if w == nil || w.Caret == nil {
		return
	}
	originByte, hasOrigin := w.FindOriginByte()
	if !hasOrigin {
		// No recorded origin (e.g. a scripted find_next with no committed
		// find): still announce the wrap itself.
		if last.wrapped {
			e.ShowNotification(wrapMessage(backwards))
		}
		return
	}
	matchByte := w.Caret.BytePos() // the caret was just parked on the match
	crossed := matchByte >= originByte
	if backwards {
		crossed = matchByte <= originByte
	}
	switch {
	case (last.wrapped || w.FindWrapped()) && crossed:
		e.ShowNotification("Search has looped")
		w.SetFindWrapped(false)
	case last.wrapped:
		e.ShowNotification(wrapMessage(backwards))
		w.SetFindWrapped(true)
	}
}

// wrapMessage is the notification for a scan that started over at the far end.
func wrapMessage(backwards bool) string {
	if backwards {
		return "Search continued from bottom"
	}
	return "Search continued from top"
}

// currentFindState returns the find state find_next should continue from:
// the editor-wide state when an all-buffers ("a") find is active, else the
// target main buffer's own state.
func (e *Editor) currentFindState() window.FindState {
	if e.globalFindSet {
		return e.globalFind
	}
	if w := e.resolveTargetMain(); w != nil {
		return w.Find
	}
	return window.FindState{}
}

// startFind runs the find command. Missing pieces are gathered through
// prompts, but only when necessary: the term whenever absent; the options
// only on a fully interactive invocation (no arguments at all); the
// replacement only when replace mode is on. Explicit arguments never prompt.
//
// The find prompt starts empty and shows the previous term in parentheses;
// accepting it blank repeats that previous search.
func (e *Editor) startFind(term, options, replacement string, haveTerm, haveOptions, haveReplacement bool) {
	prev := e.currentFindState()

	if !haveTerm || term == "" {
		label := "Find: "
		if prev.Term != "" {
			label = "Find (" + truncateHint(prev.Term, 24) + "): "
		}
		e.PromptMgr.PromptForInput(label, "", func(accepted bool, _, text string) {
			if accepted {
				if text == "" {
					text = prev.Term
				}
				if text != "" {
					if haveOptions {
						e.continueFind(text, options, replacement, haveReplacement)
					} else {
						e.promptFindOptions(text, replacement, haveReplacement)
					}
				}
			}
			e.RequestRender()
		}, "find")
		return
	}
	e.continueFind(term, options, replacement, haveReplacement)
}

// truncateHint shortens a prompt-label hint to at most max runes so a long
// previous term cannot squeeze out the input area.
func truncateHint(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// promptFindOptions asks for the option letters during an interactive find.
// The prompt starts blank every time — options describe this search, not the
// last one — but earlier entries stay reachable by arrow through history.
func (e *Editor) promptFindOptions(term, replacement string, haveReplacement bool) {
	e.PromptMgr.PromptForInput("Options (i,b,a,r,x,y,v,nnn): ", "", func(accepted bool, _, text string) {
		if accepted {
			e.continueFind(term, text, replacement, haveReplacement)
		}
		e.RequestRender()
	}, "findoptions")
}

// continueFind validates the options and gathers the replacement when
// replace mode needs one, then commits.
func (e *Editor) continueFind(term, options, replacement string, haveReplacement bool) {
	opts, err := parseFindOptions(options)
	if err != nil {
		e.ShowWarning(err.Error())
		return
	}
	replaceMode := opts.replace || haveReplacement
	if replaceMode && !haveReplacement {
		// The replacement prompt keeps history (arrow up recalls earlier
		// replacements) but starts blank, and blank is honored as a genuine
		// empty replacement (matches are deleted) rather than assuming the
		// previous one.
		e.PromptMgr.PromptForInput("Replace with: ", "", func(accepted bool, _, text string) {
			if accepted {
				e.commitFind(term, options, text, true)
			}
			e.RequestRender()
		}, "replace")
		return
	}
	e.commitFind(term, options, replacement, replaceMode)
}

// commitFind stores the find state — on the target main buffer window, or
// editor-wide for an all-buffers search — and starts the search or the
// interactive replace loop.
func (e *Editor) commitFind(term, options, replacement string, replaceMode bool) {
	state := window.FindState{Term: term, Options: options, Replacement: replacement, Replace: replaceMode}
	opts, _ := parseFindOptions(options)
	tw := e.resolveTargetMain()
	searchRegex := e.optBool(tw, "searchregex", e.Config.SearchRegex)
	opts.ignoreCase = opts.ignoreCase || e.optBool(tw, "searchignorecase", e.Config.SearchIgnoreCase)

	m, err := buildMatcher(term, opts, searchRegex)
	if err != nil {
		e.ShowWarning(err.Error())
		return
	}
	if opts.verbose {
		syntax := "joe"
		if (searchRegex || opts.stdSyntax) && !opts.joeSyntax {
			syntax = "standard"
		}
		e.appendVerboseLog(fmt.Sprintf("find: term=%q options=%q syntax=%s pattern=%s ignoreCase=%v backwards=%v count=%d replace=%v replacement=%q",
			term, options, syntax, m.debug, opts.ignoreCase, opts.backwards, opts.count, replaceMode, replacement))
	}

	if opts.allBuffers {
		e.globalFind = state
		e.globalFindSet = true
	} else {
		if w := e.resolveTargetMain(); w != nil {
			w.Find = state
		}
		e.globalFindSet = false
	}

	// Park the search-origin cursor where this search begins (the caret), so
	// wrap-around and full-loop crossings can be announced as find_next walks
	// on from here. A garland cursor, so edits slide it with the text.
	if w := e.resolveTargetMain(); w != nil {
		w.SetFindOrigin()
	}

	if replaceMode {
		w := e.resolveTargetMain()
		if w == nil || w.Buffer == nil {
			return
		}
		if opts.count > 0 {
			// JOE: a count combined with r performs exactly that many
			// replacements without asking about each one.
			mi, ok := e.findFrom(m, opts, w, w.CursorPos().Line, w.CursorPos().Rune, false)
			total := 0
			if ok {
				total = e.replaceRest(state, opts, m, mi, 0)
			}
			if opts.verbose {
				e.appendVerboseLog(fmt.Sprintf("replace done: %d replaced", total))
			}
			e.ShowNotification(fmt.Sprintf("Replace done: %d replaced", total))
			e.RequestRender()
			return
		}
		// The interactive replace loop scans from the cursor inclusively (a
		// match under the cursor is offered) and does not wrap.
		e.runReplaceLoop(state, opts, m, w, w.CursorPos().Line, w.CursorPos().Rune, 0)
		return
	}
	if !e.findStep(state, opts.count, true) {
		e.ShowNotification("Not found: " + term)
	}
}

// runReplaceLoop steps through matches interactively: y replaces, n skips,
// a replaces the rest without asking, q (or cancel) stops. (line, col) is
// the next allowed match start, inclusive. The loop runs from there to the
// end of the scan direction without wrapping — across the remaining main
// buffers when the "a" option is active. (A count with r never reaches this
// loop — commitFind replaces that many outright — but the count guard is
// kept for safety.)
//
// Each offered match is marked with the transient match marks
// (_match_begin/_match_end), which the renderer shows as the selection in
// place of the user's block while the prompt is open; the block marks
// themselves are untouched and repaint when the loop ends.
func (e *Editor) runReplaceLoop(state window.FindState, opts findOptions, m matcher, w *window.Window, line, col int, replaced int) {
	finish := func(fw *window.Window) {
		clearMatchHighlight(fw)
		if opts.verbose {
			e.appendVerboseLog(fmt.Sprintf("replace done: %d replaced", replaced))
		}
		e.ShowNotification(fmt.Sprintf("Replace done: %d replaced", replaced))
		e.RequestRender()
	}

	if opts.count > 0 && replaced >= opts.count {
		finish(w)
		return
	}
	mi, ok := e.findFrom(m, opts, w, line, col, false)
	if !ok {
		finish(w)
		return
	}
	if mi.w != w {
		// The search hopped to another buffer: clear the stale highlight
		// left behind in the previous one.
		clearMatchHighlight(w)
	}
	e.moveToMatch(mi.w, mi.line, mi.col)
	highlightMatch(mi.w, mi.line, mi.col, mi.length)
	e.RequestRender()

	e.PromptMgr.PromptForInput("Replace? (y/n/a/q): ", "", func(accepted bool, _, answer string) {
		defer e.RequestRender()
		if !accepted {
			finish(mi.w)
			return
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			if e.windowEditLocked(mi.w) {
				finish(mi.w)
				return
			}
			nl, nc := e.applyReplacement(state, mi)
			e.runReplaceLoop(state, opts, m, mi.w, nl, nc, replaced+1)
		case "n", "no":
			nl, nc := afterSkipPos(mi.line, mi.col, opts.backwards)
			e.runReplaceLoop(state, opts, m, mi.w, nl, nc, replaced)
		case "a", "all":
			clearMatchHighlight(mi.w)
			total := e.replaceRest(state, opts, m, mi, replaced)
			if opts.verbose {
				e.appendVerboseLog(fmt.Sprintf("replace done: %d replaced", total))
			}
			e.ShowNotification(fmt.Sprintf("Replace done: %d replaced", total))
		default: // q, quit, anything else: stop
			finish(mi.w)
		}
	}, "")
}

// replaceRest replaces the current match and every further match in the
// scan direction without asking, honoring the count limit, and returns the
// total replaced. Single-buffer runs collapse into one undo revision.
func (e *Editor) replaceRest(state window.FindState, opts findOptions, m matcher, mi matchInfo, replaced int) int {
	if e.windowEditLocked(mi.w) {
		return replaced
	}
	if !opts.allBuffers && mi.w.Buffer != nil {
		mi.w.Buffer.BeginUserCommand("replace")
		defer mi.w.Buffer.EndUserCommand()
	}

	for {
		nl, nc := e.applyReplacement(state, mi)
		replaced++
		if opts.count > 0 && replaced >= opts.count {
			e.moveToMatch(mi.w, mi.line, mi.col)
			return replaced
		}
		next, ok := e.findFrom(m, opts, mi.w, nl, nc, false)
		if !ok {
			e.moveToMatch(mi.w, mi.line, mi.col)
			return replaced
		}
		mi = next
	}
}

// applyReplacement splices the expanded replacement over the match and
// returns the next allowed match-start position in the scan direction.
func (e *Editor) applyReplacement(state window.FindState, mi matchInfo) (int, int) {
	opts, _ := parseFindOptions(state.Options)
	expanded := expandReplacement(state.Replacement, mi.text, mi.groups)

	content := strings.TrimRight(mi.w.Buffer.GetLine(mi.line), "\n\r")
	runes := []rune(content)
	col := mi.col
	if col > len(runes) {
		col = len(runes)
	}
	end := col + mi.length
	if end > len(runes) {
		end = len(runes)
	}
	// Localized replace: position at the match, delete its runes, insert the
	// expansion — all at the same point, so garland preserves and slides the
	// surrounding decorations (the user's block marks, other matches) instead
	// of collapsing the whole line's marks. Cursor-relative via a fresh seek.
	mi.w.Buffer.ReplaceText(mi.line, col, end-col, expanded)

	// Record the replacement in the window's cursor ring. A replace-all makes
	// many calls, but TrackEdit collapses them into at most one ring entry (only
	// the first, if the caret had moved, pushes the prior edit point). Not a
	// kill: breaks any delete accumulation.
	mi.w.TrackEdit()
	e.lastEditKill = false

	if opts.backwards {
		if col == 0 {
			return mi.line - 1, farCol
		}
		return mi.line, col - 1
	}
	// Forward: continue just past the inserted text; a replacement containing
	// line breaks moves the continuation down accordingly, and a zero-length
	// match advances one extra column to guarantee progress.
	expRunes := []rune(expanded)
	nl := strings.Count(expanded, "\n")
	contLine, contCol := mi.line, col+len(expRunes)
	if nl > 0 {
		lastSeg := expanded[strings.LastIndexByte(expanded, '\n')+1:]
		contLine = mi.line + nl
		contCol = len([]rune(lastSeg))
	}
	if mi.length == 0 {
		contCol++
	}
	return contLine, contCol
}

// expandReplacement applies JOE's replacement escapes: \& is the matched
// text, \1-\9 are capture groups, \l and \u case-convert the next character,
// \L and \U case-convert the rest, \n inserts a line break, and \\ is a
// literal backslash.
func expandReplacement(repl, matched string, groups []string) string {
	var b strings.Builder
	caseMode := rune(0) // 'L' or 'U' while active
	caseNext := rune(0) // 'l' or 'u' for one character

	emit := func(s string) {
		for _, r := range s {
			switch {
			case caseNext == 'l':
				r = unicode.ToLower(r)
				caseNext = 0
			case caseNext == 'u':
				r = unicode.ToUpper(r)
				caseNext = 0
			case caseMode == 'L':
				r = unicode.ToLower(r)
			case caseMode == 'U':
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
		}
	}

	runes := []rune(repl)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r != '\\' || i+1 >= len(runes) {
			emit(string(r))
			continue
		}
		n := runes[i+1]
		i++
		switch {
		case n == '&':
			emit(matched)
		case n >= '1' && n <= '9':
			idx := int(n - '1')
			if idx < len(groups) {
				emit(groups[idx])
			}
		case n == 'l' || n == 'u':
			caseNext = n
		case n == 'L' || n == 'U':
			caseMode = n
		case n == 'n':
			emit("\n")
		case n == '\\':
			emit("\\")
		default:
			emit(string(n))
		}
	}
	return b.String()
}

// afterSkipPos returns the next allowed match start after skipping a match
// at (line, col).
func afterSkipPos(line, col int, backwards bool) (int, int) {
	if backwards {
		if col == 0 {
			return line - 1, farCol
		}
		return line, col - 1
	}
	return line, col + 1
}

// highlightMatch marks a found match with the transient match marks so the
// renderer shows it highlighted while the replace prompt is open.
func highlightMatch(w *window.Window, line, col, termLen int) {
	if w == nil || w.Buffer == nil {
		return
	}
	w.Buffer.SetMark("_match_begin", line, col)
	w.Buffer.SetMark("_match_end", line, col+termLen)
	w.MatchHighlight = true
}

// clearMatchHighlight removes the replace loop's match highlight, letting
// the user's own block selection show again.
func clearMatchHighlight(w *window.Window) {
	if w == nil || w.Buffer == nil {
		return
	}
	w.Buffer.ClearMatchMarks()
	w.MatchHighlight = false
}

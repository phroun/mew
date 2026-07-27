package editor

import (
	"strings"

	"github.com/phroun/mew/internal/viewport"
)

// Incremental search in the JOE tradition. The search command opens an
// "I-search:" prompt whose after-key pseudo-binding (isearch_key) reacts to
// every keystroke that lands in it:
//
//   - each typed character extends the pattern and searches from the current
//     match start (inclusive) in the search's direction, so a match extends
//     in place while it still fits and the caret moves only when it must;
//   - backspace shortens the pattern and pops the position stack, returning
//     the caret to where the shorter pattern matched;
//   - find_next (its usual key) rotates to the next occurrence of the current
//     increment — the search state is committed to the ordinary find wiring
//     (term AND direction) on every change, so repetition works both during
//     the search (the caret moves while the prompt keeps the keyboard) and
//     after it;
//   - ^R / ^F (bound in the prompt's class keymap) set the direction —
//     reverse / forward — and step one occurrence that way; the direction is
//     the same "b" option the find machinery stores, so it starts from the
//     stored find state and persists back into it;
//   - Enter accepts: the prompt closes and the caret stays on the match;
//   - cancel (^C) closes the prompt and returns the caret to where the
//     search began.
//
// The current match is shown with the transient match highlight (the same
// marks the replace loop uses) while the prompt is open.

// isearchFrame records the match-start position that was current while the
// pattern had patLen runes — the position backspace returns to.
type isearchFrame struct {
	patLen int
	pos    viewport.Position
}

// isearchState is the live state of the open incremental-search prompt.
type isearchState struct {
	promptID  string
	targetID  string
	origin    viewport.Position  // caret when the search began
	prevFind  viewport.FindState // target's find state before the search
	stack     []isearchFrame     // positions to pop back to on backspace
	lastLen   int                // rune length of the pattern last seen
	backwards bool               // current direction (the find "b" option)
}

// isearchOptions renders the state's direction as find option letters.
func (st *isearchState) options() string {
	if st.backwards {
		return "b"
	}
	return ""
}

// isearchLabel is the prompt label for the current direction.
func (st *isearchState) label() string {
	if st.backwards {
		return "(F) I-search back: "
	}
	return "(F) I-search: "
}

// startIncrementalSearch is the search command: open the isearch prompt over
// the target main viewport. dir picks the initial direction: backwards when
// negative, forward when positive, and the direction stored in the target's
// find state (the "b" option) when zero.
func (e *Editor) startIncrementalSearch(dir int) bool {
	tw := e.resolveTargetMain()
	if tw == nil || tw.Buffer == nil {
		return false
	}
	st := &isearchState{
		targetID: tw.ID,
		origin:   tw.CursorPos(),
		prevFind: tw.Find,
	}
	switch {
	case dir < 0:
		st.backwards = true
	case dir > 0:
		st.backwards = false
	default:
		if opts, err := parseFindOptions(tw.Find.Options); err == nil {
			st.backwards = opts.backwards
		}
	}
	e.isearch = st

	// The wrap/loop announcements measure from where this search began.
	tw.SetFindOrigin()

	// Class "isearch" carries the prompt's keymap refinement (^R/^F) and any
	// class colors; history is neither preloaded nor wanted — the pattern
	// grows from empty, exactly one line.
	e.PromptMgr.promptForInputHist(st.label(), "", func(accepted bool, _, text string) {
		s := e.isearch
		e.isearch = nil
		target := e.ViewportManager.GetViewport(st.targetID)
		if target != nil {
			clearMatchHighlight(target)
		}
		if target == nil {
			e.RequestRender()
			return
		}
		if !accepted {
			// Abort: back to where the search began. The pattern stays in the
			// find state so find_next can still chase it later.
			target.SetCursorPos(st.origin)
			e.ensureCursorVisible(target)
		} else if s != nil && strings.TrimSpace(text) == "" {
			// Accepted empty: nothing was searched — restore the previous
			// find state; the caret never moved.
			target.Find = s.prevFind
		}
		// Accepted with a pattern: the caret already sits on the match and the
		// find state already carries the term and direction for find_next.
		e.RequestRender()
	}, "isearch", 1, "", false)

	// Arm the incremental classifier on the prompt just created.
	if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.Type == viewport.PromptViewport {
		w.AfterKey = "isearch_key"
		st.promptID = w.ID
	}
	return true
}

// isearchDirection is the search_reverse / search_forward command pair: with
// a search open, set its direction and step one occurrence that way; without
// one, start a search in that direction.
func (e *Editor) isearchDirection(back bool) bool {
	st := e.isearch
	if st == nil {
		if back {
			return e.startIncrementalSearch(-1)
		}
		return e.startIncrementalSearch(+1)
	}
	tw := e.ViewportManager.GetViewport(st.targetID)
	if tw == nil || tw.Buffer == nil {
		return false
	}
	st.backwards = back
	e.isearchRelabel(st)

	pattern := e.isearchPattern(st)
	tw.Find = viewport.FindState{Term: pattern, Options: st.options()}
	e.globalFindSet = false
	if pattern == "" {
		// Nothing to chase yet: the direction now set governs the first
		// typed character (and find_next, once there is a term).
		return true
	}
	// One occurrence in the chosen direction; the after-key hook re-syncs the
	// match highlight to wherever the caret lands.
	return e.findStep(tw.Find, 1, true)
}

// isearchPattern reads the current pattern line from the search prompt.
func (e *Editor) isearchPattern(st *isearchState) string {
	w := e.ViewportManager.GetViewport(st.promptID)
	if w == nil || w.Buffer == nil {
		return ""
	}
	return strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
}

// isearchRelabel repaints the prompt's label for the current direction.
func (e *Editor) isearchRelabel(st *isearchState) {
	w := e.ViewportManager.GetViewport(st.promptID)
	if w == nil {
		return
	}
	label := "(PI)" + st.label()
	w.RowMessages = []string{label}
	w.MarginInner = calculateAnsiAwareLength(label)
	e.RequestRender()
}

// isearchKeystroke is the isearch_key command: react to whatever the last
// keystroke did to the prompt's pattern line.
func (e *Editor) isearchKeystroke() bool {
	st := e.isearch
	if st == nil {
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.ID != st.promptID || w.Buffer == nil {
		return false
	}
	tw := e.ViewportManager.GetViewport(st.targetID)
	if tw == nil || tw.Buffer == nil {
		return false
	}

	pattern := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	plen := len([]rune(pattern))
	switch {
	case plen == st.lastLen:
		// Movement or a non-editing binding (find_next or a direction key
		// rotating the match): re-sync the highlight to wherever the caret
		// now stands.
		if plen > 0 {
			e.isearchRefreshHighlight(st, tw, pattern)
		}
		return true
	case plen > st.lastLen:
		// Extended: remember where the shorter pattern stood, then search for
		// the new one from the current match start (inclusive).
		st.stack = append(st.stack, isearchFrame{patLen: st.lastLen, pos: tw.CursorPos()})
		st.lastLen = plen
		e.isearchApply(st, tw, pattern, tw.CursorPos())
		return true
	default:
		// Shortened (backspace): pop back to the position the shorter pattern
		// had. Frames above the new length unwind; the deepest one popped is
		// the one recorded when the pattern was this length.
		st.lastLen = plen
		restored := false
		for len(st.stack) > 0 && st.stack[len(st.stack)-1].patLen >= plen {
			frame := st.stack[len(st.stack)-1]
			st.stack = st.stack[:len(st.stack)-1]
			tw.SetCursorPos(frame.pos)
			restored = true
		}
		if restored {
			e.ensureCursorVisible(tw)
		}
		if plen == 0 {
			clearMatchHighlight(tw)
			tw.Find = st.prevFind
			return true
		}
		e.isearchApply(st, tw, pattern, tw.CursorPos())
		return true
	}
}

// isearchApply commits pattern (and the direction) to the ordinary find state
// and parks the caret on its first match at or beyond from, scanning in the
// search's direction — no focus change; the prompt keeps the keyboard. A miss
// leaves the caret where it was and says so.
func (e *Editor) isearchApply(st *isearchState, tw *viewport.Viewport, pattern string, from viewport.Position) {
	// Committed on every change so find_next rotates the current increment.
	tw.Find = viewport.FindState{Term: pattern, Options: st.options()}
	e.globalFindSet = false

	opts := findOptions{backwards: st.backwards}
	opts.ignoreCase = e.optBool(tw, "searchignorecase", e.Config.SearchIgnoreCase)
	m, err := buildMatcher(pattern, opts, e.optBool(tw, "searchregex", e.Config.SearchRegex))
	if err != nil {
		e.ShowWarning(err.Error())
		return
	}
	mi, ok := e.findFrom(m, opts, tw, from.Line, from.Rune, false)
	if !ok {
		clearMatchHighlight(tw)
		e.ShowWarningTagged("Not found: "+pattern, "isearch_miss")
		return
	}
	tw.SetCursorPos(viewport.Position{Line: mi.line, Rune: mi.col})
	e.afterHorizontalMovement(tw)
	e.ensureCursorVisible(tw)
	highlightMatch(tw, mi.line, mi.col, mi.length)
}

// isearchRefreshHighlight re-marks the match under the caret after a rotation
// (find_next / a direction key) moved it, or clears the marks if the caret is
// not on a match of the pattern.
func (e *Editor) isearchRefreshHighlight(st *isearchState, tw *viewport.Viewport, pattern string) {
	var opts findOptions
	opts.ignoreCase = e.optBool(tw, "searchignorecase", e.Config.SearchIgnoreCase)
	m, err := buildMatcher(pattern, opts, e.optBool(tw, "searchregex", e.Config.SearchRegex))
	if err != nil {
		return
	}
	pos := tw.CursorPos()
	mi, ok := e.findFrom(m, opts, tw, pos.Line, pos.Rune, false)
	if ok && mi.line == pos.Line && mi.col == pos.Rune {
		highlightMatch(tw, mi.line, mi.col, mi.length)
	} else {
		clearMatchHighlight(tw)
	}
}

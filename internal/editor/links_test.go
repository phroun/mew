package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Content used across these tests: one dokuwiki link with a distinct target
// and title. Runes: "text " = 0..4, "[[a:b|Title]]" = 5..17, so the span is
// [5, 18) and the strict interior (where the caret counts as inside) is
// 5 < p < 18.
const linkLine = "text [[a:b|Title]] more\n"

var linkAnsiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripSGR(s string) string { return linkAnsiRe.ReplaceAllString(s, "") }

func linkEditor(t *testing.T, content string) (*Editor, *window.Window, *strings.Builder) {
	t.Helper()
	e, w, out := renderedEditorWithConfig(t, content, "[options]\nsyntax=dokuwiki\n")
	var sb strings.Builder
	_ = out
	_ = sb
	return e, w, nil
}

// Link spans extract from the dokuwiki grammar's Link-class runs, with the
// target/title split of [[target|Title]] and the target doubling as title
// for [[bare]] links.
func TestLinkSpanExtraction(t *testing.T) {
	e, w, _ := linkEditor(t, "text [[a:b|Title]] more\n[[bare]]\n")
	spans := e.linkSpansOnLine(w, 0)
	if len(spans) != 1 {
		t.Fatalf("line 0: want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Start != 5 || s.End != 18 {
		t.Fatalf("span range = [%d,%d), want [5,18)", s.Start, s.End)
	}
	if s.Target != "a:b" || s.Title != "Title" {
		t.Fatalf("target/title = %q/%q, want a:b/Title", s.Target, s.Title)
	}
	spans = e.linkSpansOnLine(w, 1)
	if len(spans) != 1 || spans[0].Target != "bare" || spans[0].Title != "bare" {
		t.Fatalf("line 1: want bare/bare, got %+v", spans)
	}
}

// Two links back-to-back (]][[  with no gap) extract as two separate spans,
// not one merged span — the grammar colors the whole run "Link".
func TestAdjacentLinkSpans(t *testing.T) {
	e, w, _ := linkEditor(t, "[[a]][[b]] and [[c]][[d]][[e]]\n")
	spans := e.linkSpansOnLine(w, 0)
	got := make([]string, len(spans))
	for i, s := range spans {
		got[i] = s.Target
	}
	want := []string{"a", "b", "c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("want %d links, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("link %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
	// The first two are adjacent: [[a]] = [0,5), [[b]] = [5,10).
	if spans[0].Start != 0 || spans[0].End != 5 || spans[1].Start != 5 || spans[1].End != 10 {
		t.Fatalf("adjacent spans should abut at 5: %+v %+v", spans[0], spans[1])
	}
}

// The "on a link" range is half-open [Start, End): the left edge (first
// character) counts, so entering from the left registers immediately; the
// position past the last character (End) does not.
func TestLinkEdgeBoundaries(t *testing.T) {
	// [[a:b|Title]] occupies [5, 18).
	e, w, _ := linkEditor(t, "text [[a:b|Title]] more\n")
	at := func(rune int) *linkSpan {
		w.SetCursorPos(window.Position{Line: 0, Rune: rune})
		return e.caretLinkSpan(w)
	}
	if at(4) != nil {
		t.Fatal("rune 4 (the space before) must be outside the link")
	}
	if s := at(5); s == nil || s.Target != "a:b" {
		t.Fatal("rune 5 (left edge, first '[') must count as on the link")
	}
	if s := at(17); s == nil { // the last ']'
		t.Fatal("rune 17 (last character) must be inside")
	}
	if at(18) != nil {
		t.Fatal("rune 18 (End, just past the link) must be outside")
	}
}

// A focused button protects the link's source text: content-mutating commands
// are rejected as though the buffer were read-only, and navigation/undo still
// work. Leaving the button (nav_cancel) restores editing.
func TestFocusedButtonReadOnly(t *testing.T) {
	e, w, _ := linkEditor(t, "text [[a:b|Title]] more\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 10}) // inside the link
	e.updateBrowseState()
	if e.focusedLinkButton(w) == nil {
		t.Fatal("button should be focused")
	}
	before := w.Buffer.GetLine(0)
	e.executeCommand("insert 'X'")
	if w.Buffer.GetLine(0) != before {
		t.Fatal("a focused button must reject content edits")
	}
	e.executeCommand("del_char_next")
	if w.Buffer.GetLine(0) != before {
		t.Fatal("a focused button must reject deletion")
	}
	// Navigation still works while focused.
	if !e.navFollow() {
		t.Fatal("nav_follow should still work on a focused button")
	}
	// nav_cancel leaves the button; editing resumes.
	if !e.navCancel() {
		t.Fatal("nav_cancel should disarm")
	}
	e.executeCommand("insert 'X'")
	if w.Buffer.GetLine(0) == before {
		t.Fatal("editing should resume after leaving the button")
	}
}

// Entering a link arms browse mode; nav_cancel disarms it and staying within
// the same span does not re-arm; leaving and re-entering re-arms.
func TestBrowseModeArming(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)

	w.SetCursorPos(window.Position{Line: 0, Rune: 2}) // outside
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("browse must not arm outside a link")
	}
	// The position just before the link (rune 4, the space) is still outside.
	w.SetCursorPos(window.Position{Line: 0, Rune: 4})
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("the cell before the link must not arm")
	}
	// The left edge (rune 5, the first '[') counts: the range is [Start, End).
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.updateBrowseState()
	if !w.BrowseActive {
		t.Fatal("the link's left edge should arm (entering from the left)")
	}

	// nav_cancel disarms; moving within the same span must not re-arm.
	if !e.navCancel() {
		t.Fatal("nav_cancel should succeed while armed")
	}
	if e.navCancel() {
		t.Fatal("nav_cancel should fail when not armed (chain falls through)")
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 12})
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("moving within the disarmed span must not re-arm")
	}

	// Leave, then re-enter: arms again.
	w.SetCursorPos(window.Position{Line: 0, Rune: 20})
	e.updateBrowseState()
	w.SetCursorPos(window.Position{Line: 0, Rune: 7})
	e.updateBrowseState()
	if !w.BrowseActive {
		t.Fatal("re-entering the span after leaving must re-arm")
	}
}

// Caret mode renders the raw link text in the link color; browse mode
// replaces it with a button (cap + title + cap + shadow) and the focused
// variant marks the button the caret occupies.
func TestLinkRenderStyles(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\n")

	// Caret mode: raw text, link color, no button chrome.
	out.Reset()
	e.performRender()
	raw := out.String()
	if !strings.Contains(raw, "\x1b[0;4;93;40m[") {
		t.Fatal("caret mode: link text should paint in the link color")
	}
	if strings.Contains(raw, "▐") {
		t.Fatal("caret mode: no button chrome expected")
	}

	// Browse mode, unfocused button (caret elsewhere): " Title " + shadow,
	// raw [[...]] gone from the painted line.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripSGR(out.String())
	if !strings.Contains(plain, " Title ▐") {
		t.Fatalf("browse mode: want ' Title ▐' button, got: %q", plain)
	}
	if strings.Contains(plain, "[[a:b") {
		t.Fatal("browse mode: raw link source must not paint")
	}
	if !strings.Contains(out.String(), "\x1b[0;30;47m") {
		t.Fatal("browse mode: button color expected")
	}

	// Focused button: caret inside the span switches caps/shadow/colors.
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	out.Reset()
	e.performRender()
	plain = stripSGR(out.String())
	if !strings.Contains(plain, "<Title>█") {
		t.Fatalf("focused button: want '<Title>█', got: %q", plain)
	}
	if !strings.Contains(out.String(), "\x1b[0;30;46m") {
		t.Fatal("focused button: buttonFocused color expected")
	}
}

// A focused button publishes its destination in the modebar context slot and
// accept activates it (transient notification; no newline inserted).
func TestFocusedLinkContextAndAccept(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	e.performRender()
	if w.Context != "a:b" {
		t.Fatalf("context should show the destination; got %q", w.Context)
	}

	lines := w.Buffer.GetLineCount()
	e.executeCommand("nav_follow|accept|insert '\\n'")
	if w.Buffer.GetLineCount() != lines {
		t.Fatal("nav_follow on a focused button must not insert a newline")
	}
	found := false
	for _, nw := range e.WindowManager.AllWindows() {
		if nw.Class == "notification" && strings.Contains(nw.MessageTopInner, "Link: a:b") {
			found = true
		}
	}
	if !found {
		t.Fatal("nav_follow should show a 'Link: <target>' notification")
	}

	// Disarmed (caret mode), the chain falls through nav_follow -> insert.
	if !e.navCancel() {
		t.Fatal("nav_cancel should disarm")
	}
	e.executeCommand("nav_follow|accept|insert '\\n'")
	if w.Buffer.GetLineCount() != lines+1 {
		t.Fatal("nav_follow in caret mode should fall through to insert")
	}
}

// An RTL title flows through the normal bidi machinery inside the button.
func TestLinkButtonRTLTitle(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, "x [[p|שלום]] y\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.updateBrowseState()
	if !w.BrowseActive {
		t.Fatal("caret inside the RTL-titled link should arm")
	}
	out.Reset()
	e.performRender()
	plain := stripSGR(out.String())
	for _, r := range "שלום" {
		if !strings.ContainsRune(plain, r) {
			t.Fatalf("RTL title rune %c missing from button", r)
		}
	}
	if !strings.Contains(plain, "█") {
		t.Fatal("focused shadow cell expected")
	}
}

// linkBrowsing=no disables the whole hyperlink layer: links render exactly
// as the grammar colors them (no link color, no arming, no buttons), and
// turning it off mid-browse retires the buttons immediately.
func TestLinkBrowsingGate(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, linkLine,
		"[options]\nsyntax=dokuwiki\nlinkBrowsing=no\n")
	out.Reset()
	e.performRender()
	raw := out.String()
	if strings.Contains(raw, "\x1b[0;4;93;40m") {
		t.Fatal("linkBrowsing=no: the link color must not paint")
	}
	if !strings.Contains(raw, sgrType+"[") {
		t.Fatal("linkBrowsing=no: links should keep their grammar syntax color")
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("linkBrowsing=no: entering a link must not arm")
	}

	// Turning the option off while armed retires the buttons.
	e2, w2, out2 := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\n")
	w2.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e2.updateBrowseState()
	if !w2.BrowseActive {
		t.Fatal("default: entering a link should arm")
	}
	if !e2.setOption(w2, "linkBrowsing", "no") {
		t.Fatal("set_option linkBrowsing no failed")
	}
	if w2.BrowseActive {
		t.Fatal("disabling linkBrowsing must retire browse mode")
	}
	out2.Reset()
	e2.performRender()
	if strings.Contains(stripSGR(out2.String()), "▐") {
		t.Fatal("no button chrome after disabling linkBrowsing")
	}
}

// nav_next / nav_prior cycle the caret between links, and capture only while
// a button is focused (so a fallthrough chain yields to editing otherwise).
func TestNavNextPrior(t *testing.T) {
	// Three links in document order: A (a:b) and B (b:c) on line 0, C (d:e)
	// on line 2.
	e, w, _ := linkEditor(t, "text [[a:b|Title]] mid [[b:c]] z\nplain line\ntail [[d:e]] end\n")

	// Not inside a link: nav does not capture.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.updateBrowseState()
	if e.navLink(+1) {
		t.Fatal("nav_next must not capture when no button is focused")
	}

	// Enter link A, then nav_next walks A -> B -> C -> (wrap) A.
	w.SetCursorPos(window.Position{Line: 0, Rune: 8})
	e.updateBrowseState()
	if b := e.focusedLinkButton(w); b == nil || b.Target != "a:b" {
		t.Fatalf("should focus link A; got %+v", b)
	}
	step := func(dir int, wantTarget string, wantLine int) {
		t.Helper()
		if !e.navLink(dir) {
			t.Fatalf("nav should capture while a button is focused")
		}
		e.updateBrowseState()
		b := e.focusedLinkButton(w)
		if b == nil || b.Target != wantTarget {
			t.Fatalf("after nav: want %s, got %+v", wantTarget, b)
		}
		if w.CursorPos().Line != wantLine {
			t.Fatalf("after nav: want line %d, got %d", wantLine, w.CursorPos().Line)
		}
	}
	step(+1, "b:c", 0)
	step(+1, "d:e", 2)
	step(+1, "a:b", 0) // wrapped to the first link
	step(-1, "d:e", 2) // prior wraps back to the last
	step(-1, "b:c", 0)
	step(-1, "a:b", 0)
}

// nav_start enters nav mode and focuses the first link at/after the caret.
func TestNavStart(t *testing.T) {
	e, w, _ := linkEditor(t, "no link here\ntext [[a:b|T]] end\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("browse mode should be off before nav_start")
	}
	if !e.navStart() {
		t.Fatal("nav_start should enter nav mode when a link exists")
	}
	if !w.BrowseActive {
		t.Fatal("nav_start should arm browse mode")
	}
	b := e.focusedLinkButton(w)
	if b == nil || b.Target != "a:b" {
		t.Fatalf("nav_start should focus the first link; got %+v", b)
	}
	if w.CursorPos().Line != 1 {
		t.Fatalf("caret should move to the link line; got line %d", w.CursorPos().Line)
	}

	// No links at all: nav_start fails and does not arm.
	e2, w2, _ := linkEditor(t, "nothing to see here\n")
	if e2.navStart() || w2.BrowseActive {
		t.Fatal("nav_start must fail (not arm) when there are no links")
	}
}

// nav_left / nav_right move to the optically adjacent link on the same line.
func TestNavLeftRight(t *testing.T) {
	// [[a]] [0,5), [[b]] [10,15), [[c]] [20,25) — ascending columns (LTR).
	e, w, _ := linkEditor(t, "[[a]] mid [[b]] end [[c]]\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 11}) // inside [[b]]
	e.updateBrowseState()
	if b := e.focusedLinkButton(w); b == nil || b.Target != "b" {
		t.Fatalf("should focus b; got %+v", b)
	}
	if !e.navHoriz(+1) || e.focusedLinkButton(w).Target != "c" {
		t.Fatalf("right should move b -> c; got %+v", e.focusedLinkButton(w))
	}
	if e.navHoriz(+1) {
		t.Fatal("right off the last link must not move")
	}
	if !e.navHoriz(-1) || e.focusedLinkButton(w).Target != "b" {
		t.Fatalf("left should move c -> b; got %+v", e.focusedLinkButton(w))
	}
	if !e.navHoriz(-1) || e.focusedLinkButton(w).Target != "a" {
		t.Fatalf("left should move b -> a; got %+v", e.focusedLinkButton(w))
	}
	if e.navHoriz(-1) {
		t.Fatal("left off the first link must not move")
	}
}

// Under RTL the left/right sense is optical: left moves toward higher rune
// columns (later in reading order).
func TestNavLeftRightRTL(t *testing.T) {
	// Hebrew makes the line lay out right-to-left; the two ASCII links keep
	// their reading order, so the earlier-rune link sits optically to the
	// RIGHT. Link x at rune 4, link y at rune 16.
	e, w, _ := renderedEditorWithConfig(t,
		"אבג [[x|א]] דהו [[y|ב]] זחט\n", "[options]\nsyntax=dokuwiki\ndirection=rtl\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside x
	e.updateBrowseState()
	if b := e.focusedLinkButton(w); b == nil || b.Target != "x" {
		t.Fatalf("should focus x; got %+v", b)
	}
	// Optical left, in RTL, advances reading order -> the higher-rune link y.
	if !e.navHoriz(-1) {
		t.Fatal("nav_left should move under RTL")
	}
	b := e.focusedLinkButton(w)
	if b == nil || b.Target != "y" {
		t.Fatalf("RTL nav_left should reach y; got %+v", b)
	}
	if w.CursorPos().Rune <= 5 {
		t.Fatalf("RTL nav_left should increase the rune column; got %d", w.CursorPos().Rune)
	}
}

// nav_down / nav_up move to the column-nearest link on the next / previous
// link line, and page (still succeeding) when none remains on screen.
func TestNavVertical(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"aaaa [[a]] bbbb [[b]]\nplain line\ncccc [[c]] dddd [[d]]\n",
		"[options]\nsyntax=dokuwiki\n")
	out.Reset()
	e.performRender() // establish ContentHeight

	// Focus [[b]] (column ~15); nav_down lands on the column-nearest link of
	// line 2, which is [[d]].
	w.SetCursorPos(window.Position{Line: 0, Rune: 17})
	e.updateBrowseState()
	if !e.navVert(+1) {
		t.Fatal("nav_down should move")
	}
	if w.CursorPos().Line != 2 || e.focusedLinkButton(w).Target != "d" {
		t.Fatalf("down should reach [[d]] on line 2; got line %d %+v",
			w.CursorPos().Line, e.focusedLinkButton(w))
	}
	// nav_up back to the column-nearest link of line 0: [[b]].
	if !e.navVert(-1) || e.focusedLinkButton(w).Target != "b" {
		t.Fatalf("up should reach [[b]]; got %+v", e.focusedLinkButton(w))
	}
}

// The vertical nav ideal is established once and held for the whole run, so
// repeated nav_down keeps a consistent target column even as links land at
// other columns — and a horizontal caret move re-anchors it.
func TestNavVerticalConsistentIdeal(t *testing.T) {
	// One link per line at different columns: [[a]] col 10, [[b]] col 0,
	// [[c]] col 5.
	e, w, out := renderedEditorWithConfig(t,
		"          [[a]]\n[[b]]\n     [[c]]\n", "[options]\nsyntax=dokuwiki\n")
	out.Reset()
	e.performRender()

	w.SetCursorPos(window.Position{Line: 0, Rune: 11}) // inside [[a]] (col ~10)
	e.updateBrowseState()
	if !e.navVert(+1) {
		t.Fatal("first nav_down should move")
	}
	if !w.NavIdealSet {
		t.Fatal("the vertical ideal should be established")
	}
	ideal := w.NavIdealCol
	if ideal < 9 || ideal > 11 {
		t.Fatalf("ideal should track the starting column (~10); got %d", ideal)
	}
	// Landed on [[b]] at column 0 — off the ideal.
	if w.CursorPos().Line != 1 {
		t.Fatalf("should be on line 1; got %d", w.CursorPos().Line)
	}
	// The next nav_down must still target the original ideal, not the column
	// [[b]] happened to sit at.
	if !e.navVert(+1) || w.NavIdealCol != ideal || !w.NavIdealSet {
		t.Fatalf("ideal must stay %d across the run; got %d (set=%v)",
			ideal, w.NavIdealCol, w.NavIdealSet)
	}
	if w.CursorPos().Line != 2 {
		t.Fatalf("should be on line 2; got %d", w.CursorPos().Line)
	}
	// A horizontal caret move re-anchors the vertical ideal.
	e.executeCommand("go_char_next")
	if w.NavIdealSet {
		t.Fatal("a horizontal move should clear the vertical nav ideal")
	}
}

// nav_down with no link line left on screen pages instead, and still reports
// success (staying in nav mode).
func TestNavVerticalPages(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"top [[a]] here\nplain\nplain\nplain\n", "[options]\nsyntax=dokuwiki\n")
	out.Reset()
	e.performRender()
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside [[a]]
	e.updateBrowseState()
	if !e.navVert(+1) {
		t.Fatal("nav_down should succeed (page) when no link line remains")
	}
	if !w.BrowseActive {
		t.Fatal("paging must not leave nav mode")
	}
	if e.focusedLinkButton(w) != nil {
		t.Fatal("after paging past the only link, nothing should be focused")
	}
}

// The vertical/horizontal nav commands act only in active nav mode.
func TestNavRequiresActiveMode(t *testing.T) {
	e, w, _ := linkEditor(t, "[[a]] mid [[b]]\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 7}) // the 'i' in "mid": outside any link
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("browse should be off between links")
	}
	if e.navVert(+1) || e.navVert(-1) || e.navHoriz(+1) || e.navHoriz(-1) {
		t.Fatal("nav up/down/left/right must not act outside active nav mode")
	}
}

// nav_next respects linkBrowsing=no (never captures).
func TestNavGatedByOption(t *testing.T) {
	e, w, _ := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\nlinkBrowsing=no\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	if e.navLink(+1) || e.navFollow() {
		t.Fatal("nav must not act when linkBrowsing is off")
	}
}

// The caret-hide predicate the renderer consults reports true exactly when a
// button is focused, and the keymap wires the three nav commands.
func TestNavKeymapAndCaretHide(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)
	// Exact for the no-escape bindings; prefix for the ones whose insert arg
	// carries a control char (the parser unescapes \n/\t in mapping values).
	for k, want := range map[string]string{
		"^C":    "nav_cancel|cancel|buffer_close",
		"S-tab": "nav_prior",
	} {
		if got := e.KeyProcessor.GetMapping(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	for k, prefix := range map[string]string{
		"return": "nav_follow|accept|insert ",
		"tab":    "nav_next|completion|insert ",
	} {
		if got := e.KeyProcessor.GetMapping(k); !strings.HasPrefix(got, prefix) {
			t.Fatalf("%s = %q, want prefix %q", k, got, prefix)
		}
	}

	// Caret-hide predicate: false outside a link, true on a focused button.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.updateBrowseState()
	if e.focusedLinkButton(w) != nil {
		t.Fatal("no button focused outside a link")
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	if e.focusedLinkButton(w) == nil {
		t.Fatal("button should be focused inside the link (caret then hides)")
	}
}

// nav_follow records the visit on the buffer (presence set + timestamped log),
// and the link then paints in the recent style: buttonRecent in browse mode,
// linkRecent in caret mode.
func TestVisitedLinkTrackingAndColor(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"text [[a:b|Title]] more\n", "[options]\nsyntax=dokuwiki\n")

	// Follow the link; the buffer records it.
	w.SetCursorPos(window.Position{Line: 0, Rune: 10})
	e.updateBrowseState()
	if !e.navFollow() {
		t.Fatal("nav_follow should activate the focused button")
	}
	if !w.Buffer.LinkVisited("a:b") {
		t.Fatal("the visited target should be in the presence set")
	}
	if w.Buffer.LinkVisited("nope") {
		t.Fatal("an unvisited target must not be present")
	}
	log := w.Buffer.LinkVisits()
	if len(log) != 1 || log[0].Target != "a:b" || log[0].At.IsZero() {
		t.Fatalf("visit log should hold one timestamped entry; got %+v", log)
	}

	// Browse mode, unfocused: the visited button uses the recent color.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[0;37;42m") {
		t.Fatal("a visited button should paint in buttonRecent (silver on green)")
	}

	// Caret mode: the visited link uses linkRecent, not the plain link color.
	w.BrowseActive = false
	out.Reset()
	e.performRender()
	raw := out.String()
	if !strings.Contains(raw, "\x1b[0;4;32;40m") {
		t.Fatal("a visited link should paint in linkRecent in caret mode")
	}
	if strings.Contains(raw, "\x1b[0;4;93;40m") {
		t.Fatal("a visited link must not still show the unvisited link color")
	}
}

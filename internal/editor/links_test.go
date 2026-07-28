package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// Content used across these tests: one dokuwiki link with a distinct target
// and title. Runes: "text " = 0..4, "[[a:b|Title]]" = 5..17, so the span is
// [5, 18) and the strict interior (where the caret counts as inside) is
// 5 < p < 18.
const linkLine = "text [[a:b|Title]] more\n"

var linkAnsiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripSGR(s string) string { return linkAnsiRe.ReplaceAllString(s, "") }

func linkEditor(t *testing.T, content string) (*Editor, *viewport.Viewport, *strings.Builder) {
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
		w.SetCursorPos(viewport.Position{Line: 0, Rune: rune})
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10}) // inside the link
	w.BrowseActive = true
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
	if !e.navFollow(true) {
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

// Navigation mode is entered EXPLICITLY through the navigationMode option
// (the ^O N binding is set_option_next navigationMode), not by the caret
// landing on a link. Toggling it arms/disarms browse mode; moving the caret
// never changes it; and nav_cancel disarms just like turning the option off.
func TestNavigationModeOption(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)

	// A linkable page does NOT auto-arm — it opens in caret mode.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 5}) // inside a link
	if w.BrowseActive {
		t.Fatal("a page must not auto-arm browse mode any more")
	}

	// The option turns it on (rotating through set_option_next, as ^O N does).
	if !e.setOption(w, "navigationMode", "yes") {
		t.Fatal("set_option navigationMode yes failed")
	}
	if !w.BrowseActive {
		t.Fatal("navigationMode=yes should arm browse mode")
	}
	if got, _ := e.getOption(w, "navigationMode"); got != "yes" {
		t.Fatalf("getOption navigationMode = %q, want yes", got)
	}

	// Moving the caret — onto a link or off it — never changes the mode.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2}) // outside any link
	if !w.BrowseActive {
		t.Fatal("moving off a link must not disarm")
	}

	// nav_cancel disarms; a second one fails (chain falls through).
	if !e.navCancel() {
		t.Fatal("nav_cancel should succeed while armed")
	}
	if w.BrowseActive {
		t.Fatal("nav_cancel should have disarmed")
	}
	if e.navCancel() {
		t.Fatal("nav_cancel should fail when not armed")
	}
	if got, _ := e.getOption(w, "navigationMode"); got != "no" {
		t.Fatalf("getOption navigationMode after cancel = %q, want no", got)
	}
}

// Caret mode renders the raw link text in the link color; browse mode
// replaces it with a button (cap + title + cap + shadow) and the focused
// variant marks the button the caret occupies.
func TestLinkRenderStyles(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\n")

	// Caret mode is the default now (no auto-arm): raw text, link color, no
	// button chrome.
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
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
	if !strings.Contains(out.String(), "\x1b[0;1;30;47m") {
		t.Fatal("browse mode: button color expected")
	}

	// Focused button: caret inside the span switches caps/shadow/colors.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w.BrowseActive = true
	e.performRender()
	if w.Context != "a:b" {
		t.Fatalf("context should show the destination; got %q", w.Context)
	}

	// The Enter binding: nav_follow false only fires in navigation mode, so
	// here (browse mode, focused button) it follows and no newline is inserted.
	lines := w.Buffer.GetLineCount()
	e.executeCommand("nav_follow false|accept|insert '\\n'")
	if w.Buffer.GetLineCount() != lines {
		t.Fatal("nav_follow false on a focused button must not insert a newline")
	}
	found := false
	for _, nw := range e.ViewportManager.AllViewports() {
		if nw.Class == "notification" && strings.Contains(nw.MessageTopInner, "Link: a:b") {
			found = true
		}
	}
	if !found {
		t.Fatal("nav_follow should show a 'Link: <target>' notification")
	}

	// Disarmed (caret mode): nav_follow false does NOT follow, so the chain
	// falls through to insert. (Bare nav_follow / nav_follow true WOULD follow
	// here — covered by TestNavFollowAlways.)
	if !e.navCancel() {
		t.Fatal("nav_cancel should disarm")
	}
	e.executeCommand("nav_follow false|accept|insert '\\n'")
	if w.Buffer.GetLineCount() != lines+1 {
		t.Fatal("nav_follow false in caret mode should fall through to insert")
	}
}

// nav_follow (no arg / true) follows from ordinary EDIT mode when the caret is
// within a link's source, while nav_follow false stays gated to navigation
// mode. This is what lets ^B F jump directly and the Enter binding
// (nav_follow false) fall through to a newline.
func TestNavFollowAlways(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)

	// Caret mode (browse off), caret inside the link source.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	if w.BrowseActive {
		t.Fatal("precondition: not in navigation mode")
	}

	// Gated form does nothing here (falls through).
	if e.navFollow(false) {
		t.Fatal("nav_follow false must not follow outside navigation mode")
	}

	// Always form follows even from edit mode: a notification names the target.
	if !e.navFollow(true) {
		t.Fatal("nav_follow (always) should follow from edit mode when on a link")
	}
	found := false
	for _, nw := range e.ViewportManager.AllViewports() {
		if nw.Class == "notification" && strings.Contains(nw.MessageTopInner, "Link: a:b") {
			found = true
		}
	}
	if !found {
		t.Fatal("always-follow should activate the link (Link: a:b notification)")
	}

	// Off any link, even the always form fails (nothing to follow).
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	if e.navFollow(true) {
		t.Fatal("always-follow must fail when the caret is not on a link")
	}
}

// An RTL title flows through the normal bidi machinery inside the button.
func TestLinkButtonRTLTitle(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t, "x [[p|שלום]] y\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 5})
	w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	if !e.setOption(w, "navigationMode", "yes") {
		t.Fatal("set_option navigationMode yes should not error")
	}
	if w.BrowseActive {
		t.Fatal("linkBrowsing=no: navigationMode must not arm without the link layer")
	}

	// Turning the option off while armed retires the buttons.
	e2, w2, out2 := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\n")
	w2.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w2.BrowseActive = true
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

	// Not inside a link: the GATED form does not capture, which is what lets
	// tab = nav_next false|completion|insert '\t' fall through to editing.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	if e.navLink(+1, false) {
		t.Fatal("gated nav_next must not capture when no button is focused")
	}

	// Enter link A, then nav_next walks A -> B -> C -> (wrap) A.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 8})
	w.BrowseActive = true
	if b := e.focusedLinkButton(w); b == nil || b.Target != "a:b" {
		t.Fatalf("should focus link A; got %+v", b)
	}
	step := func(dir int, wantTarget string, wantLine int) {
		t.Helper()
		if !e.navLink(dir, false) {
			t.Fatalf("nav should capture while a button is focused")
		}
		w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 11}) // inside [[b]]
	w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 5}) // inside x
	w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 17})
	w.BrowseActive = true
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

	w.SetCursorPos(viewport.Position{Line: 0, Rune: 11}) // inside [[a]] (col ~10)
	w.BrowseActive = true
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
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 5}) // inside [[a]]
	w.BrowseActive = true
	if !e.navVert(+1) {
		t.Fatal("nav_down should succeed (page) when no link line remains")
	}
	if !w.BrowseActive {
		t.Fatal("paging must not leave nav mode")
	}
	if e.focusedLinkButton(w) != nil {
		t.Fatal("after paging past the only link, nothing should be focused")
	}
	// With no button focused (the caret paged off the link), a further nav_down
	// does nothing: nav_up/down act only when a button is focused at activation.
	if e.navVert(+1) {
		t.Fatal("nav_down must not act once no button is focused")
	}
}

// The vertical/horizontal nav commands act only in active nav mode.
func TestNavRequiresActiveMode(t *testing.T) {
	e, w, _ := linkEditor(t, "[[a]] mid [[b]]\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 7}) // the 'i' in "mid": outside any link
	if w.BrowseActive {
		t.Fatal("browse should be off (no auto-arm)")
	}
	// Not in nav mode: the vertical/horizontal nav commands do nothing.
	if e.navVert(+1) || e.navVert(-1) || e.navHoriz(+1) || e.navHoriz(-1) {
		t.Fatal("nav up/down/left/right must not act outside active nav mode")
	}
}

// nav_next respects linkBrowsing=no (never captures).
func TestNavGatedByOption(t *testing.T) {
	e, w, _ := renderedEditorWithConfig(t, linkLine, "[options]\nsyntax=dokuwiki\nlinkBrowsing=no\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w.BrowseActive = true
	// Even ungated: linkBrowsing=no turns the whole layer off.
	if e.navLink(+1, true) || e.navFollow(true) {
		t.Fatal("nav must not act when linkBrowsing is off")
	}
}

// The caret-hide predicate the renderer consults reports true exactly when a
// button is focused, and the keymap wires the three nav commands.
func TestNavKeymapAndCaretHide(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)
	// Exact for the no-escape bindings; prefix for the ones whose insert arg
	// carries a control char (the parser unescapes \n/\t in mapping values).
	//
	// The nav commands carry "false" here: that is the GATED form, which is
	// what lets these chains fall through to editing when the caret is not on
	// a link. Bare nav_next / nav_prior always act (^B tab / ^B S-tab).
	for k, want := range map[string]string{
		"^C":    "cancel|buffer_close",
		"S-tab": "nav_prior false|smart_unindent",
	} {
		if got := e.KeyProcessor.GetMapping(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	for k, prefix := range map[string]string{
		"return": "nav_follow false|accept|insert_newline",
		"tab":    "nav_next false|completion|insert ",
	} {
		if got := e.KeyProcessor.GetMapping(k); !strings.HasPrefix(got, prefix) {
			t.Fatalf("%s = %q, want prefix %q", k, got, prefix)
		}
	}

	// Caret-hide predicate: false outside a link, true on a focused button.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	if e.focusedLinkButton(w) != nil {
		t.Fatal("no button focused outside a link")
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w.BrowseActive = true
	if e.focusedLinkButton(w) == nil {
		t.Fatal("button should be focused inside the link (caret then hides)")
	}
}

// A visited target is recorded editor-wide (presence set + timestamped log)
// and the link then paints in the recent style: buttonRecent in browse mode,
// linkRecent in caret mode. (The recording itself is exercised via the
// internal helper here; TestFollowMissingLinkVisitOnlyOnCreate covers WHEN a
// follow records a visit.)
func TestVisitedLinkTrackingAndColor(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"text [[a:b|Title]] more\n", "[options]\nsyntax=dokuwiki\n")

	// Record the visit (an unnamed buffer has no resolution context, so the
	// identity is the raw target).
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 10})
	w.BrowseActive = true
	e.recordLinkVisit(w.Buffer, "a:b", e.resolveFollow(w, "a:b"))
	if !e.linkTargetVisited(w, "a:b") {
		t.Fatal("the visited target should be in the presence set")
	}
	if e.linkTargetVisited(w, "nope") {
		t.Fatal("an unvisited target must not be present")
	}
	if len(e.linkVisitLog) != 1 || e.linkVisitLog[0].Key != "a:b" || e.linkVisitLog[0].At.IsZero() {
		t.Fatalf("visit log should hold one timestamped entry; got %+v", e.linkVisitLog)
	}

	// Browse mode, unfocused: the visited button uses the recent color.
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[0;30;47m") {
		t.Fatal("a visited button should paint in buttonRecent (black on silver)")
	}
	if !strings.Contains(out.String(), "\x1b[0;34;47m") {
		t.Fatal("a visited button's shadow should use buttonShadowRecent")
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

// Direction controls never reach the terminal byte stream (outside showBidi's
// visible markers): not the FSI/PDI isolates injected around browse-mode
// buttons, and not document-embedded controls — the emitted stream is already
// in visual order, so raw controls would invite a bidi-aware terminal to
// reorder it again.
func TestNoDirectionControlsEmitted(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"go [[page|Title]] on\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	for _, c := range []rune{'⁦', '⁧', '⁨', '⁩'} {
		if strings.ContainsRune(out.String(), c) {
			t.Fatalf("browse-mode output must not contain isolate U+%04X", c)
		}
	}

	// A document-embedded LRM/RLM stays out of the stream too (unmarked mode).
	e2, _, out2 := renderedEditorWithConfig(t, "a‎b ‏c\n", "[options]\n")
	out2.Reset()
	e2.performRender()
	for _, c := range []string{"‎", "‏"} {
		if strings.Contains(out2.String(), c) {
			t.Fatal("document direction controls must not be emitted raw")
		}
	}
	plain := stripSGR(out2.String())
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("content %q should still render; got %q", want, plain)
		}
	}
}

// Following a link whose page does not exist yet asks "Create it?" — and the
// link is marked VISITED only if the page is actually created. Declining (or
// cancelling) leaves the link unfollowed, still painting as unvisited.
func TestFollowMissingLinkVisitOnlyOnCreate(t *testing.T) {
	files := map[string]string{"w/page.txt": "go [[missing]] now\n"}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	w.BrowseActive = true
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 5}) // inside [[missing]]

	// Follow; the create prompt appears. Decline it.
	if !e.navFollow(true) {
		t.Fatal("nav_follow should engage (create prompt)")
	}
	answerPrompt(t, e, "") // plain accept = default N
	if e.linkTargetVisited(w, "missing") {
		t.Fatal("declining creation must leave the link UNvisited")
	}
	if len(e.linkVisitLog) != 0 {
		t.Fatalf("no visit may be logged on decline; log %+v", e.linkVisitLog)
	}

	// Follow again; accept creation this time.
	if !e.navFollow(true) {
		t.Fatal("nav_follow should engage again")
	}
	answerPrompt(t, e, "y")
	if !e.linkTargetVisited(w, "missing") {
		t.Fatal("creating the page should mark the link visited")
	}
	if len(e.linkVisitLog) != 1 {
		t.Fatalf("exactly one visit should be logged; log %+v", e.linkVisitLog)
	}
}

// Bare nav_next / nav_prior ALWAYS act: from the caret's own link, or into the
// first link from the caret when it is in none. The gated form (argument
// "false") is the one that yields, so a key bound alone gets the action while
// a key in a chain still falls through — the same split nav_follow uses.
func TestNavLinkUngatedEntersFromPlainCaret(t *testing.T) {
	e, w, _ := linkEditor(t, "text [[a:b|Title]] mid [[b:c]] z\nplain line\ntail [[d:e]] end\n")

	// Caret on plain text, browse mode OFF: nothing is focused.
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 0})
	w.BrowseActive = false
	if e.focusedLinkButton(w) != nil {
		t.Fatal("precondition: no button should be focused")
	}

	if e.navLink(+1, false) {
		t.Error("gated nav must not act with no focused button")
	}
	if !e.navLink(+1, true) {
		t.Fatal("ungated nav should enter the first link from the caret")
	}
	if !w.BrowseActive {
		t.Error("entering a link should arm browse mode")
	}
	b := e.focusedLinkButton(w)
	if b == nil {
		t.Fatal("ungated nav should leave a link focused")
	}
	if b.Target != "d:e" {
		t.Errorf("focused %q, want the first link at/after the caret (d:e)", b.Target)
	}
}

// With the caret already inside a link, ungated nav steps to the sibling
// rather than re-entering the one it is on.
func TestNavLinkUngatedStepsFromCaretLink(t *testing.T) {
	e, w, _ := linkEditor(t, "text [[a:b|Title]] mid [[b:c]] z\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 8}) // inside link A
	w.BrowseActive = false

	if !e.navLink(+1, true) {
		t.Fatal("ungated nav should step from the caret's own link")
	}
	if b := e.focusedLinkButton(w); b == nil || b.Target != "b:c" {
		t.Fatalf("focused %+v, want the sibling link b:c", b)
	}
}

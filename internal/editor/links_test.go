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

// Entering a link arms browse mode; nav_cancel disarms it and staying within
// the same span does not re-arm; leaving and re-entering re-arms.
func TestBrowseModeArming(t *testing.T) {
	e, w, _ := linkEditor(t, linkLine)

	w.SetCursorPos(window.Position{Line: 0, Rune: 2}) // outside
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("browse must not arm outside a link")
	}
	// Boundary positions are NOT inside: the span starts at 5.
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.updateBrowseState()
	if w.BrowseActive {
		t.Fatal("the span's start boundary must not arm")
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 10}) // strictly inside
	e.updateBrowseState()
	if !w.BrowseActive {
		t.Fatal("entering the link must arm browse mode")
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

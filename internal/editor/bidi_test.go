package editor

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/window"
)

// stripAnsi removes ANSI escape sequences so rendered content can be matched
// as plain text (the renderer wraps each cell in its own color escapes).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripAnsi(s string) string { return ansiRe.ReplaceAllString(s, "") }

// bidiLine has an RTL (Hebrew) word between LTR words: logical indices
// 0-3 "abc ", 4-7 the Hebrew run, 8-11 " xyz".
const bidiLine = "abc שלום xyz"

// On a bidirectional line the cursor's visual column is where its logical
// rune is painted: the Hebrew run occupies columns 4-7 REVERSED, so logical
// rune 4 (the first Hebrew letter) paints at column 7.
func TestBidiRuneToVisualColumn(t *testing.T) {
	e, w := newTestEditor(t, bidiLine+"\n")
	if got := e.runeToVisualColumn(w, bidiLine, 0, 4); got != 0 {
		t.Fatalf("rune 0: col %d, want 0", got)
	}
	if got := e.runeToVisualColumn(w, bidiLine, 4, 4); got != 7 {
		t.Fatalf("first hebrew rune should paint at col 7, got %d", got)
	}
	if got := e.runeToVisualColumn(w, bidiLine, 7, 4); got != 4 {
		t.Fatalf("last hebrew rune should paint at col 4, got %d", got)
	}
	if got := e.runeToVisualColumn(w, bidiLine, 9, 4); got != 9 {
		t.Fatalf("post-run latin rune keeps its col 9, got %d", got)
	}
	if got := e.runeToVisualColumn(w, bidiLine, len([]rune(bidiLine)), 4); got != 12 {
		t.Fatalf("end of line should be total width 12, got %d", got)
	}

	// And the inverse: the cell at column 7 holds logical rune 4.
	if got := e.visualColumnToRune(w, bidiLine, 7, 4); got != 4 {
		t.Fatalf("col 7 should map to logical rune 4, got %d", got)
	}
	if got := e.visualColumnToRune(w, bidiLine, 4, 4); got != 7 {
		t.Fatalf("col 4 should map to logical rune 7, got %d", got)
	}
}

// The rtl command reports whether the caret sits in an RTL segment.
func TestRTLCommand(t *testing.T) {
	e, w := newTestEditor(t, bidiLine+"\n")

	w.SetCursorPos(window.Position{Line: 0, Rune: 1}) // 'b'
	if res := e.PawScript.ExecuteAsync("rtl"); res != pawscript.BoolStatus(false) {
		t.Fatalf("latin position should report false, got %v", res)
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside the Hebrew word
	if res := e.PawScript.ExecuteAsync("rtl"); res != pawscript.BoolStatus(true) {
		t.Fatalf("hebrew position should report true, got %v", res)
	}
}

// The direction option is per-window: set_option targets the current window,
// round-trips through getOption, leaves the global base untouched, and rejects
// junk values.
func TestDirectionOption(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if v, ok := e.getOption(w, "direction"); !ok || v != "ltr" {
		t.Fatalf("default direction: %q ok=%v", v, ok)
	}
	// set_option targets the last main-buffer window.
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	if v, _ := e.getOption(w, "direction"); v != "rtl" {
		t.Fatalf("window direction after set: %q", v)
	}
	if !e.winRTL(w) {
		t.Fatal("winRTL should reflect the per-window option")
	}
	// The global base direction is unaffected by a per-window override.
	if e.baseRTL() {
		t.Fatal("per-window direction must not change the global base")
	}
	if v, _ := e.getOption(nil, "direction"); v != "ltr" {
		t.Fatalf("global direction should stay ltr: %q", v)
	}
	e.PawScript.ExecuteAsync("set_option 'direction', 'sideways'")
	if v, _ := e.getOption(w, "direction"); v != "rtl" {
		t.Fatalf("invalid value must not change direction: %q", v)
	}
}

// Rendering paints the Hebrew run reversed: the visual byte sequence of the
// reversed word appears in the output, the logical sequence does not.
func TestBidiRenderedOutput(t *testing.T) {
	e, _, out := newRenderedEditor(t, bidiLine+"\n")
	e.performRender()
	plain := stripAnsi(out.String())
	if !strings.Contains(plain, "םולש") { // shalom reversed
		t.Fatal("rendered output should contain the visually reversed hebrew run")
	}
	if strings.Contains(plain, "שלום") {
		t.Fatal("rendered output must not contain the logical-order hebrew run")
	}
}

// With direction=rtl the whole line mirrors: the LTR word stays intact but
// the line reads from the right (last logical word first).
func TestBidiRenderedRTLBase(t *testing.T) {
	e, _, out := newRenderedEditor(t, "אבג abc דהו\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	out.Reset()
	e.performRender()
	if !strings.Contains(stripAnsi(out.String()), "והד abc גבא") {
		t.Fatal("RTL base should mirror the whole line around the LTR word")
	}
}

// The modebar logo flips to _M while the caret is in an RTL segment and back
// to M_ on LTR text.
func TestModebarLogoFlips(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	e.createPluginWindows() // the modebar window (normally made by run())

	w.SetCursorPos(window.Position{Line: 0, Rune: 1}) // latin
	e.performRender()
	plain := stripAnsi(out.String())
	if !strings.Contains(plain, " M_ ") || strings.Contains(plain, " _M ") {
		t.Fatal("LTR caret should show the M_ logo")
	}

	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // hebrew
	e.Renderer.ForceRedraw()                          // inspect the whole modebar row, not just the diff
	out.Reset()
	e.performRender()
	if !strings.Contains(stripAnsi(out.String()), " _M ") {
		t.Fatal("RTL caret should flip the logo to _M")
	}
}

// The caret covers the cell of the rune it precedes, on both sides of a
// direction change: an RTL fragment's carets sit ON each Hebrew letter's own
// painted cell, not one cell to its right.
func TestBidiCaretSide(t *testing.T) {
	e, w := newTestEditor(t, bidiLine+"\n")
	// Logical 4 (before the first Hebrew letter, painted at col 7): on that
	// cell, col 7.
	if got := e.caretVisualColumn(w, bidiLine, 4, 4); got != 7 {
		t.Fatalf("caret before first hebrew rune: col %d, want 7", got)
	}
	// Logical 5 (the 2nd Hebrew letter, painted at col 6): on that cell, col 6.
	if got := e.caretVisualColumn(w, bidiLine, 5, 4); got != 6 {
		t.Fatalf("caret inside hebrew run: col %d, want 6", got)
	}
	// LTR positions keep the on-cell convention.
	if got := e.caretVisualColumn(w, bidiLine, 1, 4); got != 1 {
		t.Fatalf("ltr caret: col %d, want 1", got)
	}
	if got := e.caretVisualColumn(w, bidiLine, len([]rune(bidiLine)), 4); got != 12 {
		t.Fatalf("eol after ltr rune: col %d, want 12", got)
	}
}

// ghostColumn returns the 1-based screen column of the rendered ghost-cursor
// glyph ("|"), or -1 if none was drawn. It replays the emitted stream (CUP
// positioning + glyph advancement) since the row-granularity diff paints the
// ghost inside its row rather than with a dedicated cursor move.
func ghostColumn(out []byte) int {
	raw := string(out)
	col, row := 1, 1
	found := -1
	i := 0
	for i < len(raw) {
		if raw[i] == 0x1b {
			j := i + 2
			for j < len(raw) && !((raw[j] >= 'A' && raw[j] <= 'Z') || (raw[j] >= 'a' && raw[j] <= 'z')) {
				j++
			}
			if j < len(raw) && raw[j] == 'H' {
				parts := strings.SplitN(raw[i+2:j], ";", 2)
				if len(parts) == 2 {
					fmtSscan(parts[0], &row)
					fmtSscan(parts[1], &col)
				}
			}
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(raw[i:])
		i += size
		w := textwidth.Rune(r)
		if w <= 0 {
			continue
		}
		if r == '|' {
			found = col // last one wins (overlays paint after content)
		}
		col += w
	}
	return found
}

func fmtSscan(s string, out *int) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*out = n
}

// direction=rtl: the ghost cursor must work — moving from a long line onto a
// shorter one holds the caret's SCREEN column (the view is right-anchored, so
// the sticky column is a reading distance from the right), drawing the ghost
// there while the real caret clamps to the shorter line's end.
func TestBidiRTLGhostHoldsScreenColumn(t *testing.T) {
	e, w, out := newRenderedEditor(t, "אבגדהוזח\nאב\n") // 8 letters, then 2
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")

	w.SetCursorPos(window.Position{Line: 0, Rune: 6})
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()
	_, srcCol := lastCursor(out.Bytes())

	e.executeCommand("go_line_next")
	if !w.HasGhostCursor {
		t.Fatal("moving onto the shorter RTL line should raise a ghost cursor")
	}
	out.Reset()
	e.performRender()
	if gc := ghostColumn(out.Bytes()); gc != srcCol {
		t.Fatalf("ghost column %d, want %d (the source line's screen column)", gc, srcCol)
	}
}

// direction=rtl: descending from the reading start of a short line onto a
// longer line keeps the caret at the reading start (right edge) with no ghost.
func TestBidiRTLGhostReadingStartPreserved(t *testing.T) {
	e, w, out := newRenderedEditor(t, "אב\nאבגדהוזח\n") // 2 letters, then 8
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")

	w.SetCursorPos(window.Position{Line: 0, Rune: 0}) // reading start
	e.afterHorizontalMovement(w)
	e.executeCommand("go_line_next")
	if w.HasGhostCursor {
		t.Fatal("the reading start reaches every line; no ghost expected")
	}
	out.Reset()
	e.performRender()
	if _, col := lastCursor(out.Bytes()); col != 80 {
		t.Fatalf("caret should stay at the reading start (right edge, col 80), got %d", col)
	}
}

// The hardware cursor lands on the caret's rune cell (rune 5, the 2nd Hebrew
// letter, is painted at visual col 6 -> screen col 7 with no gutter).
func TestBidiHardwareCursorSide(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside the Hebrew word
	e.performRender()
	_, col := lastCursor(out.Bytes())
	if col != 7 { // visual col 6 + 1 (no gutter)
		t.Fatalf("hardware cursor col %d, want 7", col)
	}
}

// LTR base: the hardware cursor walks ONTO each RTL-fragment cell, never one
// cell to its right. "abc שלום xyz" paints the Hebrew (L->R) as ם ו ל ש at
// screen cols 5-8, so logical runes 4..7 (ש,ל,ו,ם) sit at cols 8,7,6,5.
func TestBidiHardwareCursorWalksRTLFragmentOnCell(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	want := map[int]int{4: 8, 5: 7, 6: 6, 7: 5}
	for rune_, wantCol := range want {
		w.SetCursorPos(window.Position{Line: 0, Rune: rune_})
		e.afterHorizontalMovement(w)
		out.Reset()
		e.performRender()
		if _, col := lastCursor(out.Bytes()); col != wantCol {
			t.Fatalf("rune %d: hardware cursor col %d, want %d (on the letter's own cell)", rune_, col, wantCol)
		}
	}
}

// direction=rtl right-aligns lines: the content sits flush against the right
// edge with the padding on the left, and the hardware cursor follows.
func TestBidiRTLRightAligned(t *testing.T) {
	e, w, out := newRenderedEditor(t, "אבג\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0}) // reading start
	out.Reset()
	e.performRender()

	plain := stripAnsi(out.String())
	if !strings.Contains(plain, strings.Repeat(" ", 30)+"גבא") {
		t.Fatal("RTL base should right-align the line (padding on the left)")
	}
	// Reading start parks at the right edge (col 80 on the 80-wide screen).
	_, col := lastCursor(out.Bytes())
	if col != 80 {
		t.Fatalf("caret at reading start should sit at the right edge, col %d, want 80", col)
	}

	// End of line (logical len) parks one cell LEFT of the content.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	out.Reset()
	e.performRender()
	_, col = lastCursor(out.Bytes())
	if col != 77 { // content at 78..80; boundary cell left of it
		t.Fatalf("caret at eol should sit left of the content, col %d, want 77", col)
	}
}

// Horizontal scrolling under direction=rtl advances through READING order:
// the view is right-anchored, so scrolling trims the reading head off the
// RIGHT and reveals the tail on the left — with the per-line truncation
// marker on the LEFT edge where the tail continues.
func TestBidiRTLScrollTrimsReadingHead(t *testing.T) {
	content := strings.Repeat("a", 90) + "XYZ" // 93 cols; reading head = "XYZ" side
	e, w, out := newRenderedEditor(t, content+"\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	// Park the caret at the reading head (visible, right edge) so the off-screen
	// cursor "@" indicator is not drawn over the left-edge truncation marker —
	// the two share column 0, and under the back buffer the later "@" write
	// composites over the marker (as it does on a real terminal).
	w.SetCursorPos(window.Position{Line: 0, Rune: 93})

	out.Reset()
	e.performRender()
	plain := stripAnsi(out.String())
	if !strings.Contains(plain, "XYZ") {
		t.Fatal("unscrolled RTL view must show the reading head (XYZ) at the right")
	}
	if !strings.Contains(plain, "<a") {
		t.Fatal("the trimmed reading tail should be marked on the LEFT edge")
	}

	// Scroll forward: the reading head is trimmed off the right.
	w.ViewState.ViewOffsetX = 5
	out.Reset()
	e.performRender()
	plain = stripAnsi(out.String())
	if strings.Contains(plain, "XYZ") {
		t.Fatal("scrolling should trim the reading head off the RIGHT edge")
	}
}

// Under direction=rtl, when the view is horizontally scrolled (a long line
// elsewhere), a short/empty line's caret sits past the right edge in reading
// space — so the off-screen "@" indicator must appear. Regression for the RTL
// pad clamp that pinned the caret to the edge and suppressed the indicator.
func TestRTLScrolledShortLineShowsOffScreenIndicator(t *testing.T) {
	// Empty first line above a long line that justifies the horizontal scroll.
	e, w, out := newRenderedEditor(t, "\n"+strings.Repeat("a", 120)+"\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.ViewState.ViewOffsetX = 80 // scrolled deep into the long line's tail
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.performRender()
	offGlyph := config.DefaultIndicators().CursorOffScreen
	if !strings.Contains(stripAnsi(out.String()), offGlyph) {
		t.Fatalf("caret on a scrolled-off short line should show the off-screen %q indicator", offGlyph)
	}
}

// Typing at the end of a long RTL-base line keeps the caret visible at the
// right edge without scrolling (the reading start is column 0 in reading
// space); jumping to the line's logical start scrolls in reading columns.
func TestBidiRTLTypingKeepsCaretVisible(t *testing.T) {
	e, w, out := newRenderedEditor(t, strings.Repeat("k", 85)+"\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.SetCursorPos(window.Position{Line: 0, Rune: 85})

	e.executeCommand("insert 'q'") // caret at EOL of an 86-col line
	if w.ViewState.ViewOffsetX != 0 {
		t.Fatalf("typing at the reading start must not scroll, offset %d", w.ViewState.ViewOffsetX)
	}
	out.Reset()
	e.performRender()
	if _, col := lastCursor(out.Bytes()); col != 80 {
		t.Fatalf("caret should sit at the right edge, col %d, want 80", col)
	}

	// The line's logical start is 86 reading columns back: scrolls into view.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("go_line_beg")
	if w.ViewState.ViewOffsetX != 7 {
		t.Fatalf("reading-space scroll offset %d, want 7", w.ViewState.ViewOffsetX)
	}
	out.Reset()
	e.performRender()
	if _, col := lastCursor(out.Bytes()); col != 2 {
		t.Fatalf("caret at logical start should be visible at col 2, got %d", col)
	}
}

// Prompt windows are pinned LTR regardless of the direction option.
func TestPromptPinnedLTR(t *testing.T) {
	e, _ := newTestEditor(t, "אבג\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	e.PromptForInput("x: ", "", func(string, bool) {})
	p := focusedPrompt(e)
	if p == nil {
		t.Fatal("expected a focused prompt")
	}
	if p.ViewState.Direction != "ltr" {
		t.Fatalf("prompt direction %q, want ltr", p.ViewState.Direction)
	}
}

// Under RTL the line-number gutter mirrors to the right side of the content,
// and a window's left/right messages swap slots.
func TestBidiRTLGutterAndMessagesMirror(t *testing.T) {
	e, w, out := newRenderedEditor(t, "אבג\nxyz\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.ViewState.ShowLineNumbers = true
	w.MessageTopInner = "INNERMSG"
	w.MessageTopOuter = "OUTERMSG"
	out.Reset()
	e.performRender()
	plain := stripAnsi(out.String())

	// Gutter on the right: the line's content is followed by its number.
	if !strings.Contains(plain, "גבא 1") {
		t.Fatal("line number should sit to the RIGHT of the content")
	}
	// Inner renders at the reading start (RIGHT under rtl): outer paints first.
	ri, li := strings.Index(plain, "OUTERMSG"), strings.Index(plain, "INNERMSG")
	if ri < 0 || li < 0 || ri > li {
		t.Fatalf("messages should mirror (OUTERMSG at left, INNERMSG at reading start): outer@%d inner@%d", ri, li)
	}
}

// showBidi renders direction markers at fragment leading edges, in the
// control-character color, and every visual-column computation accounts for
// their cells.
func TestShowBidiMarkers(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	w.ViewState.ShowBidi = true
	e.performRender()
	plain := stripAnsi(out.String())
	// Hebrew fragment: "<" at its right (leading) edge; the returning LTR
	// fragment gets ">" before its cells.
	if !strings.Contains(plain, "םולש<> xyz") {
		t.Fatal("markers should flank the fragment boundaries")
	}

	// Width accounting: 'y' (logical 10) sits three marker cells further
	// right than without markers (the fragment's "|", "<" and ">": visual
	// col 10 -> 13).
	if got := e.runeToVisualColumn(w, bidiLine, 10, 4); got != 13 {
		t.Fatalf("logical 10 with markers: col %d, want 13", got)
	}
	// The caret entering the Hebrew fragment lands ON the first Hebrew
	// letter's cell (ש at col 8), not the "<" marker one cell to its right.
	if got := e.caretVisualColumn(w, bidiLine, 4, 4); got != 8 {
		t.Fatalf("caret before hebrew with markers: col %d, want 8", got)
	}
	// Total line width includes all four marker cells (two begins, two ends).
	if got := e.lineVisualWidth(w, bidiLine, 4); got != 16 {
		t.Fatalf("line width with markers: %d, want 16", got)
	}
}

// The "|" end marker closes each marked fragment: it renders at the reading
// end of a foreign-direction run — left of a reversed RTL fragment, right of
// a returning LTR fragment.
func TestShowBidiEndMarkers(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	w.ViewState.ShowBidi = true
	_ = e
	e.performRender()
	plain := stripAnsi(out.String())
	// Full marked visual: "abc |םולש<> xyz|".
	if !strings.Contains(plain, "abc |םולש<> xyz|") {
		t.Fatal("end markers should close both foreign fragments")
	}
}

// An explicit direction control renders as its own marker (one column, no
// synthetic duplicate).
func TestShowBidiExplicitControl(t *testing.T) {
	content := "ab‏אב cd" // RLM leads the RTL fragment
	e, w, out := newRenderedEditor(t, content+"\n")
	w.ViewState.ShowBidi = true
	_ = e
	e.performRender()
	plain := stripAnsi(out.String())
	// The RLM paints as "<" at the fragment's leading (right) edge; exactly
	// one "<" — no synthetic marker was added on top of it.
	if !strings.Contains(plain, "בא<> cd") {
		t.Fatal("control should render as the fragment's marker")
	}
	if strings.Contains(plain, "<<") {
		t.Fatal("no double marker for a control-led fragment")
	}
}

// showBidi is an option like showInvisibles: config default plus per-window
// set_option.
func TestShowBidiOption(t *testing.T) {
	e, w := newTestEditor(t, "x\n", "showBidi=true")
	// The [general] value lands in the editor config (windows created through
	// the editor's own paths inherit it; the test harness window is created
	// directly, like showInvisibles).
	if !e.Config.ShowBidi {
		t.Fatal("config should parse showBidi=true")
	}
	e.PawScript.ExecuteAsync("set_option 'showBidi', 'true'")
	if !w.ViewState.ShowBidi {
		t.Fatal("set_option should enable the window's showBidi")
	}
	e.PawScript.ExecuteAsync("set_option 'showBidi', 'false'")
	if w.ViewState.ShowBidi {
		t.Fatal("set_option should clear the window's showBidi")
	}
	if v, _ := e.getOption(w, "showBidi"); v != "no" {
		t.Fatalf("get_option showBidi: %q", v)
	}
}

// With showBidi on, the caret covers the cell of the rune it precedes. On an
// LTR-base line the carets inside an RTL run sit ON each Hebrew letter's own
// cell (not the transition marker or the next letter one cell right);
// positions in LTR text use the same on-cell convention; end of line after an
// RTL run rests one cell past its reading-last character, leftward.
func TestShowBidiCaretRunEnds(t *testing.T) {
	e, w := newTestEditor(t, "שלום abc\n")
	w.ViewState.ShowBidi = true
	line := "שלום abc"
	// Visual: "|םולש<> abc|" — walking the Hebrew word ON each letter: ש(4),
	// ל(3), ו(2), ם(1); then position 4 is before the space (LTR) at col 7.
	wantWalk := []int{4, 3, 2, 1, 7}
	for p, want := range wantWalk {
		if got := e.caretVisualColumn(w, line, p, 4); got != want {
			t.Fatalf("pos %d: col %d, want %d", p, got, want)
		}
	}
	if got := e.caretVisualColumn(w, line, 5, 4); got != 8 {
		t.Fatalf("pos 5 (inside ' abc'): col %d, want 8", got)
	}

	// A trailing RTL run: EOL rests one cell past its reading-last character
	// (leftward) — which is exactly the fragment's "|" end-marker cell.
	e2, w2 := newTestEditor(t, "abc שלום\n")
	w2.ViewState.ShowBidi = true
	if got := e2.caretVisualColumn(w2, "abc שלום", 8, 4); got != 4 {
		t.Fatalf("EOL after RTL run: col %d, want 4 (the | cell)", got)
	}

	// Without showBidi the unmarked conventions hold (regression guard).
	e4, w4 := newTestEditor(t, "abc שלום\n")
	if got := e4.caretVisualColumn(w4, "abc שלום", 8, 4); got != 3 {
		t.Fatalf("unmarked EOL convention changed: col %d, want 3", got)
	}
}

// The full caret walk over "[general]" under direction=rtl with showBidi on
// (visual: "[<>general]" right-aligned; the end brackets are the mirrored
// forms of the logical brackets). Forward logical movement visits: the
// rightmost bracket (crossed the opening bracket), the ">" entering the LTR
// island, then one cell left of each crossed letter (where an RTL-typed
// character would land), the "<" entering the closing bracket's fragment, and
// finally the pad cell LEFT of the line — because the next RTL character
// typed at EOL continues the line leftward past the bracket.
func TestShowBidiBracketLineWalk(t *testing.T) {
	e, w := newTestEditor(t, "[general]\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.ViewState.ShowBidi = true
	line := "[general]"
	// Visual now "|[<>general|]" (end markers flanking both foreign
	// fragments). The caret sits on the cell of the rune at the caret, and
	// EOL rests one past the closing bracket, leftward — the "|" cell.
	want := []int{12, 4, 5, 6, 7, 8, 9, 10, 1, 0}
	for p, wc := range want {
		if got := e.caretVisualColumn(w, line, p, 4); got != wc {
			t.Fatalf("pos %d: col %d, want %d", p, got, wc)
		}
	}
}

// A combining diacritic as the last character shares its base's cell, so the
// caret adjacent to it rests on the side the BASE character dictates — not
// one cell off from the mark's phantom column.
func TestShowBidiCombiningMarkCaretSide(t *testing.T) {
	// "abc אְ": alef + combining sheva end the line. EOL must rest one cell
	// left of the ALEF (col 4, the fragment's "|" cell), exactly as it would
	// without the mark.
	line := "abc אְ"
	e, w := newTestEditor(t, line+"\n")
	w.ViewState.ShowBidi = true
	if got := e.caretVisualColumn(w, line, 6, 4); got != 4 {
		t.Fatalf("EOL after base+mark: col %d, want 4", got)
	}
	// Same line without the mark: identical resting place.
	bare := "abc א"
	e2, w2 := newTestEditor(t, bare+"\n")
	w2.ViewState.ShowBidi = true
	if got := e2.caretVisualColumn(w2, bare, 5, 4); got != 4 {
		t.Fatalf("EOL after bare base: col %d, want 4", got)
	}
	// Unmarked mode gets the same treatment.
	e3, w3 := newTestEditor(t, line+"\n")
	if got := e3.caretVisualColumn(w3, line, 6, 4); got != 3 {
		t.Fatalf("unmarked EOL after base+mark: col %d, want 3", got)
	}
}

// At an automatic direction boundary the renderer paints a SECONDARY cursor:
// the cell just beyond the other end of the caret's fragment, re-drawn in
// reverse video — the other visual interpretation of the same logical
// position. Not shown mid-fragment, nor when an explicit direction control
// expresses the boundary (it is its own marker).
func TestShowBidiSecondaryCursor(t *testing.T) {
	// "[general]" under rtl+showBidi (visual "|[<>general|]"): caret at pos 1
	// (between the opening bracket and g) — primary on g; the secondary
	// rests one cell past the bracket in ITS (RTL) direction, which is now
	// the LTR island's "|" end-marker cell.
	e, w, out := newRenderedEditor(t, "[general]\n")
	e.PawScript.ExecuteAsync("set_option 'direction', 'rtl'")
	w.ViewState.ShowBidi = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	out.Reset()
	e.performRender()
	// The back buffer paints the reversed cell with \x1b[7m in its style; the
	// next cell's absolute style clears reverse, so the inversion is confined to
	// this one glyph. Assert reverse-video immediately precedes the marker.
	if !strings.Contains(out.String(), "\x1b[7m|") {
		t.Fatal("rune 2: secondary should invert the fragment's | end-marker cell")
	}

	// Caret at pos 8 (rune 9, between l and the closing bracket): secondary
	// one cell past the l in ITS (LTR) direction — again the "|" cell that
	// ends the island.
	w.SetCursorPos(window.Position{Line: 0, Rune: 8})
	e.Renderer.ForceRedraw() // both carets' secondaries land on the same "|" cell
	out.Reset()
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[7m|") {
		t.Fatal("rune 9: secondary should invert the | cell ending the island")
	}

	// Mid-fragment: no secondary.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.Renderer.ForceRedraw()
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), "\x1b[7m") {
		t.Fatal("no secondary cursor inside a fragment")
	}
}

// LTR base: caret entering the Hebrew word (primary hovers the "<"); the
// secondary rests one cell past the preceding space in ITS (LTR) direction —
// now the Hebrew fragment's "|" end-marker cell, inverted.
func TestShowBidiSecondaryCursorLTRBase(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	w.ViewState.ShowBidi = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 4})
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[7m|") {
		t.Fatal("expected the fragment's | end-marker cell inverted")
	}
}

// A boundary expressed by an explicit direction control gets NO secondary
// cursor: the control renders as its own marker.
func TestShowBidiSecondaryCursorSkipsControls(t *testing.T) {
	content := "ab‏אב cd" // RLM before the hebrew
	e, w, out := newRenderedEditor(t, content+"\n")
	w.ViewState.ShowBidi = true
	// Caret between RLM and א: boundary runes include the control.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.performRender()
	if strings.Contains(out.String(), "\x1b[7m") {
		t.Fatal("no secondary cursor at a control-expressed boundary")
	}
}

// The dual cursor appears even without showBidi: the boundary ambiguity exists
// on any bidirectional line, markers or not.
func TestSecondaryCursorWithoutShowBidi(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	// showBidi OFF: unmarked layout (no <> marker cells). Caret entering the
	// Hebrew word: secondary rests one cell past the preceding space in its
	// LTR direction — the run's leftmost cell (ם).
	w.SetCursorPos(window.Position{Line: 0, Rune: 4})
	e.performRender()
	if !strings.Contains(out.String(), "\x1b[7mם") {
		t.Fatal("secondary cursor should render without showBidi")
	}

	// Still absent mid-fragment and on plain LTR lines.
	w.SetCursorPos(window.Position{Line: 0, Rune: 2})
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), "\x1b[7m") {
		t.Fatal("no secondary cursor inside a fragment")
	}
}

// EOL caret under an RTL base ending in LTR text must sit one cell right of
// the last character (where further LTR text goes), NOT the line's right edge
// (the gutter). Regression for "Hello. שלום. Testin". Independent of showBidi.
func TestBidiRTLBaseEOLNotGutter(t *testing.T) {
	line := "Hello. שלום. Testin"
	for _, show := range []bool{false, true} {
		e, w := newTestEditor(t, line+"\n", "direction=rtl")
		w.ViewState.ShowBidi = show
		rn := []rune(line)
		got := e.caretVisualColumn(w, line, len(rn), 4)
		// 'n' (last, LTR) is at visual col 5; caret rests at col 6.
		want := 6
		if show {
			want = 7 // one extra marker cell precedes "Testin" under showBidi
		}
		if got != want {
			t.Fatalf("showBidi=%v EOL caret col %d, want %d (not the line's right edge)", show, got, want)
		}
	}
}

// A lam-alef ligature occupies ONE visual cell across its two logical code
// points: the caret on either side highlights the same cell, and deleting
// half breaks the ligature back into separate letters.
func TestLamAlefLigatureCell(t *testing.T) {
	e, w := newTestEditor(t, "بلا\n", "direction=rtl") // beh lam alef
	line := "بلا"                                      // beh lam alef
	// The lam-alef pair is one cell: total width is beh(1)+ligature(1) = 2.
	if got := e.lineVisualWidth(w, line, 4); got != 2 {
		t.Fatalf("line width with ligature: %d, want 2", got)
	}
	// Caret between lam and alef (rune 2) and after the lam-before-alef... both
	// the lam position (1) and the absorbed alef (2) resolve to the same cell.
	c1 := e.caretVisualColumn(w, line, 1, 4)
	c2 := e.caretVisualColumn(w, line, 2, 4)
	if c1 != c2 {
		t.Fatalf("lam (col %d) and absorbed alef (col %d) should share a cell", c1, c2)
	}

	// Delete the alef: the ligature breaks; the lam re-shapes on its own.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3}) // after alef
	e.executeCommand("del_char_prior")                // remove the alef
	if got := docContent(w); got != "بل" {
		t.Fatalf("after deleting alef: %q, want بل", got)
	}
	// Now "بل" is two cells (no ligature).
	if got := e.lineVisualWidth(w, "بل", 4); got != 2 {
		t.Fatalf("broken-apart width: %d, want 2", got)
	}
}

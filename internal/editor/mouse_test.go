package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Mouse input end to end, through the pseudo-key stream the key layer emits:
// a click sets the caret; a press on a browse-mode button shows the pressed
// style; dragging off cancels the click; press+release on the button follows
// the link exactly as keyboard navigation would.
func TestMouseButtonPressDragFollow(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")
	e.performRender() // establish window geometry (ContentX/Y, widths)
	if !w.BrowseActive {
		t.Fatal("the wiki page should have auto-armed browse mode")
	}
	src := w.Buffer

	row := w.ContentY + 1 // line 0 of the buffer, 1-based screen row
	colOf := func(cell int) int { return w.ContentX + 1 + cell }
	click := func(kind string, cell int) {
		if !e.handleMouseKey("Mouse@" + itoa(colOf(cell)) + "," + itoa(row)) {
			t.Fatal("position pseudo-key should be consumed")
		}
		if !e.handleMouseKey(kind) {
			t.Fatal("mouse pseudo-key should be consumed")
		}
	}

	// A plain click sets the caret to the clicked cell.
	click("MouseLeftPress", 1) // the 'o' of "go"
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 1 {
		t.Fatalf("click should set the caret; got %+v", got)
	}
	if e.mousePressed.active {
		t.Fatal("a click outside any button must not arm the pressed state")
	}
	click("MouseLeftRelease", 1)

	// Press ON the button (the display shows "go ⟨ other ⟩▐ now"; cell 5 is
	// inside the button): pressed state arms, the caret parks in the span,
	// and the pressed style paints.
	click("MouseLeftPress", 5)
	if !e.mousePressed.active {
		t.Fatal("pressing a button should arm the pressed state")
	}
	if e.focusedLinkButton(w) == nil {
		t.Fatal("the pressed button should be the focused button")
	}
	var out strings.Builder
	_ = out
	// The pressed color must appear in the next frame.
	eOut := renderTo(e)
	if !strings.Contains(eOut, "\x1b[0;97;44m") {
		t.Fatal("the pressed button should paint in buttonPressed")
	}

	// Dragging off the button cancels the click for good.
	if !e.handleMouseKey("MouseLeftDrag@" + itoa(colOf(0)) + "," + itoa(row)) {
		t.Fatal("drag pseudo-key should be consumed")
	}
	if e.mousePressed.active {
		t.Fatal("dragging off the button must cancel the pressed state")
	}
	click("MouseLeftRelease", 5)
	if w.Buffer != src {
		t.Fatal("a cancelled click must not follow")
	}

	// Press and release on the button: the follow triggers.
	click("MouseLeftPress", 5)
	click("MouseLeftRelease", 6)
	if e.mousePressed.active {
		t.Fatal("release must clear the pressed state")
	}
	wantPath := filepath.Join(root, "w", "other.txt")
	if w.Buffer == src || w.Buffer.GetFilename() != wantPath {
		t.Fatalf("press+release on a button should follow; got %q", w.Buffer.GetFilename())
	}
	// And history returns, as with any follow.
	if !e.navHistory(-1) || w.Buffer != src {
		t.Fatal("a mouse follow is a normal follow: history returns")
	}
}

// The scroll wheel scrolls the window under the pointer.
func TestMouseScroll(t *testing.T) {
	lines := strings.Repeat("line\n", 60)
	e, w, _ := wikiTreeEditor(t, map[string]string{"w/long.txt": lines}, "w/long.txt")
	e.performRender()
	row := w.ContentY + 1
	col := w.ContentX + 1
	e.handleMouseKey("Mouse@" + itoa(col) + "," + itoa(row))
	e.handleMouseKey("MouseScrollDown")
	if w.ViewState.ViewOffsetY != 3 {
		t.Fatalf("scroll down should advance the view; off=%d", w.ViewState.ViewOffsetY)
	}
	e.handleMouseKey("MouseScrollUp")
	if w.ViewState.ViewOffsetY != 0 {
		t.Fatalf("scroll up should return; off=%d", w.ViewState.ViewOffsetY)
	}
}

// renderTo renders a frame and returns what was written to the harness
// terminal.
func renderTo(e *Editor) string {
	type outGetter interface{ String() string }
	e.performRender()
	if og, ok := e.Config.Terminal.Output.(outGetter); ok {
		return og.String()
	}
	return ""
}

// Non-mouse keys pass through untouched.
func TestMouseKeyPassthrough(t *testing.T) {
	e, _, _ := wikiTreeEditor(t, map[string]string{"w/p.txt": "x\n"}, "w/p.txt")
	for _, k := range []string{"a", "return", "C-c", "S-tab", "Mou", "MouseTrap"} {
		want := strings.HasPrefix(k, "Mouse")
		if got := e.handleMouseKey(k); got != want {
			t.Fatalf("handleMouseKey(%q) = %v, want %v", k, got, want)
		}
	}
	_ = window.Position{}
}

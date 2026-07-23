package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// The I-beam region mew publishes after a render is the FOCUSED window's
// editable content rectangle (1-based cells), so a graphical host shows the
// I-beam over text and the arrow over the gutter, modebar, and other chrome.
func TestPointerRegionPublished(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "hello\nworld\n", "[options]\nshowLineNumbers=yes\n")
	var last [4]int
	var arrows []PointerArrowSpan
	e.Config.PointerRegion = func(col, row, w, h int, a []PointerArrowSpan) {
		last = [4]int{col, row, w, h}
		arrows = a
	}
	e.createPluginWindows()
	e.performRender()

	want := [4]int{doc.ContentX + 1, doc.ContentY + 1, doc.ContentWidth, doc.ContentHeight}
	if last != want {
		t.Fatalf("pointer region = %v, want the focused doc content rect %v", last, want)
	}
	if len(arrows) != 0 {
		t.Fatalf("a plain document has no button exclusions; got %v", arrows)
	}
	// The gutter sits left of the region.
	if doc.ViewState.ShowLineNumbers && doc.LineNumWidth > 0 && last[0] <= 1 {
		t.Fatal("the I-beam region should start past the gutter")
	}
	// The modebar row is chrome — outside the region's rows.
	if mw := e.WindowManager.GetWindow(e.Modebar.WindowID()); mw != nil {
		mrow := mw.ContentY + 1
		if mrow >= last[1] && mrow < last[1]+last[3] {
			t.Fatal("the modebar row must lie outside the I-beam region")
		}
	}
}

// When a prompt takes focus the region becomes the PROMPT's own field, so the
// document area shows the arrow (a cue that input is awaited at the prompt)
// while the prompt field shows the I-beam.
func TestPointerRegionFollowsPromptFocus(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "hello\n", "[options]\n")
	var last [4]int
	e.Config.PointerRegion = func(col, row, w, h int, _ []PointerArrowSpan) { last = [4]int{col, row, w, h} }
	e.createPluginWindows()
	e.performRender()
	docRect := [4]int{doc.ContentX + 1, doc.ContentY + 1, doc.ContentWidth, doc.ContentHeight}
	if last != docRect {
		t.Fatalf("precondition: region should be the doc rect %v, got %v", docRect, last)
	}

	e.PromptForInput("Find: ", "", func(string, bool) {})
	e.performRender()
	pw := e.WindowManager.GetFocusedWindow()
	if pw == nil || pw.Type != window.PromptWindow {
		t.Fatal("the prompt should hold focus")
	}
	promptRect := [4]int{pw.ContentX + 1, pw.ContentY + 1, pw.ContentWidth, pw.ContentHeight}
	if last != promptRect {
		t.Fatalf("with a prompt focused, region = %v, want the prompt field %v", last, promptRect)
	}
	if last == docRect {
		t.Fatal("the region must move off the document while a prompt is focused")
	}
}

// A browse-mode link button that sits inside the text is published as an
// arrow-exclusion span, so the host shows the arrow over the button and the
// I-beam over the surrounding text.
func TestPointerRegionExcludesBrowseButtons(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, _ := wikiTreeEditor(t, files, "w/page.txt")
	var arrows []PointerArrowSpan
	e.Config.PointerRegion = func(_, _, _, _ int, a []PointerArrowSpan) { arrows = a }
	e.performRender()

	if !w.BrowseActive {
		t.Fatal("precondition: the wiki page should be in browse mode")
	}
	if len(arrows) == 0 {
		t.Fatal("the browse-mode link button should publish an arrow-exclusion span")
	}
	a := arrows[0]
	if a.Row != w.ContentY+1 {
		t.Fatalf("button span row = %d, want the first content row %d", a.Row, w.ContentY+1)
	}
	if a.Col < w.ContentX+1 || a.Width <= 0 {
		t.Fatalf("button span should sit within the content columns: %+v", a)
	}
}

// A modal prompt stands the modebar nav buttons down: no capture on press and
// no hover styling, even with back/forward history present.
func TestModebarNavStandsDownForPrompt(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "A\n", "[options]\n")
	doc.SwapBuffer(buffer.NewFromString("B\n"), func(*buffer.Buffer) bool { return false })
	e.createPluginWindows()
	e.performRender()
	bx, by, ok := e.findModebarNavButton(1) // back button
	if !ok {
		t.Fatal("expected a back button")
	}
	// Hover lights it while the document is focused.
	e.modebarNavHoverAt(bx, by)
	if e.modebarNavHover != 1 {
		t.Fatal("hover should light the back button when the doc is focused")
	}

	// Raise a prompt: hover no longer lights, and a press does not capture.
	e.PromptForInput("Find: ", "", func(string, bool) {})
	if !e.promptHasPriority() {
		t.Fatal("the prompt should hold focus")
	}
	e.modebarNavHoverAt(bx, by)
	if e.modebarNavHover != 0 {
		t.Fatal("hover must not light a nav button while a prompt holds focus")
	}
	if e.modebarNavPressAt(bx, by) {
		t.Fatal("a nav press must not be consumed while a prompt holds focus")
	}
}

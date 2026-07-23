package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// The pointer shows the I-beam only over the focused window's editable text —
// its content area and the blank rows below the document — and the arrow
// everywhere else (the gutter, the modebar, an unfocused pane).
func TestPointerIBeamOverFocusedText(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "hello world\nsecond line\n", "[options]\nshowLineNumbers=yes\n")
	e.createPluginWindows()
	e.performRender()

	// A column inside the content area (past the gutter) over a text row.
	tx := doc.ContentX + 2
	ty := doc.ContentY + 1 // first content row, 1-based
	if !e.pointerShowIBeam(tx, ty) {
		t.Fatal("pointer over focused text should show the I-beam")
	}

	// The gutter (line-number) columns are chrome: arrow.
	if doc.ViewState.ShowLineNumbers && doc.LineNumWidth > 0 {
		if e.pointerShowIBeam(1, ty) {
			t.Fatal("pointer over the gutter should show the arrow, not the I-beam")
		}
	}

	// A blank row below the last line (still click-to-EOF) is text: I-beam.
	belowY := doc.ContentY + 1 + 5 // several rows below the 2-line document
	if !e.pointerShowIBeam(tx, belowY) {
		t.Fatal("pointer below the document should show the I-beam (click-to-EOF area)")
	}

	// The modebar row is chrome: arrow.
	mw := e.WindowManager.GetWindow(e.Modebar.WindowID())
	if mw != nil {
		if e.pointerShowIBeam(tx, mw.ContentY+1) {
			t.Fatal("pointer over the modebar should show the arrow")
		}
	}
}

// While a prompt holds focus the document area shows the ARROW (a cue that
// input is awaited at the prompt), but the prompt's own field shows the I-beam.
func TestPointerIBeamPromptFocus(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "hello world\n", "[options]\n")
	e.createPluginWindows()
	e.performRender()
	tx := doc.ContentX + 2
	ty := doc.ContentY + 1
	if !e.pointerShowIBeam(tx, ty) {
		t.Fatal("precondition: doc text should show the I-beam when the doc is focused")
	}

	// Raise a prompt (it takes focus).
	e.PromptForInput("Find: ", "", func(string, bool) {})
	e.performRender()
	if !e.promptHasPriority() {
		t.Fatal("the prompt should hold focus")
	}
	// The document text now shows the arrow, not the I-beam.
	if e.pointerShowIBeam(tx, ty) {
		t.Fatal("with a prompt focused, the document area should show the arrow")
	}
	// The prompt's own field shows the I-beam.
	pw := e.WindowManager.GetFocusedWindow()
	if pw == nil || pw.Type != window.PromptWindow {
		t.Fatal("focused window should be the prompt")
	}
	if !e.pointerShowIBeam(pw.ContentX+1, pw.ContentY+1) {
		t.Fatal("the prompt's own field should show the I-beam")
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

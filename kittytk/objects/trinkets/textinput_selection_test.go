package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"

	"github.com/phroun/kittytk/core"
)

// fakeClip is a minimal PopupController that also carries a clipboard,
// standing in for a TearOffHost when the input has no desktop.
type fakeClip struct {
	buf    string
	popups map[string]*core.PopupRequest
}

func (f *fakeClip) RegisterPopup(r *core.PopupRequest)                          { f.popups[r.ID] = r }
func (f *fakeClip) UnregisterPopup(id string)                                   { delete(f.popups, id) }
func (f *fakeClip) MapToScreen(_ core.Trinket, p core.UnitPoint) core.UnitPoint { return p }
func (f *fakeClip) ScreenBounds() core.UnitRect                                 { return core.UnitRect{Width: 1000, Height: 1000} }
func (f *fakeClip) Clipboard() string                                           { return f.buf }
func (f *fakeClip) SetClipboard(s string)                                       { f.buf = s }

func newClippedInput(text string) (*TextInput, *fakeClip) {
	ti := NewTextInput()
	ti.SetText(text)
	clip := &fakeClip{popups: map[string]*core.PopupRequest{}}
	ti.SetPopupController(clip)
	return ti, clip
}

// Shift+arrow extends the selection from the pre-move caret; the
// bare "S-Left" spelling folds to a shift-extend.
func TestTextInputShiftArrowSelection(t *testing.T) {
	ti, _ := newClippedInput("hello")
	ti.SetCursorPosition(5) // end
	ti.HandleKeyPress(core.KeyPressEvent{Key: "S-Left", Modifiers: core.ShiftModifier})
	ti.HandleKeyPress(core.KeyPressEvent{Key: "S-Left", Modifiers: core.ShiftModifier})
	if got := ti.SelectedText(); got != "lo" {
		t.Errorf("shift-left selection = %q, want %q", got, "lo")
	}
	if ti.CursorPosition() != 3 {
		t.Errorf("caret at %d, want 3", ti.CursorPosition())
	}
}

// Shift+Ctrl+A / Shift+Ctrl+E extend to the ends.
func TestTextInputShiftCtrlHomeEnd(t *testing.T) {
	ti, _ := newClippedInput("hello world")
	ti.SetCursorPosition(6)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "C-S-a", Modifiers: core.ControlModifier | core.ShiftModifier})
	if got := ti.SelectedText(); got != "hello " {
		t.Errorf("shift-ctrl-A = %q, want %q", got, "hello ")
	}
	ti.SetCursorPosition(6)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "C-S-e", Modifiers: core.ControlModifier | core.ShiftModifier})
	if got := ti.SelectedText(); got != "world" {
		t.Errorf("shift-ctrl-E = %q, want %q", got, "world")
	}
}

// Typing/pasting over a selection overwrites it.
func TestTextInputOverwriteSelection(t *testing.T) {
	ti, clip := newClippedInput("hello")
	ti.SelectAll()
	ti.HandleKeyPress(core.KeyPressEvent{Text: "X"})
	if ti.Text() != "X" {
		t.Errorf("typing over selection = %q, want %q", ti.Text(), "X")
	}
	ti.SelectAll()
	clip.SetClipboard("paste")
	ti.Paste()
	if ti.Text() != "paste" {
		t.Errorf("paste over selection = %q, want %q", ti.Text(), "paste")
	}
}

// Cut/Copy/Paste round-trip through the clipboard bridge.
func TestTextInputClipboardActions(t *testing.T) {
	ti, clip := newClippedInput("hello world")
	// Select "world".
	ti.SetCursorPosition(6)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "C-S-e", Modifiers: core.ControlModifier | core.ShiftModifier})
	ti.Copy()
	if clip.buf != "world" {
		t.Errorf("copy put %q on clipboard, want %q", clip.buf, "world")
	}
	ti.Cut()
	if ti.Text() != "hello " {
		t.Errorf("after cut = %q, want %q", ti.Text(), "hello ")
	}
	ti.SetCursorPosition(0)
	ti.Paste()
	if ti.Text() != "worldhello " {
		t.Errorf("after paste = %q, want %q", ti.Text(), "worldhello ")
	}
}

// selectWordAt expands to the run of same-class characters around the click.
func TestTextInputSelectWordAt(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello world_42 a,b")
	// idx: h0 e1 l2 l3 o4 _5=space w6 o7 r8 l9 d10 _11 4:12 2:13 _14=space a15 ,16 b17

	ti.selectWordAt(2) // inside "hello"
	if got := ti.SelectedText(); got != "hello" {
		t.Errorf("word at 2 = %q, want hello", got)
	}
	ti.selectWordAt(8) // inside "world_42" (underscore and digits are word chars)
	if got := ti.SelectedText(); got != "world_42" {
		t.Errorf("word at 8 = %q, want world_42", got)
	}
	ti.selectWordAt(5) // the space between "hello" and "world_42"
	if got := ti.SelectedText(); got != " " {
		t.Errorf("space at 5 = %q, want a single space", got)
	}
	ti.selectWordAt(16) // the comma - a lone punctuation character
	if got := ti.SelectedText(); got != "," {
		t.Errorf("punctuation at 16 = %q, want a single comma", got)
	}
}

// Double-click selects the word under the pointer, triple-click selects all.
func TestTextInputMultiClickSelection(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := rasterNew(240, 24)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	ti := NewTextInput()
	ti.SetText("hello world")
	ti.SetBounds(core.UnitRect{Width: 220, Height: 16})

	click := func() {
		ti.HandleMousePress(core.MousePressEvent{X: 3, Y: 4, Button: core.LeftButton})
	}

	click() // single: just places the caret, no selection
	if ti.HasSelection() {
		t.Errorf("single click should not select, got %q", ti.SelectedText())
	}
	click() // double: the word under the pointer
	if got := ti.SelectedText(); got != "hello" {
		t.Errorf("double click = %q, want hello", got)
	}
	click() // triple: everything
	if got := ti.SelectedText(); got != "hello world" {
		t.Errorf("triple click = %q, want the whole text", got)
	}
}

// A right click opens the context menu on the popup controller.
func TestTextInputContextMenu(t *testing.T) {
	ti, clip := newClippedInput("hello")
	ti.SetBounds(core.UnitRect{Width: 200, Height: 16})
	ti.HandleMousePress(core.MousePressEvent{X: 10, Y: 4, Button: core.RightButton})
	if len(clip.popups) != 1 {
		t.Fatalf("right click registered %d popups, want 1", len(clip.popups))
	}
}

// On a pixel surface, when the selection runs past the last visible
// glyph (caret far left, the other end scrolled off the right), the
// leftover sliver on the right edge is painted in the selection
// color, not the box fill.
func TestTextInputSelectionSliverFillsToEdge(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := rasterNew(240, 24)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	ti := NewTextInput()
	ti.SetText("proportional selection running off the right edge")
	ti.SetBounds(core.UnitRect{Width: 220, Height: 16})
	ti.SetFocus()
	// Caret to the end, then Shift+Home: caret jumps left, the
	// selection anchor stays at the (now off-screen) right end.
	ti.SetCursorPosition(len("proportional selection running off the right edge"))
	ti.HandleKeyPress(core.KeyPressEvent{Key: "S-Home", Modifiers: core.ShiftModifier})
	ti.Paint(core.NewPainter(px))

	// The rightmost column, mid-row, must be the selection background
	// (ColorWhite = the standard/dim white, silver #AAAAAA -> R=170 in
	// the EGA palette), not the focused box fill (cyan #00AAAA -> R=0).
	img := px.Image()
	r, _, _, _ := img.At(219, 8).RGBA()
	if r>>8 < 100 {
		t.Errorf("trailing sliver not selection-colored: R=%d (want silver, ~170; cyan fill R=0)", r>>8)
	}
}

func rasterNew(w, h int) (*raster.Backend, error) { return raster.New(w, h) }

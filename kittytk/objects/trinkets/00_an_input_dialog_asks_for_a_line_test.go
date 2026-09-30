package trinkets

// An input dialog asks for one line of text. Its prompt, field and buttons are
// trinkets in the window's own tree, so the focus ring, the pointer and the
// buttons' letters reach them like any window's. It opens with the field
// focused and the offered text selected, and it is answered once: Return in
// the field or OK accepts, Cancel cancels, and closing the window says nothing
// was chosen. Keys reach only what is focused -- nothing is caught at the
// window -- so Esc does nothing, and Return on Cancel cancels.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

type heard struct {
	calls    int
	text     string
	accepted bool
}

func renameDialog(t *testing.T) (*InputDialog, *heard) {
	t.Helper()
	core.SetTextMeasurer(nil)
	d := NewInputDialog("Rename", "New name for the file:", "draft.txt")
	h := &heard{}
	d.SetOnFinished(func(text string, accepted bool) {
		h.calls++
		h.text, h.accepted = text, accepted
	})
	return d, h
}

func press(w *window.Window, k string) bool {
	text := ""
	if len([]rune(k)) == 1 {
		text = k
	}
	return w.HandleKeyPress(core.KeyPressEvent{Key: k, Text: text})
}

func typeInto(w *window.Window, s string) {
	for _, r := range s {
		press(w, string(r))
	}
}

func TestTheFieldIsFocusedWithTheOfferSelected(t *testing.T) {
	d, _ := renameDialog(t)
	if f := d.FocusManager().FocusedTrinket(); f != core.Trinket(d.input) {
		t.Fatalf("the dialog opens with %T focused, want the field", f)
	}
	typeInto(&d.Window, "notes.txt")
	if got := d.Text(); got != "notes.txt" {
		t.Errorf("typing over the offer left %q, want notes.txt", got)
	}
}

func TestReturnInTheFieldAccepts(t *testing.T) {
	d, h := renameDialog(t)
	typeInto(&d.Window, "notes.txt")
	press(&d.Window, "Return")
	if h.calls != 1 || !h.accepted || h.text != "notes.txt" {
		t.Fatalf("Return in the field was heard %d times as (%q, %v), want once as (notes.txt, true)",
			h.calls, h.text, h.accepted)
	}
	if !d.Accepted() || d.Result() != "notes.txt" || d.Answer() != ResultOK {
		t.Errorf("the dialog reports accepted %v, result %q, answer %v", d.Accepted(), d.Result(), d.Answer())
	}
	if !d.IsClosed() {
		t.Error("the dialog stayed open after it was answered")
	}
}

// Return goes to whatever is focused: on OK it accepts, on Cancel it cancels.
func TestReturnPressesTheFocusedButton(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tabs     int
		accepted bool
		text     string
	}{
		{"OK", 1, true, "draft.txt"},
		{"Cancel", 2, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, h := renameDialog(t)
			for i := 0; i < tc.tabs; i++ {
				press(&d.Window, "Tab")
			}
			press(&d.Window, "Return")
			settle(t, func() bool { return h.calls > 0 })
			if h.calls != 1 || h.accepted != tc.accepted || h.text != tc.text {
				t.Errorf("Return on %s was heard %d times as (%q, %v), want once as (%q, %v)",
					tc.name, h.calls, h.text, h.accepted, tc.text, tc.accepted)
			}
			if d.Accepted() != tc.accepted {
				t.Errorf("after Return on %s, Accepted() = %v", tc.name, d.Accepted())
			}
		})
	}
}

// Nothing is caught at the window. Esc in the field is the field's, and the
// field has no use for it; Esc anywhere else is nobody's.
func TestEscapeDoesNotAnswer(t *testing.T) {
	d, h := renameDialog(t)
	press(&d.Window, "Escape")
	press(&d.Window, "Tab")
	press(&d.Window, "Escape")
	if h.calls != 0 || d.IsClosed() {
		t.Errorf("Esc answered the dialog: heard %d times, closed %v", h.calls, d.IsClosed())
	}
}

// At rest, the body holds the focus and offers the buttons by letter; in the
// field, a letter is typed.
func TestTheButtonsAnswerToTheirLettersAtRest(t *testing.T) {
	d, h := renameDialog(t)
	press(&d.Window, "c")
	if h.calls != 0 || d.Text() != "c" {
		t.Fatalf("c in the field: heard %d times, field %q; want it typed", h.calls, d.Text())
	}
	press(&d.Window, "S-Tab")
	if !d.FocusManager().AtRest() {
		t.Fatalf("Shift-Tab off the field came to %T, want the rest", d.FocusManager().FocusedTrinket())
	}
	press(&d.Window, "c")
	settle(t, func() bool { return h.calls > 0 })
	if h.calls != 1 || h.accepted {
		t.Errorf("c at rest was heard %d times, accepted %v; want Cancel once", h.calls, h.accepted)
	}
}

func TestClosingTheWindowChoosesNothing(t *testing.T) {
	d, h := renameDialog(t)
	typeInto(&d.Window, "notes.txt")
	d.Close()
	if h.calls != 1 || h.accepted || h.text != "" {
		t.Errorf("closing was heard %d times as (%q, %v), want once as (\"\", false)", h.calls, h.text, h.accepted)
	}
	if d.Answer() != ResultNone || d.Accepted() || d.Result() != "" {
		t.Errorf("closing answered %v, accepted %v, result %q; want none, false, \"\"",
			d.Answer(), d.Accepted(), d.Result())
	}
}

// The first answer is the one given: a second button pressed before the first
// has finished, or the window closing behind the answer, changes nothing.
func TestOnlyTheFirstAnswerCounts(t *testing.T) {
	d, h := renameDialog(t)
	d.okButton.Click()
	d.cancelButton.Click()
	d.Close()
	if h.calls != 1 || !h.accepted || d.Answer() != ResultOK {
		t.Errorf("heard %d times, accepted %v, answer %v; want OK once", h.calls, h.accepted, d.Answer())
	}
}

// A press lands on a button through the window, as it would from the pointer.
func TestAClickReachesTheButtons(t *testing.T) {
	d, h := renameDialog(t)
	d.body.Layout()
	at := core.UnitPoint{}
	for t := core.Trinket(d.cancelButton); t != nil && t != core.Trinket(&d.Window); t = t.Parent() {
		b := t.Bounds()
		at.X += b.X
		at.Y += b.Y
	}
	cb := d.ContentBounds()
	at.X += cb.X + 1
	at.Y += cb.Y + 1
	d.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	d.HandleMouseRelease(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	settle(t, func() bool { return h.calls > 0 })
	if h.calls != 1 || h.accepted {
		t.Errorf("a click on Cancel was heard %d times, accepted %v; want Cancel once", h.calls, h.accepted)
	}
}

// The window is sized to hold what it shows: a long prompt widens it, and it
// is never narrower than forty columns.
func TestTheDialogFitsItsPrompt(t *testing.T) {
	d, _ := renameDialog(t)
	if got := d.prompt.Parent(); got != core.Container(d.body) {
		t.Errorf("the prompt's parent is %T, want the dialog's body", got)
	}
	if d.Type() != window.WindowTypeModal {
		t.Errorf("the dialog's type is %v, want modal", d.Type())
	}
	if d.Flags()&window.WindowFlagNoResize == 0 {
		t.Error("the dialog can be resized, though it is sized to what it shows")
	}
	m := d.EffectiveCellMetrics()
	if w := d.ContentBounds().Width; w < 40*m.UnitsPerCellWidth {
		t.Errorf("the content is %d wide, want at least forty columns (%d)", w, 40*m.UnitsPerCellWidth)
	}
	narrow := d.ContentBounds().Width
	d.SetPrompt("A much longer prompt that asks for rather more than the short one did:")
	if d.ContentBounds().Width <= narrow {
		t.Error("a longer prompt did not widen the dialog")
	}
	d.SetPrompt(strings.Repeat("a prompt that goes on ", 10))
	if w := d.ContentBounds().Width; w > 64*m.UnitsPerCellWidth {
		t.Errorf("a very long prompt made the content %d wide, want at most sixty-four columns (%d)",
			w, 64*m.UnitsPerCellWidth)
	}
	d.body.Layout()
	if b := d.cancelButton; b.Bounds().Width == 0 {
		t.Error("the buttons were given no room")
	}
}

// Over the wire it finishes as a message box does, with a result word, and
// carries what was typed only when the answer was OK.
func TestAnInputDialogFromTheWire(t *testing.T) {
	build := func(t *testing.T) (*InputDialog, func() []string) {
		t.Helper()
		core.SetTextMeasurer(nil)
		f, events := buildWithEvents(t, nil, `
dlg=new inputdialog title="Rename" prompt="New name:" text="draft.txt" placeholder="a name"
sub dlg finish
`)
		d := f.targets[0].(*InputDialog)
		return d, func() []string {
			var out []string
			for _, ev := range eventsOfType(*events, "finish") {
				r, _ := ev.Word("result")
				s, has := ev.Text("text")
				if has {
					r += " " + s
				}
				out = append(out, r)
			}
			return out
		}
	}

	d, finished := build(t)
	if d.Title() != "Rename" || d.Prompt() != "New name:" || d.Text() != "draft.txt" {
		t.Errorf("built as title %q, prompt %q, text %q", d.Title(), d.Prompt(), d.Text())
	}
	if got := d.Input().Placeholder(); got != "a name" {
		t.Errorf("placeholder = %q, want \"a name\"", got)
	}
	typeInto(&d.Window, "notes.txt")
	press(&d.Window, "Return")
	if got := finished(); len(got) != 1 || got[0] != "ok notes.txt" {
		t.Errorf("accepting told %q, want [ok notes.txt]", got)
	}

	d, finished = build(t)
	d.cancelButton.Click()
	if got := finished(); len(got) != 1 || got[0] != "cancel" {
		t.Errorf("cancelling told %q, want [cancel]", got)
	}

	d, finished = build(t)
	d.Close()
	if got := finished(); len(got) != 1 || got[0] != "none" {
		t.Errorf("closing told %q, want [none]", got)
	}

	// Taken away by the one who asked: nothing to tell them.
	d, finished = build(t)
	d.dismiss()
	if got := finished(); len(got) != 0 || !d.IsClosed() {
		t.Errorf("dismissing told %q and left closed %v, want nothing and closed", got, d.IsClosed())
	}
}

package trinkets

// A button's letter is live while any trinket in its parent chain, below the
// window, holds the focus: a panel or a scroll area that takes the focus
// offers every button inside it by letter, as a message box's area does. The
// button itself holding the focus does not count, nor does a field beside it,
// and while one letter's press is under way every other letter is withdrawn.

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

type letterForm struct {
	win        *window.Window
	group      *Panel
	field      *TextInput
	save, skip *Button
	saved      atomic.Int32
	skipped    atomic.Int32
}

// newLetterForm is a window holding a field and, beside it, a focusable group
// whose buttons sit one panel further down.
func newLetterForm(t *testing.T) *letterForm {
	t.Helper()
	f := &letterForm{}
	outer := NewPanel()
	f.field = NewTextInput()
	f.group = NewPanel()
	f.group.SetFocusPolicy(core.StrongFocus)
	row := NewPanel()
	f.save = NewButton("&Save")
	f.skip = NewButton("&S&kip")
	f.save.SetOnClick(func() { f.saved.Add(1) })
	f.skip.SetOnClick(func() { f.skipped.Add(1) })
	row.AddChild(f.save)
	row.AddChild(f.skip)
	f.group.AddChild(row)
	outer.AddChild(f.field)
	outer.AddChild(f.group)
	f.win = window.NewWindow("form")
	f.win.SetContent(outer)
	return f
}

func (f *letterForm) focus(t core.Trinket) { f.win.FocusManager().SetFocusedTrinket(t) }

func (f *letterForm) press(key string) bool {
	return f.win.HandleKeyPress(core.KeyPressEvent{Key: key, Text: key})
}

// settle waits out a press's animation.
func settle(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAFocusedPanelOffersItsButtonsByLetter(t *testing.T) {
	f := newLetterForm(t)
	f.focus(f.group)
	if got := f.save.liveMnemonic(); got != 0 {
		t.Errorf("Save's letter is at %d, want 0", got)
	}
	// Skip's first choice went to Save, written first; it falls back to K.
	if got := f.skip.liveMnemonic(); got != 1 {
		t.Errorf("Skip's letter is at %d, want 1 (the k)", got)
	}
	if !f.press("K") {
		t.Fatal("K was not spent with the group focused")
	}
	settle(t, func() bool { return f.skipped.Load() > 0 })
	if f.skipped.Load() != 1 || f.saved.Load() != 0 {
		t.Errorf("K pressed Skip %d times and Save %d, want 1 and 0", f.skipped.Load(), f.saved.Load())
	}
}

func TestNothingButAnAncestorOffersTheLetters(t *testing.T) {
	f := newLetterForm(t)
	f.focus(f.field)
	if f.save.liveMnemonic() >= 0 || f.skip.liveMnemonic() >= 0 {
		t.Error("a field beside the group offers its letters")
	}
	f.press("s")
	if f.field.Text() != "s" {
		t.Errorf("the field holds %q; the s typed into it went elsewhere", f.field.Text())
	}
	f.focus(f.save)
	if f.save.liveMnemonic() >= 0 || f.skip.liveMnemonic() >= 0 {
		t.Error("a focused button offers the letters")
	}
	time.Sleep(400 * time.Millisecond)
	if f.saved.Load() != 0 {
		t.Error("the s typed into the field pressed Save")
	}
}

// A hidden button takes no letter, so the next one gets it.
func TestAHiddenButtonGivesUpItsLetter(t *testing.T) {
	f := newLetterForm(t)
	f.focus(f.group)
	f.save.SetVisible(false)
	if got := f.skip.liveMnemonic(); got != 0 {
		t.Errorf("with Save hidden, Skip's letter is at %d, want 0 (the S)", got)
	}
}

// Once a letter's press is under way, every other letter is withdrawn until
// the press is done: a second letter pressed in the meantime does nothing.
func TestAPressWithdrawsTheOtherLettersUntilItIsDone(t *testing.T) {
	f := newLetterForm(t)
	f.focus(f.group)
	f.press("s")
	if f.skip.liveMnemonic() >= 0 {
		t.Error("Skip still offers its letter while Save's press is under way")
	}
	if got := f.save.liveMnemonic(); got != 0 {
		t.Errorf("Save's own letter is at %d while it presses, want 0", got)
	}
	if !f.press("k") {
		t.Error("k was not spent while Save's press was under way")
	}
	settle(t, func() bool { return f.saved.Load() > 0 })
	time.Sleep(400 * time.Millisecond)
	if f.saved.Load() != 1 || f.skipped.Load() != 0 {
		t.Errorf("Save pressed %d times and Skip %d, want 1 and 0", f.saved.Load(), f.skipped.Load())
	}
	if got := f.skip.liveMnemonic(); got != 1 {
		t.Errorf("after the press, Skip's letter is at %d, want 1", got)
	}
}

// A window nested inside the focused container -- an MDI child, say -- answers
// to its own focus: its buttons are not offered by the container around it.
func TestANestedWindowsButtonsAreItsOwn(t *testing.T) {
	f := newLetterForm(t)
	opened := atomic.Int32{}
	open := NewButton("&Open")
	open.SetOnClick(func() { opened.Add(1) })
	inner := window.NewWindow("inner")
	inner.SetContent(open)
	f.group.AddChild(inner)
	f.focus(f.group)
	if open.liveMnemonic() >= 0 {
		t.Error("the nested window's button offers its letter to the group around it")
	}
	if f.press("o") {
		t.Error("o was spent by the group, though only the nested window's button offers it")
	}
	time.Sleep(400 * time.Millisecond)
	if opened.Load() != 0 {
		t.Error("o pressed the nested window's button")
	}
}

// The window is the ancestor of everything in it, so it never counts: were it
// ever marked focused, every button it holds would light up at once.
func TestTheWindowItselfOffersNoLetters(t *testing.T) {
	f := newLetterForm(t)
	f.win.SetFocus()
	if !f.win.HasFocus() {
		t.Skip("the window would not take the focus; nothing to check")
	}
	if f.save.liveMnemonic() >= 0 {
		t.Error("a window marked focused offers its buttons by letter")
	}
}

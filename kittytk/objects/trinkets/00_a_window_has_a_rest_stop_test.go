package trinkets

// A window's focus ring runs title bar, REST, then its trinkets, and wraps back
// to the rest: a stop where the focus is on nothing in particular. A root that
// holds other trinkets is the rest itself, so a panel's buttons answer to their
// letters there; a lone field or list gets an empty rest instead. A root that
// already takes the focus first -- a message box's area -- serves as it is,
// and no window opens at rest.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

func tab(w *window.Window)      { w.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) }
func shiftTab(w *window.Window) { w.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"}) }

// where names the stop a window's focus is at.
func where(w *window.Window, names map[core.Trinket]string) string {
	if w.HasTitleFocus() {
		return "title"
	}
	fm := w.FocusManager()
	if fm.AtRest() {
		return "rest"
	}
	if f := fm.FocusedTrinket(); f != nil {
		if n, ok := names[f]; ok {
			return n
		}
		return "?"
	}
	return "none"
}

func TestAPanelIsItsWindowsRest(t *testing.T) {
	panel := NewPanel()
	save, skip := NewButton("&Save"), NewButton("S&kip")
	panel.AddChild(save)
	panel.AddChild(skip)
	w := window.NewWindow("form")
	w.SetContent(panel)
	names := map[core.Trinket]string{save: "Save", skip: "Skip", panel: "panel"}

	if got := where(w, names); got != "Save" {
		t.Fatalf("the window opens at %s, want Save", got)
	}
	steps := []struct {
		move func(*window.Window)
		want string
	}{
		{shiftTab, "rest"}, {shiftTab, "title"}, {tab, "rest"},
		{tab, "Save"}, {tab, "Skip"}, {tab, "rest"},
	}
	for i, s := range steps {
		s.move(w)
		if got := where(w, names); got != s.want {
			t.Fatalf("step %d: at %s, want %s", i, got, s.want)
		}
	}
	if w.FocusManager().FocusedTrinket() != panel {
		t.Error("the rest is not the panel holding the focus")
	}
	if save.liveMnemonic() != 0 || skip.liveMnemonic() != 1 {
		t.Error("at rest the panel does not offer its buttons by letter")
	}
}

func TestALoneFieldHasAnEmptyRest(t *testing.T) {
	field := NewTextInput()
	w := window.NewWindow("find")
	w.SetContent(field)
	names := map[core.Trinket]string{field: "field"}

	if got := where(w, names); got != "field" {
		t.Fatalf("the window opens at %s, want the field", got)
	}
	shiftTab(w)
	if got := where(w, names); got != "rest" || w.FocusManager().FocusedTrinket() != nil {
		t.Fatalf("Shift-Tab off the field lands at %s, want an empty rest", got)
	}
	w.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	if field.Text() != "" {
		t.Errorf("a typed at rest reached the field: %q", field.Text())
	}
	steps := []struct {
		move func(*window.Window)
		want string
	}{
		{tab, "field"}, {tab, "rest"}, {shiftTab, "title"}, {tab, "rest"}, {shiftTab, "title"},
	}
	for i, s := range steps {
		s.move(w)
		if got := where(w, names); got != s.want {
			t.Fatalf("step %d: at %s, want %s", i, got, s.want)
		}
	}
}

// A root that takes the focus by itself is already the rest: the ring gains no
// second stop in front of it.
func TestAMessageAreaIsAlreadyTheRest(t *testing.T) {
	mb := exitQuestion(t)
	fm := mb.FocusManager()
	fm.FocusFirstNonFurtive()
	if fm.FocusedTrinket() != core.Trinket(mb.content) || !fm.AtRest() {
		t.Fatal("the message box does not open with its area, at rest")
	}
	if chain := fm.FocusChain(); len(chain) != 1+len(mb.content.buttonTrinkets) {
		t.Errorf("the ring has %d stops, want the area and its %d buttons",
			len(chain), len(mb.content.buttonTrinkets))
	}
}

// A window left at an empty rest is still there when it is activated again:
// activation focuses a first trinket only where nothing is focused by accident.
func TestARestSurvivesActivation(t *testing.T) {
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	wm := d.WindowManager()
	field := NewTextInput()
	a := window.NewWindow("a")
	a.SetContent(field)
	b := window.NewWindow("b")
	b.SetContent(NewTextInput())
	wm.AddWindow(a)
	wm.AddWindow(b)
	wm.ActivateWindow(a)
	shiftTab(a)
	if !a.FocusManager().AtRest() {
		t.Fatal("setup: a is not at rest")
	}
	wm.ActivateWindow(b)
	wm.ActivateWindow(a)
	if !a.FocusManager().AtRest() {
		t.Errorf("reactivating a moved its focus to %v", a.FocusManager().FocusedTrinket())
	}
}

// A window holding nothing that takes the focus has no ring, and so no rest.
func TestALabelAloneHasNoRest(t *testing.T) {
	w := window.NewWindow("note")
	w.SetContent(NewLabel("Nothing to do here."))
	w.SetTitleFocus(window.TitleFocusTitle)
	tab(w) // off the title bar, to the rest were there one
	if fm := w.FocusManager(); fm.AtRest() || fm.FocusedTrinket() != nil {
		t.Errorf("a label-only window came to %v, at rest %v", fm.FocusedTrinket(), fm.AtRest())
	}
}

// The ring is the focus manager's, so walking it without a window's help
// passes through the rest as well.
func TestTheManagerWalksThroughTheRest(t *testing.T) {
	field := NewTextInput()
	w := window.NewWindow("find")
	w.SetContent(field)
	fm := w.FocusManager()
	fm.FocusPrior()
	if !fm.AtRest() {
		t.Fatalf("FocusPrior off the only field came to %v, want the rest", fm.FocusedTrinket())
	}
	fm.FocusPrior()
	if fm.FocusedTrinket() != core.Trinket(field) {
		t.Errorf("FocusPrior off the rest came to %v, want the field", fm.FocusedTrinket())
	}
}

// FocusFirst is the first TRINKET: activation uses it where nothing is
// focused, and the rest is never where it lands.
func TestFocusFirstPassesTheRest(t *testing.T) {
	panel := NewPanel()
	save := NewButton("&Save")
	panel.AddChild(save)
	w := window.NewWindow("form")
	w.SetContent(panel)
	fm := w.FocusManager()
	fm.ClearFocus()
	fm.FocusFirst()
	if fm.FocusedTrinket() != core.Trinket(save) {
		t.Errorf("FocusFirst came to %v, want Save", fm.FocusedTrinket())
	}
	fm.ClearFocus()
	fm.FocusFirstWithoutScroll()
	if fm.FocusedTrinket() != core.Trinket(save) {
		t.Errorf("FocusFirstWithoutScroll came to %v, want Save", fm.FocusedTrinket())
	}
}

// Tab off a window's own menu bar comes to the rest, as Tab off the title bar
// does: the bar sits between the title and the content.
func TestTabOffTheMenuBarComesToRest(t *testing.T) {
	panel := NewPanel()
	panel.AddChild(NewButton("&Save"))
	w := window.NewWindow("form")
	w.SetContent(panel)
	mb := NewMenuBar()
	w.SetWindowMenuBar(mb)
	if mb.onFocusOut == nil || !mb.onFocusOut(true) {
		t.Fatal("the menu bar was given no way off it forward")
	}
	if !w.FocusManager().AtRest() {
		t.Errorf("Tab off the menu bar came to %v, want the rest", w.FocusManager().FocusedTrinket())
	}
}

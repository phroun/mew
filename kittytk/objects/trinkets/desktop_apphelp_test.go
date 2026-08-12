package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

func helpDesktop(t *testing.T, withHelp bool) (*Desktop, *window.Window) {
	t.Helper()
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.SetBounds(core.UnitRect{Width: 800, Height: 600})
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("Doc")
	win.SetContent(NewTextInput())
	d.windowManager.AddWindow(win)

	menus := []*Menu{NewMenu("&File"), NewMenu("&Edit")}
	if withHelp {
		help := NewMenu("&Help").SetWellKnownID(MenuIDHelp)
		help.AddItem(NewMenuItem("Contents"))
		help.AddItem(NewMenuItem("About"))
		menus = append(menus, help)
	}
	d.AddApplication(&mockApp{name: "Demo", main: win, windows: []*window.Window{win}, menus: menus})
	d.windowManager.ActivateWindow(win)
	return d, win
}

// The help key is the menu key carried one step further: the bar takes the
// keyboard, the window gives it up, and Help is selected, dropped open, and
// stepped into once -- which is the menu key followed by Down.
func TestHelpKeyOpensHelpWithItsFirstItemHighlighted(t *testing.T) {
	d, _ := helpDesktop(t, true)

	if !d.windowManager.HandleKeyPress(core.KeyPressEvent{Key: "F1"}) {
		t.Fatal("F1 was not handled")
	}
	if !d.menuBar.HasFocus() {
		t.Error("the bar did not take the keyboard")
	}
	if d.windowManager.ActiveWindow() != nil {
		t.Error("the window kept the keyboard while the bar opened a menu")
	}
	open := d.menuBar.ActiveMenu()
	if open == nil {
		t.Fatal("F1 did not drop a menu open")
	}
	if open.WellKnownID() != MenuIDHelp {
		t.Errorf("F1 opened %q, want the Help menu", open.Title())
	}
	if open.currentIndex != 0 {
		t.Errorf("the first item is not highlighted (index %d)", open.currentIndex)
	}
}

// Help is found by its ROLE, not its title, so a localised bar still finds it.
func TestHelpKeyFindsHelpByRoleNotTitle(t *testing.T) {
	d, _ := helpDesktop(t, false)
	hilfe := NewMenu("&Hilfe").SetWellKnownID(MenuIDHelp)
	hilfe.AddItem(NewMenuItem("Über"))
	d.menuBar.AddMenu(hilfe)

	d.windowManager.HandleKeyPress(core.KeyPressEvent{Key: "F1"})

	if open := d.menuBar.ActiveMenu(); open != hilfe {
		t.Errorf("F1 opened %v, want the menu tagged as Help", open)
	}
}

// With no Help menu the key does exactly what the menu key does -- it is never
// dead, and it never opens something that is not Help.
func TestHelpKeyWithoutAHelpMenuIsJustTheMenuKey(t *testing.T) {
	d, _ := helpDesktop(t, false)

	if !d.windowManager.HandleKeyPress(core.KeyPressEvent{Key: "F1"}) {
		t.Fatal("F1 was not handled")
	}
	if !d.menuBar.HasFocus() {
		t.Error("the bar did not take the keyboard")
	}
	if d.windowManager.ActiveWindow() != nil {
		t.Error("the window kept the keyboard")
	}
	if open := d.menuBar.ActiveMenu(); open != nil {
		t.Errorf("with no Help menu, F1 dropped %q open", open.Title())
	}
}

// A window carrying its OWN bar answers the help key the same way.
func TestHelpKeyOnAWindowsOwnMenuBar(t *testing.T) {
	win := window.NewWindow("Solo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	help := NewMenu("&Help").SetWellKnownID(MenuIDHelp)
	help.AddItem(NewMenuItem("About"))
	mb.AddMenu(help)
	win.SetWindowMenuBar(mb)
	win.SetContent(NewTextInput())

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "F1"}) {
		t.Fatal("F1 was not handled")
	}
	if mb.ActiveMenu() != help {
		t.Errorf("F1 opened %v, want Help", mb.ActiveMenu())
	}
	if help.currentIndex != 0 {
		t.Errorf("the first item is not highlighted (index %d)", help.currentIndex)
	}
}

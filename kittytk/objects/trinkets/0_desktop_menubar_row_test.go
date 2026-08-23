package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// soleAppDesktop is the chrome-free single-app screen a TUI host runs: one
// full-screen app, the desktop's own chrome suppressed, the menu bar's row
// reclaimed by the app.
func soleAppDesktop(t *testing.T) (*Desktop, *window.Window) {
	t.Helper()
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.SetSoleAppChromeSuppression(true)
	d.SetHideMenuBarForSoleApp(true)
	d.SetBounds(core.UnitRect{Width: 800, Height: 600})
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	win := window.NewWindow("mew")
	win.SetContent(NewTextInput())
	d.windowManager.AddWindow(win)
	d.AddApplication(&mockApp{
		name: "mew", main: win, windows: []*window.Window{win},
		menus: []*Menu{NewMenu("&File"), NewMenu("&Help")},
	})
	d.windowManager.MaximizeWindow(win)
	return d, win
}

// A lone full-screen app owns every row, the bar's included -- so a focused
// bar has nowhere to draw. It borrows the top row for as long as it holds the
// keyboard.
func TestFocusedMenuBarBorrowsTheTopRow(t *testing.T) {
	d, win := soleAppDesktop(t)

	if d.menuBarShown() {
		t.Fatal("precondition: the bar's row should be reclaimed by the sole app")
	}
	before := win.Bounds()
	if before.Y != 0 {
		t.Fatalf("precondition: the app should start at the top, got Y=%d", before.Y)
	}

	d.menuBar.SetFocus()

	if !d.menuBarShown() {
		t.Error("the focused bar did not get a row to draw in")
	}
	lent := win.Bounds()
	row := d.EffectiveCellMetrics().CellHeight
	if lent.Y != before.Y+row {
		t.Errorf("the app did not scoot down a row: Y %d -> %d (row=%d)", before.Y, lent.Y, row)
	}
	if lent.Height != before.Height-row {
		t.Errorf("the app did not give up a row: height %d -> %d", before.Height, lent.Height)
	}
	if lent.Y+lent.Height != before.Y+before.Height {
		t.Error("the app's bottom edge moved; only the top row was borrowed")
	}
}

// ...and gives it back exactly, not to a recomputed guess.
func TestUnfocusedMenuBarGivesTheRowBack(t *testing.T) {
	d, win := soleAppDesktop(t)
	before := win.Bounds()

	d.menuBar.SetFocus()
	d.menuBar.HandleFocusOut()

	if d.menuBarShown() {
		t.Error("the bar kept a row it no longer holds the keyboard for")
	}
	if got := win.Bounds(); got != before {
		t.Errorf("the app came back as %v, want exactly %v", got, before)
	}
}

// Repeating it does not accumulate: the row is borrowed once and returned
// once, however many times focus moves.
func TestBorrowedRowSurvivesRepeatedFocus(t *testing.T) {
	d, win := soleAppDesktop(t)
	before := win.Bounds()

	for i := 0; i < 3; i++ {
		d.menuBar.SetFocus()
		d.menuBar.HandleFocusOut()
	}
	if got := win.Bounds(); got != before {
		t.Errorf("after three rounds the app is %v, want %v", got, before)
	}

	d.menuBar.SetFocus()
	d.menuBar.HandleFocusIn() // a second focus-in must not borrow twice
	lent := win.Bounds()
	row := d.EffectiveCellMetrics().CellHeight
	if lent.Height != before.Height-row {
		t.Errorf("borrowed twice: height %d, want %d", lent.Height, before.Height-row)
	}
}

// Where the bar already has a row of its own -- an ordinary desktop, or one
// whose host never asked for the chrome-free single-app screen -- nothing is
// borrowed and no window moves.
func TestOrdinaryDesktopLendsNothing(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.SetBounds(core.UnitRect{Width: 800, Height: 600})
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	win := window.NewWindow("Doc")
	d.windowManager.AddWindow(win)
	d.AddApplication(&mockApp{name: "Demo", windows: []*window.Window{win},
		menus: []*Menu{NewMenu("&File")}})
	before := win.Bounds()

	d.menuBar.SetFocus()

	if !d.menuBarShown() {
		t.Error("an ordinary desktop's bar should already have its row")
	}
	if d.menuBarRowLent {
		t.Error("a row was borrowed where the bar already had one")
	}
	if got := win.Bounds(); got != before {
		t.Errorf("a window moved for nothing: %v -> %v", before, got)
	}
}

// show_desktop is the app declaring itself MULTI-WINDOW, which brings the
// whole chrome back and stays until the app says otherwise. The borrowed row
// is a different thing -- one row, for as long as the bar holds the keyboard --
// and the two must not be taken for each other.
func TestMultiWindowAppNeedsNoLoan(t *testing.T) {
	d, win := soleAppDesktop(t)
	// What show_desktop does: the sole app is no longer single-window, so the
	// desktop's chrome is no longer suppressed.
	d.activeApp.(*mockApp).multiWindow = true
	d.layoutChildren()

	if !d.menuBarShown() {
		t.Fatal("a multi-window app should have the chrome back on its own")
	}
	before := win.Bounds()

	d.menuBar.SetFocus()

	if d.menuBarRowLent {
		t.Error("a row was borrowed although show_desktop had already given one")
	}
	if got := win.Bounds(); got != before {
		t.Errorf("the window moved: %v -> %v", before, got)
	}
}

// ...and if it happens WHILE the row is out on loan, giving the row back
// settles the windows against the client area as it is then, not as it was.
func TestShowDesktopDuringTheLoanIsHonoured(t *testing.T) {
	d, win := soleAppDesktop(t)
	d.menuBar.SetFocus()
	if !d.menuBarRowLent {
		t.Fatal("precondition: the row should be on loan")
	}

	d.activeApp.(*mockApp).multiWindow = true
	d.menuBar.HandleFocusOut()

	client := d.ClientArea()
	got := win.Bounds()
	if got.Y < client.Y || got.Y+got.Height > client.Y+client.Height {
		t.Errorf("the window came back as %v, outside the client area %v", got, client)
	}
	if d.menuBarRowLent {
		t.Error("the loan was not given back")
	}
}

// Clicking off an open dropdown, back into the app behind it, gives the row
// back. The click closes the menu by a route that does NOT unfocus the bar --
// so the bar read as focused while the window had the keyboard, and the row
// stayed out with it. The bar is on screen over an app that has the keyboard
// back, which is exactly the thing the loan is supposed to avoid.
func TestClickingBackIntoTheAppGivesTheRowBack(t *testing.T) {
	for _, c := range []struct {
		name string
		open func(d *Desktop)
	}{
		{"dropdown", func(d *Desktop) { d.menuBar.OpenMenu(1) }},
		{"submenu", func(d *Desktop) {
			parent := d.menuBar.menus[1]
			child := NewMenu("More")
			child.AddItem(NewMenuItem("Deep"))
			item := NewMenuItem("Recent")
			item.SubMenu = child
			parent.AddItem(item)
			d.menuBar.OpenMenu(1)
			parent.openSubMenu(item)
		}},
	} {
		d, win := soleAppDesktop(t)
		before := win.Bounds()

		d.toggleMenuBarFromKey() // the menu key: bar takes the keyboard
		c.open(d)
		if !d.menuBarRowLent {
			t.Fatalf("%s: precondition, the row should be on loan", c.name)
		}

		// A click in the app behind the menu.
		d.dispatchEvent(core.MousePressEvent{X: 100, Y: 300, Button: core.LeftButton})

		if d.menuBarRowLent {
			t.Errorf("%s: the row stayed out after the click", c.name)
		}
		if d.menuBarShown() {
			t.Errorf("%s: the bar is still on screen", c.name)
		}
		if got := win.Bounds(); got != before {
			t.Errorf("%s: the app came back as %v, want %v", c.name, got, before)
		}
	}
}

// The bar KEEPS the row while it genuinely holds the keyboard: closing a
// dropdown with the bar still focused and no window active is not letting go.
func TestClosingADropdownKeepsTheRowWhileTheBarHoldsFocus(t *testing.T) {
	d, _ := soleAppDesktop(t)
	d.toggleMenuBarFromKey()
	d.menuBar.OpenMenu(1)
	d.menuBar.CloseMenu()

	d.syncMenuBarRow()

	if !d.menuBarRowLent {
		t.Error("the row was taken back while the bar still had the keyboard")
	}
	if !d.menuBarShown() {
		t.Error("the focused bar lost its row")
	}
}

// A torn window that has yielded OS focus to the desktop stays QUASI-active --
// still lit -- while the menu bar holds the keyboard. That is a legitimate
// pairing, so the row must not be taken back for it. Quasi-active is a paint
// state and nothing more: it leaves IsActive alone and never reaches the
// manager, which is what deactivating cleared.
func TestQuasiActiveWindowDoesNotEndTheLoan(t *testing.T) {
	d, win := soleAppDesktop(t)
	d.toggleMenuBarFromKey()
	if !d.menuBarRowLent {
		t.Fatal("precondition: the row should be on loan")
	}
	win.SetQuasiActive(true)
	if !win.IsQuasiActive() {
		t.Fatal("precondition: the window should be quasi-active")
	}
	if win.IsActive() {
		t.Error("quasi-active must not make a window read as active")
	}

	d.syncMenuBarRow()

	if !d.menuBarRowLent {
		t.Error("a quasi-active window took the row from a bar that holds the keyboard")
	}
}

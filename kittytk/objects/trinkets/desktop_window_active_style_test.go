package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/objects/window"
)

// A detached window lives on its own OS surface; the desktop must not mark
// it passive (which would force the single/heavy border) just because it is
// the manager's remembered previous window while no in-surface window is
// active.
func TestDetachedWindowNotPassive(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	w := window.NewWindow("Main")
	d.windowManager.AddWindow(w)
	d.windowManager.ActivateWindow(w)
	d.windowManager.DeactivateActiveWindow() // now previous; active == nil

	if !d.IsWindowPassive(w) {
		t.Fatal("in-surface remembered window should be passive while menu holds focus")
	}
	w.SetDetached(true)
	if d.IsWindowPassive(w) {
		t.Error("detached window must not be reported passive by the desktop")
	}
}

// quasiActivateExclusive lights exactly one top-level window; any other torn
// window still carrying a lit/heavy style is returned to inactive.
func TestQuasiActivateExclusive(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	a := window.NewWindow("A")
	b := window.NewWindow("B")
	// Simulate two torn windows both left quasi-active.
	a.SetQuasiActive(true)
	b.SetQuasiActive(true)
	d.windowManager.AddWindow(a)
	d.windowManager.AddWindow(b)

	d.quasiActivateExclusive(b)

	if a.IsQuasiActive() || a.IsActive() {
		t.Errorf("A should be inactive after B becomes exclusively quasi-active")
	}
	if !b.IsQuasiActive() {
		t.Errorf("B should be quasi-active")
	}
}

// The Window menu checkmarks the currently lit top-level window.
func TestWindowMenuChecksCurrentWindow(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	winMenu := NewMenu("&Window")
	a := window.NewWindow("Doc A")
	b := window.NewWindow("Doc B")
	a.SetActive(true) // the lit top-level window
	app := &mockApp{name: "Demo", windows: []*window.Window{a, b}}

	menu := d.buildDesktopWindowMenu(winMenu, app)
	var checked []string
	for _, it := range menu.Items() {
		if it.Checkable && it.Checked {
			checked = append(checked, it.Text)
		}
	}
	if len(checked) != 1 || checked[0] != "Doc A" {
		t.Errorf("checkmark = %v, want [Doc A]", checked)
	}
}

// A torn-out window that hosts an active MDI child paints thick/single (the
// focus lives in the child, which shows the double focused border), even
// though a plain focused torn window paints double.
func TestDetachedWindowPassiveWithActiveMDIChild(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	parent := window.NewWindow("Torn Parent")
	parent.SetDetached(true)
	parent.SetActive(true)

	// Plain detached window with no MDI child: not passive -> double.
	if d.IsWindowPassive(parent) {
		t.Fatal("plain detached window should not be passive")
	}

	// Give it an MDIPane with an active child.
	mdi := NewMDIPane()
	parent.SetContent(mdi)
	child := window.NewWindow("Child")
	mdi.AddWindow(child)
	mdi.ActivateWindow(child)

	if !d.IsWindowPassive(parent) {
		t.Error("torn parent hosting an active MDI child should be passive (thick)")
	}
}

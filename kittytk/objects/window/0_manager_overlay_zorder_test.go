package window

import "testing"

// indexOf returns the z-order index of w (higher = more to front), or -1.
func zIndexOf(ws []*Window, w *Window) int {
	for i, x := range ws {
		if x == w {
			return i
		}
	}
	return -1
}

// Selecting an owner brings its owned overlays (dialogs, tool palettes) to the
// front, above the owner, without focusing them.
func TestOwnerSelectionRaisesOverlays(t *testing.T) {
	m := NewWindowManager()
	base := NewWindow("base")
	other := NewWindow("other")
	dlg := NewWindow("dlg")
	dlg.SetType(WindowTypeDialog)
	m.AddWindow(base)
	m.AddWindow(dlg)
	m.AddWindow(other)
	dlg.SetOwner(base)

	m.ActivateWindow(other) // push base+dlg down
	m.ActivateWindow(base)  // selecting base pulls dlg above it

	ws := m.Windows()
	if ws[len(ws)-1] != dlg {
		t.Errorf("top window is not the dialog")
	}
	if zIndexOf(ws, dlg) < zIndexOf(ws, base) {
		t.Error("dialog must sit above its owner")
	}
	// The dialog is raised, not activated: base is the active window.
	if m.ActiveWindow() != base {
		t.Errorf("active window = %v, want base (overlays raise without focus)", m.ActiveWindow())
	}
}

// Focusing a tool palette brings its whole owner group forward, with the
// palette itself on the very top.
func TestToolPaletteSelectionRaisesGroup(t *testing.T) {
	m := NewWindowManager()
	base := NewWindow("base")
	tool := NewWindow("tool")
	tool.SetType(WindowTypeToolPalette)
	dlg := NewWindow("dlg")
	dlg.SetType(WindowTypeDialog)
	other := NewWindow("other")
	m.AddWindow(base)
	m.AddWindow(dlg)
	m.AddWindow(tool)
	m.AddWindow(other)
	tool.SetOwner(base)
	dlg.SetOwner(base)

	m.ActivateWindow(other) // group behind other
	m.ActivateWindow(tool)  // palette selected: group forward, palette top

	ws := m.Windows()
	if ws[len(ws)-1] != tool {
		t.Error("tool palette must be on the very top")
	}
	if zIndexOf(ws, base) < zIndexOf(ws, other) || zIndexOf(ws, dlg) < zIndexOf(ws, other) {
		t.Error("the whole owner group should be forward of 'other'")
	}
}

// Focusing a dialog brings only the dialog forward - it must not drag its
// owner (or the owner's other overlays) forward with it.
func TestDialogSelectionDoesNotRaiseOwner(t *testing.T) {
	m := NewWindowManager()
	base := NewWindow("base")
	dlg := NewWindow("dlg")
	dlg.SetType(WindowTypeDialog)
	other := NewWindow("other")
	m.AddWindow(base)
	m.AddWindow(dlg)
	m.AddWindow(other)
	dlg.SetOwner(base)

	m.ActivateWindow(other) // base+dlg behind other
	m.ActivateWindow(dlg)   // dialog forward alone

	ws := m.Windows()
	if ws[len(ws)-1] != dlg {
		t.Error("focused dialog should be on top")
	}
	if zIndexOf(ws, base) > zIndexOf(ws, other) {
		t.Error("a focused dialog must not raise its owner above other windows")
	}
}

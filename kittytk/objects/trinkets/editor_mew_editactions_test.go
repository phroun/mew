//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The mew-backed Editor satisfies the desktop's Edit-menu standard
// (editActor) with mew semantics, OVERRIDING the embedded PurfecTerm's
// terminal semantics: Cut is enabled (a document block can be cut, unlike
// terminal output), and HasSelection defers to mew (always true here, so
// the desktop never second-guesses with its own "nothing selected" notice —
// mew reports "No block marked" itself).
func TestMewEditorEditActions(t *testing.T) {
	type editActions interface {
		Cut()
		Copy()
		Paste()
		SelectAll()
	}
	var _ editActions = (*Editor)(nil)

	e := NewEditor()
	defer e.Close()

	if !e.CutEnabled() {
		t.Error("mew Editor must report Cut enabled (PurfecTerm's false overridden)")
	}
	if !e.HasSelection() {
		t.Error("mew Editor must defer selection state to mew (always true)")
	}

	// A read-only focused buffer (mirrored from mew via WithEditState)
	// greys Cut out; back to writable re-enables it.
	e.readOnlyFocused.Store(true)
	if e.CutEnabled() {
		t.Error("Cut must grey out while the focused buffer is read-only")
	}
	e.readOnlyFocused.Store(false)
	if !e.CutEnabled() {
		t.Error("Cut must re-enable when the buffer is writable again")
	}

	// With no running session (unbound port) every action is a safe no-op.
	e.Cut()
	e.Copy()
	e.Paste()
	e.SelectAll()
}

// mewMenuPopupController captures RegisterPopup requests, standing in for
// the window manager on ANY backend (text or graphical) — the popup overlay
// machinery is backend-neutral, which is what makes the mew context menu
// work on the TUI desktop too.
type mewMenuPopupController struct {
	registered []*core.PopupRequest
}

func (r *mewMenuPopupController) RegisterPopup(req *core.PopupRequest) {
	r.registered = append(r.registered, req)
}
func (r *mewMenuPopupController) UnregisterPopup(string) {}
func (r *mewMenuPopupController) MapToScreen(_ core.Trinket, p core.UnitPoint) core.UnitPoint {
	return p
}
func (r *mewMenuPopupController) ScreenBounds() core.UnitRect {
	return core.UnitRect{Width: 10000, Height: 10000}
}

// The context menu registers through the backend-neutral popup controller,
// anchored by cellToLocal under TEXT cell metrics — i.e. it works on the
// TUI desktop, where classic PurfecTerm deliberately pops no menu of its
// own. (No graphical measurer is installed in this test, so cellDims
// resolves to the text-mode cell metrics.)
func TestMewEditorContextMenuOnTextBackend(t *testing.T) {
	e := NewEditor()
	defer e.Close()

	pc := &mewMenuPopupController{}
	e.SetPopupController(pc)

	e.showMewContextMenu(5, 3)
	if len(pc.registered) != 1 {
		t.Fatalf("expected one popup registration, got %d", len(pc.registered))
	}
	req := pc.registered[0]
	if req.Paint == nil || req.HandleMousePress == nil {
		t.Fatal("popup must carry paint and press handlers")
	}
	// cellToLocal under text metrics: cell (5,3) -> units ((5-1)*cw, (3-1)*ch).
	cw, ch := e.cellDims()
	if req.Bounds.X != core.Unit(4)*cw || req.Bounds.Y != core.Unit(2)*ch {
		t.Fatalf("menu anchored at (%d,%d), want (%d,%d) for cell (5,3) with cell %dx%d",
			req.Bounds.X, req.Bounds.Y, core.Unit(4)*cw, core.Unit(2)*ch, cw, ch)
	}
}

// The right-click menu mirrors the TextInput control's menu: the same
// items in the same order, each the matching Edit-menu action.
func TestMewEditorContextMenuItems(t *testing.T) {
	e := NewEditor()
	defer e.Close()

	items := e.mewContextMenuItems()
	want := []struct {
		label     string
		separator bool
	}{
		{"Cut", false},
		{"Copy", false},
		{"Paste", false},
		{"", true},
		{"Select All", false},
	}
	if len(items) != len(want) {
		t.Fatalf("menu has %d entries, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].separator != w.separator || (!w.separator && items[i].label != w.label) {
			t.Errorf("item %d = {%q sep=%v}, want {%q sep=%v}",
				i, items[i].label, items[i].separator, w.label, w.separator)
		}
		if !w.separator && items[i].action == nil {
			t.Errorf("item %d (%q) has no action", i, w.label)
		}
	}
}

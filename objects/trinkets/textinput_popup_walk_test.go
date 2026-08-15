package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// stubPopupController is a minimal core.PopupController for tests.
type stubPopupController struct{}

func (stubPopupController) RegisterPopup(*core.PopupRequest) {}
func (stubPopupController) UnregisterPopup(string)           {}
func (stubPopupController) MapToScreen(core.Trinket, core.UnitPoint) core.UnitPoint {
	return core.UnitPoint{}
}
func (stubPopupController) ScreenBounds() core.UnitRect { return core.UnitRect{} }

// A TextInput must resolve its popup controller by walking up the parent chain,
// not just its own (often unset) field - otherwise the right-click context menu
// and the clipboard bridge silently no-op inside an MDI child window, whose
// content is never stamped with a controller directly (only an ancestor is).
func TestTextInputFindsPopupControllerFromAncestor(t *testing.T) {
	pc := stubPopupController{}

	ancestor := NewPanel()
	ancestor.SetPopupController(pc) // e.g. the MDI pane, stamped by the manager
	mid := NewPanel()
	mid.SetParent(ancestor)

	ti := NewTextInput()
	ti.SetParent(mid)

	// The input's own field is unset; the walk must find the ancestor's.
	if ti.PopupController() != nil {
		t.Fatal("precondition: input should have no directly-set controller")
	}
	if got := ti.findPopupController(); got != core.PopupController(pc) {
		t.Errorf("findPopupController did not resolve the ancestor's controller (got %v)", got)
	}
}

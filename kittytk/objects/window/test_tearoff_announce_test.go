package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// a11yParent is a minimal Container that also provides an
// AccessibilityManager, so a hosted window's announcements are captured.
type a11yParent struct {
	core.TrinketBase
	am *core.AccessibilityManager
}

func (p *a11yParent) Children() []core.Trinket          { return nil }
func (p *a11yParent) AddChild(core.Trinket)             {}
func (p *a11yParent) RemoveChild(core.Trinket)          {}
func (p *a11yParent) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (p *a11yParent) Layout()                          {}
func (p *a11yParent) LayoutManager() core.LayoutManager { return nil }
func (p *a11yParent) SetLayoutManager(core.LayoutManager) {}
func (p *a11yParent) AccessibilityManager() *core.AccessibilityManager { return p.am }

// The tear handle announces its current action when keyboard-focused:
// "tear-away button" while docked, "dock torn window button" while torn.
func TestTearHandleAnnouncesAction(t *testing.T) {
	var spoken []string
	am := core.NewAccessibilityManager()
	am.OnAnnounce = func(a core.AccessibilityAnnouncement) { spoken = append(spoken, a.Message) }

	parent := &a11yParent{am: am}
	parent.TrinketBase = *core.NewTrinketBase()

	w := NewWindow("w")
	w.SetTearable(true)
	w.SetParent(parent)

	// Docked: tearing off.
	w.SetTitleFocus(TitleFocusTear)
	if len(spoken) == 0 || spoken[len(spoken)-1] != "tear-away button" {
		t.Errorf("docked tear focus announced %v, want last = tear-away button", spoken)
	}

	// Torn: re-docking. Reset focus first so the change re-announces.
	w.SetTitleFocus(TitleFocusNone)
	w.SetDetached(true)
	w.SetTitleFocus(TitleFocusTear)
	if spoken[len(spoken)-1] != "dock torn window button" {
		t.Errorf("torn tear focus announced %q, want dock torn window button", spoken[len(spoken)-1])
	}
}

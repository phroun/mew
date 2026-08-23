package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A child window that declines to close cancels its parent's close. Its own
// dialog is the one asking, and answering "don't close" there cannot mean the
// window behind it goes anyway.
func TestChildRefusalCancelsParentClose(t *testing.T) {
	parent := NewWindow("Document")
	child := NewWindow("Unsaved changes")
	child.SetParentWindow(parent)
	child.SetOnClose(func() bool { return false })

	if parent.Close() {
		t.Error("Close reported success although a child refused")
	}
	if !parent.IsVisible() {
		t.Error("the parent closed over a child that refused")
	}
	if !child.IsVisible() {
		t.Error("the child closed despite refusing")
	}
}

// The refusal stops the sweep where it stands: children that already agreed
// stay closed, matching how a refused application quit behaves.
func TestChildRefusalStopsAtThatChild(t *testing.T) {
	parent := NewWindow("Document")
	agreeable := NewWindow("Palette")
	refuser := NewWindow("Unsaved changes")
	untouched := NewWindow("Inspector")
	agreeable.SetParentWindow(parent)
	refuser.SetParentWindow(parent)
	untouched.SetParentWindow(parent)
	refuser.SetOnClose(func() bool { return false })

	if parent.Close() {
		t.Error("Close reported success although a child refused")
	}
	if agreeable.IsVisible() {
		t.Error("the child that agreed should have stayed closed")
	}
	if !refuser.IsVisible() || !untouched.IsVisible() {
		t.Error("the refusal should stop the sweep where it stood")
	}
	if !parent.IsVisible() {
		t.Error("the parent closed over a child that refused")
	}
}

// With every child willing, the whole tree goes and Close reports success.
func TestWillingChildrenAllClose(t *testing.T) {
	parent := NewWindow("Document")
	child := NewWindow("Palette")
	grandchild := NewWindow("Swatch")
	child.SetParentWindow(parent)
	grandchild.SetParentWindow(child)

	if !parent.Close() {
		t.Fatal("Close reported failure with no refusal anywhere")
	}
	for _, w := range []*Window{parent, child, grandchild} {
		if w.IsVisible() {
			t.Errorf("%s stayed open", w.Title())
		}
	}
}

// recordingSurfacer stands in for the desktop, noting what got raised and in
// what order. It is a core.Container so a Window can parent to it.
type recordingSurfacer struct {
	*core.TrinketBase
	raised []string
}

func (r *recordingSurfacer) Children() []core.Trinket            { return nil }
func (r *recordingSurfacer) AddChild(core.Trinket)               {}
func (r *recordingSurfacer) RemoveChild(core.Trinket)            {}
func (r *recordingSurfacer) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (r *recordingSurfacer) Layout()                             {}
func (r *recordingSurfacer) LayoutManager() core.LayoutManager   { return nil }
func (r *recordingSurfacer) SetLayoutManager(core.LayoutManager) {}

func (r *recordingSurfacer) SurfaceWindow(w *Window) {
	r.raised = append(r.raised, w.Title())
}

// A refusal deep in the tree surfaces every window between the close and the
// one that declined, OUTERMOST FIRST - so the window actually asking the user
// something is raised last and lands on top of the ones it belongs to.
func TestRefusalSurfacesTheChainInnermostLast(t *testing.T) {
	desk := &recordingSurfacer{TrinketBase: core.NewTrinketBase()}
	parent := NewWindow("Document")
	child := NewWindow("Palette")
	grandchild := NewWindow("Unsaved changes")
	parent.SetParent(desk)
	child.SetParentWindow(parent)
	grandchild.SetParentWindow(child)
	grandchild.SetOnClose(func() bool { return false })

	if parent.Close() {
		t.Fatal("Close reported success although a grandchild refused")
	}
	want := []string{"Document", "Palette", "Unsaved changes"}
	if len(desk.raised) != len(want) {
		t.Fatalf("raised %v, want %v", desk.raised, want)
	}
	for i := range want {
		if desk.raised[i] != want[i] {
			t.Fatalf("raised %v, want %v", desk.raised, want)
		}
	}
}

// Nothing is surfaced when nothing refused.
func TestSuccessfulCloseSurfacesNothing(t *testing.T) {
	desk := &recordingSurfacer{TrinketBase: core.NewTrinketBase()}
	parent := NewWindow("Document")
	child := NewWindow("Palette")
	parent.SetParent(desk)
	child.SetParentWindow(parent)

	if !parent.Close() {
		t.Fatal("Close reported failure with no refusal")
	}
	if len(desk.raised) != 0 {
		t.Errorf("a clean close raised %v", desk.raised)
	}
}

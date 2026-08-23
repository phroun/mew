package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// When focus lives in an MDI child's control, the Edit-menu focus inspection
// must drill through the MDI pane (which is all the enclosing window's focus
// manager can see) to the child's actually-focused trinket.
func TestResolveFocusedTrinketDrillsMDIChild(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	d := NewDesktop()
	d.SetBackend(&nullBackend{})

	mdi := NewMDIPane()
	input := NewTextInput()
	input.SetText("mdi text")

	child := window.NewWindow("child")
	child.SetContent(input)
	mdi.AddWindow(child)
	child.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
	child.Layout()
	mdi.ActivateWindow(child)
	input.SetFocus()

	// Sanity: the pane itself is not an edit target.
	if _, ok := core.Trinket(mdi).(interface{ Copy() }); ok {
		t.Fatal("precondition: the MDI pane must not itself be an edit target")
	}

	got := resolveFocusedTrinket(mdi)
	if got != core.Trinket(input) && got != input.Self() {
		t.Fatalf("resolveFocusedTrinket(mdi) = %v, want the child's input", got)
	}

	// The drilled-to trinket exposes the edit actions the Edit menu needs.
	ea, ok := got.(interface {
		Copy()
		SelectAll()
	})
	if !ok {
		t.Fatal("drilled trinket does not expose the edit actions")
	}
	ea.SelectAll()
	if input.SelectedText() != "mdi text" {
		t.Errorf("SelectAll via drilled trinket selected %q", input.SelectedText())
	}
}

// With no active child, the pane resolves to itself (no drill, no panic).
func TestResolveFocusedTrinketNoActiveChild(t *testing.T) {
	mdi := NewMDIPane()
	if got := resolveFocusedTrinket(mdi); got != core.Trinket(mdi) && got != mdi.Self() {
		t.Errorf("resolveFocusedTrinket(empty mdi) = %v, want the pane itself", got)
	}
	if resolveFocusedTrinket(nil) != nil {
		t.Error("resolveFocusedTrinket(nil) should be nil")
	}
}

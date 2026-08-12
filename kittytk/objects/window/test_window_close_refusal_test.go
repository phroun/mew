package window

import (
	"testing"
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

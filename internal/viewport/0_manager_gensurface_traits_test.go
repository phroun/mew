package viewport

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// vpShowing builds a bare viewport showing a buffer with the given filename and
// line numbers turned on, so the address-derived traits are what's under test.
func vpShowing(filename string) *Viewport {
	b := buffer.NewFromString("x\n")
	b.SetFilename(filename)
	w := &Viewport{Buffer: b}
	w.ViewState.ShowLineNumbers = true
	return w
}

// A mew: generated surface never shows the line-number gutter, even with the
// ShowLineNumbers view option on; an ordinary document still honors it.
func TestGenSurfaceHidesLineNumbers(t *testing.T) {
	if vpShowing("mew:/buffers").LineNumbersVisible() {
		t.Error("a mew: surface must not show line numbers even with ShowLineNumbers=true")
	}
	doc := vpShowing("/home/user/notes.txt")
	if !doc.LineNumbersVisible() {
		t.Error("a document with ShowLineNumbers=true should show its gutter")
	}
	doc.ViewState.ShowLineNumbers = false
	if doc.LineNumbersVisible() {
		t.Error("ShowLineNumbers=false should still hide the gutter")
	}
}

// A mew: generated surface reports the SurfaceClass so it themes separately from
// documents; an explicit class still wins, and a document reports its own class.
func TestGenSurfaceEffectiveClass(t *testing.T) {
	surf := vpShowing("mew:/viewports")
	if got := surf.EffectiveClass(); got != SurfaceClass {
		t.Errorf("surface EffectiveClass = %q, want %q", got, SurfaceClass)
	}
	surf.Class = "custom"
	if got := surf.EffectiveClass(); got != "custom" {
		t.Errorf("an explicit class should win over the surface default: got %q", got)
	}
	if got := vpShowing("/home/user/notes.txt").EffectiveClass(); got != "" {
		t.Errorf("document EffectiveClass = %q, want empty", got)
	}
}

// The traits are keyed on the buffer's ADDRESS, so a viewport that navigates
// in place from a surface to a document flips them back with no per-viewport
// state to reset — this is what keeps the surface look from sticking.
func TestSurfaceTraitsFollowBufferAddress(t *testing.T) {
	w := vpShowing("mew:/buffers")
	if w.LineNumbersVisible() || w.EffectiveClass() != SurfaceClass {
		t.Fatal("precondition: surface should hide the gutter and report the surface class")
	}
	doc := buffer.NewFromString("y\n")
	doc.SetFilename("/tmp/doc.txt")
	w.Buffer = doc
	if !w.LineNumbersVisible() {
		t.Error("the gutter should return once the viewport shows a document")
	}
	if w.EffectiveClass() != "" {
		t.Error("the class should revert to the document's own once the buffer changes")
	}
}

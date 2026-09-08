package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// markerWidthBar builds an overflowing menu bar on a cell surface whose first
// title is the one given, followed by enough menus to run past the end. A
// terminal lays every width in whole cells, which is where a title and the
// "..." that replaces it can come out exactly the same width.
func markerWidthBar(t *testing.T, first string) *MenuBar {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	d.SetBounds(core.UnitRect{Width: 60, Height: 24})
	bar := NewMenuBar()
	d.AddChild(bar)
	for _, title := range []string{first, "File", "Edit", "View", "Insert", "Format", "Tools", "Window", "Help"} {
		bar.AddMenu(NewMenu(title))
	}
	bar.SetBounds(core.UnitRect{Width: 60, Height: bar.menuMetrics().RowH})
	if !bar.menusNeedScrolling() {
		t.Fatal("precondition: the bar should overflow")
	}
	return bar
}

// pressScrollButton clicks [<] (step -1) or [>] (step +1).
func pressScrollButton(t *testing.T, bar *MenuBar, step int) {
	t.Helper()
	w := bar.scrollButtonWidth()
	x := bar.Bounds().Width - bar.dateTimeWidth() - w*2
	if step > 0 {
		x += w
	}
	if !bar.HandleMousePress(core.MousePressEvent{X: x + w/2, Y: 0, Button: core.LeftButton}) {
		t.Fatalf("press on the scroll button at x=%d was not consumed", x)
	}
}

// The first step to the right takes the leftmost title away and puts the "..."
// where it was, so what it frees is however much wider than the marker that
// title was. A one-glyph title -- a Ψ, a hamburger -- is its glyph and a pad on
// either side, three cells against the marker's three, so the step frees
// nothing and the bar comes back looking exactly as it did. It takes another.
func TestAScrollStepThatWouldShowNothingTakesAnother(t *testing.T) {
	bar := markerWidthBar(t, "Ψ")
	if got, want := bar.menuTitleWidth("Ψ"), bar.ellipsisWidth(); got != want {
		t.Fatalf("precondition: the title measures %d and its marker %d; they should tie", got, want)
	}

	pressScrollButton(t, bar, +1)
	if bar.scrollOffset != 2 {
		t.Errorf("the first step right landed at offset %d, want 2 -- offset 1 shows what offset 0 did",
			bar.scrollOffset)
	}

	// Past the first, every step gives its title up outright: the marker is
	// already there and is paid for once.
	pressScrollButton(t, bar, +1)
	if bar.scrollOffset != 3 {
		t.Errorf("the second step right landed at offset %d, want 3", bar.scrollOffset)
	}
}

// Coming back the other way, the first menu is shown along with the second:
// held behind a "..." of its own width it hides nothing, and the step that
// revealed it alone would have looked like no step at all.
func TestAScrollStepBackRevealsATitleTheMarkerCannotHide(t *testing.T) {
	bar := markerWidthBar(t, "Ψ")
	bar.scrollOffset = 3

	pressScrollButton(t, bar, -1)
	if bar.scrollOffset != 2 {
		t.Fatalf("a step back from offset 3 landed at %d, want 2", bar.scrollOffset)
	}
	pressScrollButton(t, bar, -1)
	if bar.scrollOffset != 0 {
		t.Errorf("a step back from offset 2 landed at offset %d, want 0 -- the first title costs no more room than the marker over it",
			bar.scrollOffset)
	}
}

// A title the marker really does hide steps one at a time, either way. The rule
// is a comparison of widths, not a special case for the leftmost menu.
func TestAWiderFirstTitleStepsOneAtATime(t *testing.T) {
	bar := markerWidthBar(t, "Preferences")
	if bar.menuTitleWidth("Preferences") <= bar.ellipsisWidth() {
		t.Fatal("precondition: this title should be wider than its marker")
	}

	pressScrollButton(t, bar, +1)
	if bar.scrollOffset != 1 {
		t.Errorf("the first step right landed at offset %d, want 1", bar.scrollOffset)
	}
	pressScrollButton(t, bar, -1)
	if bar.scrollOffset != 0 {
		t.Errorf("the step back landed at offset %d, want 0", bar.scrollOffset)
	}
}

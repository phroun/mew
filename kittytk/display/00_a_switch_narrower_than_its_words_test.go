package display

// The two switches above the list sit in rows of their own. A row with less
// room than the words in it want brings them down rather than laying them out
// past its end.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

func TestASwitchTooWideForTheWindowIsCutToIt(t *testing.T) {
	px, err := raster.NewScaled(800, 400, 1)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	store := newAuthStore(t.TempDir() + "/authorizations")
	v, win := buildConnections(shownDesktop(t), &fakeHost{}, store,
		newPairStore(t.TempDir()+"/nicks", ""), newPairStore(t.TempDir()+"/seen", ""))
	if v == nil || win == nil {
		t.Fatal("the window did not build")
	}

	// Narrower than the longer of the two captions.
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 440})
	win.Layout()
	win.Paint(core.NewPainter(px))

	row, ok := v.trusted.Parent().(*trinkets.Panel)
	if !ok {
		t.Fatalf("the switch hangs off a %T", v.trusted.Parent())
	}
	box := v.trusted.Bounds()
	if box.Width > row.Bounds().Width {
		t.Errorf("the switch is %d wide in a row %d wide", box.Width, row.Bounds().Width)
	}
	if box.X+box.Width > row.Bounds().Width {
		t.Errorf("the switch runs to %d, past the row's %d", box.X+box.Width, row.Bounds().Width)
	}

	// And with less room than its words need, it says so on hover.
	text, _, ok := v.trusted.TooltipAt(core.UnitPoint{X: 4, Y: 4})
	if !ok {
		t.Fatal("a switch too narrow for its caption offered nothing")
	}
	if !strings.Contains(text, "Previously Trusted") {
		t.Errorf("it offered %q", text)
	}
}

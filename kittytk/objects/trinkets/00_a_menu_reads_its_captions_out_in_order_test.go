package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A menu caption reaches a cell target the same way any other run does -- in
// the order the cells are stamped. The accelerator makes it the awkward case:
// one letter wears its own style, so the caption is drawn in pieces, and the
// pieces of a run that reads right to left are not the pieces of the text that
// made it.
func TestAMenuReadsItsCaptionsOutInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	// The caption drawn: the pieces in the order they were handed over.
	pieces := func(caption string, dir core.Direction) []string {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		m := NewMenu("File")
		m.SetDirection(dir)
		m.AddItem(NewMenuItem(caption))
		m.SetBounds(core.UnitRect{Width: 40 * 8, Height: 10 * 16})
		m.Show(0, 0) // a dropdown paints only while it is open
		m.Paint(core.NewPainter(ink))
		return ink.texts
	}

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	// A caption with no accelerator arrives as one run, turned over.
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		got := strings.Join(pieces(shalom, dir), "")
		if !strings.Contains(got, turned) {
			t.Errorf("%v: the menu drew %q, want it to contain the turned-over caption %q",
				dir, got, turned)
		}
	}

	// With an accelerator on the second letter, the caption is drawn in three
	// pieces -- and joined back up they are still the turned-over caption, in
	// the order the cells go down. Cut before preparing, they would read
	// backwards: each piece ordered on its own, and the first piece drawn
	// where the last one belongs.
	got := pieces("ש&לום", core.DirRTL)
	if joined := strings.Join(got, ""); !strings.Contains(joined, turned) {
		t.Errorf("with an accelerator the menu drew %q (%d pieces), want the pieces to spell "+
			"the turned-over caption %q in the order they were drawn",
			joined, len(got), turned)
	}

	// The accelerator's own piece is the letter it marks -- the SECOND letter
	// of the word, which in a turned-over run is the third cell.
	var accel string
	for _, s := range got {
		if len([]rune(s)) == 1 && []rune(s)[0] == 'ל' {
			accel = s
		}
	}
	if accel == "" {
		t.Errorf("the menu drew %q; no piece is the accelerator's own letter", got)
	}
}

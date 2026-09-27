package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// tabTextRecorder remembers every run the strip draws. It embeds the raster
// backend whole rather than the RenderBackend interface, so the optional
// interfaces the painter probes -- GraphicalMode among them -- still answer
// and the strip takes its pixel path.
type tabTextRecorder struct {
	*raster.Backend
	texts []string
}

func (r *tabTextRecorder) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.texts = append(r.texts, text)
	return r.Backend.DrawText(x, y, text, s, font)
}

// drawnPrefix reports how many leading characters of label the strip drew.
func (r *tabTextRecorder) drawnPrefix(label string) int {
	n := 0
	for _, s := range r.texts {
		if s != "" && s != "..." && strings.HasPrefix(label, s) && len(s) > n {
			n = len([]rune(s))
		}
	}
	return n
}

// Widening the strip never shows FEWER characters of a tab's label.
//
// A tab with more tabs after it must show an ellipsis, so its label is trimmed
// even at the width where the whole label would have fit. The trim took a
// whole CELL off the budget, but a label is measured in proportional glyphs:
// where its last letter was narrower than a cell, a cell's worth of budget
// took the letter before it down as well. Widening the strip by a pixel then
// crossed into the forced trim and turned "Termina..." into "Termin...",
// spending the width it had just gained on nothing.
func TestWideningAStripNeverShowsFewerCharacters(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const label = "Terminal"
	px, err := raster.New(900, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	best, bestW := 0, core.Unit(0)
	sawForcedTrim := false
	for w := core.Unit(120); w <= 420; w++ {
		rec := &tabTextRecorder{Backend: px}

		d := NewDesktop()
		d.SetBackend(px)
		d.SetBounds(core.UnitRect{Width: 900, Height: 200})
		win := window.NewWindow("w")
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{Width: 900, Height: 200})

		tw := NewTabTrinket()
		win.AddChild(tw)
		for _, name := range []string{"Progress", "Bottom Tabs", label, "Details", "MDI Demo"} {
			tw.AddTab(name, NewLabel(name))
		}
		tw.SetCurrentIndex(2) // the tab under test, with tabs after it
		tw.SetBounds(core.UnitRect{Width: w, Height: 96})
		tw.tabScrollOffset = 0
		if !core.FindSmoothPositioning(tw.Self()) {
			t.Fatal("precondition: this strip should measure proportionally")
		}

		px.Clear(style.DefaultStyle())
		tw.Paint(core.NewPainter(rec))

		n := rec.drawnPrefix(label)
		if n > 0 && n < len([]rune(label)) {
			sawForcedTrim = true
		}
		if n < best {
			t.Errorf("at width %d the strip drew %d characters of %q, having drawn %d at the narrower width %d",
				w, n, label, best, bestW)
		}
		if n > best {
			best, bestW = n, w
		}
	}
	if !sawForcedTrim {
		t.Fatal("precondition: no width trimmed the label at all, so nothing was under test")
	}
	if best != len([]rune(label)) {
		t.Fatalf("precondition: the label never came out whole across the sweep (best %d of %d)",
			best, len([]rune(label)))
	}
}

// A tab is trimmed to leave room for the strip's own ellipsis only when its
// LABEL and that ellipsis cannot both stand -- measured as the ellipsis
// actually measures, a proportional run of dots.
//
// The reserve used to be four or five whole CELLS, and it counted the
// separator running into the next tab as well: a separator into a tab the
// strip was never going to show. So a name with room to be whole was trimmed
// anyway, and the strip drew its ellipsis after it regardless -- "Neste" then
// dots then dots again, a letter of the name paying for a second ellipsis
// beside the first.
func TestATabIsNotTrimmedForAnEllipsisThatFitsBesideIt(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const label = "Nested"
	px, err := raster.New(900, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)
	rec := &tabTextRecorder{Backend: px}

	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 900, Height: 200})
	win := window.NewWindow("w")
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: 900, Height: 200})

	tw := NewTabTrinket()
	win.AddChild(tw)
	for _, name := range []string{"Alphabet", label, "Vertical Tabs", "Details", "MDI Demo", "Extra", "Final"} {
		tw.AddTab(name, NewLabel(name))
	}
	tw.SetCurrentIndex(2) // the tab AFTER the one under test is the selected one
	tw.SetBounds(core.UnitRect{Width: 184, Height: 96})
	tw.tabScrollOffset = 0
	if !core.FindSmoothPositioning(tw.Self()) {
		t.Fatal("precondition: this strip should measure its ellipsis proportionally")
	}

	px.Clear(style.DefaultStyle())
	tw.Paint(core.NewPainter(rec))

	if n := rec.drawnPrefix(label); n != len([]rune(label)) {
		t.Errorf("the strip drew %d characters of %q where the whole name and the strip's ellipsis both had room (runs: %v)",
			n, label, rec.texts)
	}

	// And one ellipsis stands at the end of the run, not two.
	trailing := 0
	for _, s := range rec.texts {
		if s == "..." {
			trailing++
		}
	}
	if trailing > 1 {
		t.Errorf("%d ellipses drawn where one says it: %v", trailing, rec.texts)
	}
}

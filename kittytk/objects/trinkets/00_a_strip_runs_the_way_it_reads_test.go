package trinkets

import (
	"fmt"
	"sort"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// markTape writes down every mark a strip makes and where it occupies, so one
// strip's ink can be compared against another's reflected.
type markTape struct {
	core.RenderBackend
	graphical bool
	marks     []string
	// reflect turns a recorded mark into what it would be in a mirror, so a
	// straight strip's tape can be compared against a turned-over one's.
	reflect core.Unit
}

func (r *markTape) GraphicalMode() bool { return r.graphical }

func (r *markTape) note(x, w core.Unit, what string, s style.CellStyle) {
	if r.reflect > 0 {
		x = r.reflect - x - w
		what = string(mirroredGlyph([]rune(what + " ")[0])) + what[len(string([]rune(what)[0])):]
	}
	r.marks = append(r.marks, fmt.Sprintf("%d+%d %s %v/%v", x, w, what, s.Fg, s.Bg))
}

func (r *markTape) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	r.note(x, 8, string(ch), s)
}
func (r *markTape) DrawText(x, y core.Unit, t string, s style.CellStyle, f *core.Font) core.Unit {
	w := r.RenderBackend.DrawText(x, y, t, s, f)
	r.note(x, w, "«"+t+"»", s)
	return w
}
func (r *markTape) FillRect(rect core.UnitRect, ch rune, s style.CellStyle) {
	r.note(rect.X, rect.Width, fmt.Sprintf("fill%d", rect.Height), s)
}

// A turned-over strip is the same strip seen in a mirror. Every mark stands
// its own width back from the far side, and the marks that point along the run
// -- the slashes that shape a tab, the scroll arrows and their brackets --
// become their partners.
//
// The captions here are DIGITS, which name no direction of their own, so every
// mark on the strip is the strip's. A word that reads the other way is the one
// exception to a plain reflection and has its own test below.
func TestATurnedOverStripIsTheStripReflected(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tape := func(dir core.Direction, pos TabPosition, n, sel, scroll, wCells int) []string {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		rec := &markTape{RenderBackend: px}
		if dir == core.DirRTL {
			rec.reflect = core.Unit(wCells * 8)
		}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		form.AddChild(tt)
		for i := 0; i < n; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetCurrentIndex(sel)
		tt.tabScrollOffset = scroll
		tt.SetBounds(core.UnitRect{Width: core.Unit(wCells * 8), Height: 10 * 16})
		tt.Paint(core.NewPainter(rec))
		out := append([]string(nil), rec.marks...)
		sort.Strings(out)
		return out
	}

	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		for _, c := range []struct{ n, sel, scroll, w int }{
			{1, 0, 0, 40}, {3, 0, 0, 40}, {3, 1, 0, 40}, {3, 2, 0, 20},
			{9, 4, 0, 30}, {9, 0, 2, 20}, {30, 15, 5, 40},
		} {
			straight := tape(core.DirLTR, pos, c.n, c.sel, c.scroll, c.w)
			turned := tape(core.DirRTL, pos, c.n, c.sel, c.scroll, c.w)
			if len(straight) != len(turned) {
				t.Errorf("%v n=%d sel=%d scroll=%d w=%d: %d marks straight, %d turned",
					pos, c.n, c.sel, c.scroll, c.w, len(straight), len(turned))
				continue
			}
			for i := range straight {
				if straight[i] != turned[i] {
					t.Errorf("%v n=%d sel=%d scroll=%d w=%d: mark %d is %q straight and %q reflected",
						pos, c.n, c.sel, c.scroll, c.w, i, straight[i], turned[i])
					break
				}
			}
		}
	}
}

// shapeTape writes down the silhouette: its arcs, the strokes that join them,
// and the edge line the strip carries between them. These go straight to the
// painter rather than onto the strip's tape, so they are recorded here.
type shapeTape struct {
	core.RenderBackend
	marks   []string
	reflect core.Unit // device pixels across the bar, when the strip is turned
}

func (r *shapeTape) GraphicalMode() bool { return true }

func (r *shapeTape) at(xPx, wPx int, what string) {
	if r.reflect > 0 {
		xPx = int(r.reflect) - xPx - wPx
	}
	r.marks = append(r.marks, fmt.Sprintf("%d+%d %s", xPx, wPx, what))
}

func (r *shapeTape) FillRectPx(x, y, w, h int, s style.CellStyle) {
	r.at(x, w, fmt.Sprintf("rect h=%d y=%d", h, y))
}

// The strokes that join the arcs are unit rects rather than pixel ones.
func (r *shapeTape) FillRect(rect core.UnitRect, ch rune, s style.CellStyle) {
	r.at(int(rect.X), int(rect.Width), fmt.Sprintf("rule h=%d y=%d", rect.Height, rect.Y))
}
func (r *shapeTape) DrawArcWedge(rect core.UnitRect, centerRight, centerBottom bool, strokeW core.Unit, offXPx, offYPx int, s style.CellStyle) {
	corner := "left"
	if centerRight != (r.reflect > 0) {
		corner = "right"
	}
	off := offXPx
	if r.reflect > 0 {
		off = -off
	}
	r.at(int(rect.X), int(rect.Width),
		fmt.Sprintf("arc %s bottom=%v off=%d", corner, centerBottom, off))
}

// The selected tab's silhouette turns over with the run it stands in: the same
// arcs, the same strokes, the same edge line, reflected. The captions are
// digits, which name no direction, so nothing here is tied. A tab with no
// trailing foot -- the last in the strip, or one cut short by its end -- turns
// over like any other.
func TestATurnedOverSilhouetteIsTheSilhouetteReflected(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tape := func(dir core.Direction, pos TabPosition, n, sel, scroll, wCells int) []string {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		rec := &shapeTape{RenderBackend: px}
		p := core.NewPainter(rec)
		if dir == core.DirRTL {
			rec.reflect = core.Unit(p.UnitSpanPxX(0, core.Unit(wCells*8)))
		}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		form.AddChild(tt)
		for i := 0; i < n; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetCurrentIndex(sel)
		tt.tabScrollOffset = scroll
		tt.SetBounds(core.UnitRect{Width: core.Unit(wCells * 8), Height: 10 * 16})
		tt.Paint(p)
		out := append([]string(nil), rec.marks...)
		sort.Strings(out)
		return out
	}

	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		for _, c := range []struct {
			n, sel, scroll, w int
			what              string
		}{
			{3, 1, 0, 40, "a tab with both feet"},
			{1, 0, 0, 40, "the only tab"},
			// These reach the branch where the selected tab has NO trailing
			// foot: the strip has run out of room and the tab carries the
			// overflow mark itself. Reflecting the two anchors rather than
			// each mark left the shape with its right edge behind its left,
			// and the whole silhouette collapsed to one line drawn straight
			// across the strip -- through the tab it was meant to outline.
			{2, 0, 0, 14, "a tab that runs out of room"},
			{3, 1, 0, 18, "the same, with a tab either side"},
			{3, 1, 0, 20, "the same again, a little wider"},
			{5, 1, 1, 14, "one in a scrolled strip"},
			{9, 1, 0, 20, "one in a long strip"},
			{3, 2, 1, 20, "the last tab of a scrolled strip"},
		} {
			straight := tape(core.DirLTR, pos, c.n, c.sel, c.scroll, c.w)
			turned := tape(core.DirRTL, pos, c.n, c.sel, c.scroll, c.w)
			if len(straight) != len(turned) {
				t.Errorf("%v, %s: %d silhouette marks straight, %d turned",
					pos, c.what, len(straight), len(turned))
				continue
			}
			for i := range straight {
				if straight[i] != turned[i] {
					t.Errorf("%v, %s: mark %d is %q straight and %q reflected",
						pos, c.what, i, straight[i], turned[i])
					break
				}
			}
		}
	}
}

// A word and the dots that stand for what was cut off it read together. In a
// strip turned over, an English label still runs left to right, so its dots
// belong on its RIGHT -- the end the word ends at, not the end the strip does.
// Reflecting them separately puts the dots in front of the word.
func TestTheDotsStayOnTheEndTheWordEndsAt(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// A label wide enough that the strip runs out of room and the tab has to
	// carry its own overflow mark.
	place := func(dir core.Direction, caption string) (word, dots core.Unit, ok bool) {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := &markTape{RenderBackend: px, graphical: true}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		form.AddChild(tt)
		tt.AddTab(caption, NewPanel())
		tt.AddTab("Second", NewPanel())
		tt.AddTab("Third", NewPanel())
		tt.SetCurrentIndex(0)
		tt.SetBounds(core.UnitRect{Width: 18 * 8, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))

		word, dots = -1, -1
		for _, m := range ink.marks {
			var x, w core.Unit
			var what string
			if _, err := fmt.Sscanf(m, "%d+%d %s", &x, &w, &what); err != nil {
				continue
			}
			switch {
			case what == "«...»":
				if dots < 0 {
					dots = x
				}
			case len(what) > 2 && what[:len("«")] == "«":
				// The label, whether it was trimmed or not.
				word = x
			}
		}
		return word, dots, word >= 0 && dots >= 0
	}

	const caption = "Default"
	straightWord, straightDots, ok := place(core.DirLTR, caption)
	if !ok {
		t.Fatalf("the straight strip drew word=%d dots=%d; the case needs both",
			straightWord, straightDots)
	}
	if straightDots < straightWord {
		t.Fatalf("even straight, the dots at %d come before the word at %d",
			straightDots, straightWord)
	}

	turnedWord, turnedDots, ok := place(core.DirRTL, caption)
	if !ok {
		t.Fatalf("the turned strip drew word=%d dots=%d; the case needs both",
			turnedWord, turnedDots)
	}
	if turnedDots < turnedWord {
		t.Errorf("turned over, the dots at %d come before the word at %d; an English "+
			"word keeps its dots on its own end", turnedDots, turnedWord)
	}
}

// A column of tabs does not turn over, but each LABEL in it still reads its
// own way: a Hebrew name sits against the right of its slot and an English one
// against the left, in a strip on either edge and in a form reading either
// way. What a label is doing is reading.
func TestASideTabsLabelSitsWhereItsScriptReadsFrom(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const english, hebrewName = "Alpha", "שלום"

	at := func(dir core.Direction, pos TabPosition, caption string) (x, slotLo, slotHi core.Unit) {
		px, err := raster.New(600, 400)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := newInk(t)
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		form.AddChild(tt)
		tt.AddTab(caption, NewPanel())
		tt.AddTab("Beta", NewPanel())
		tt.SetBounds(core.UnitRect{Width: 40 * 8, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))
		got, ok := ink.textAt(caption)
		if !ok {
			t.Fatalf("%v %v: the strip drew no %q", dir, pos, caption)
		}
		w := tt.calculateTabBarWidth()
		lo := core.Unit(0)
		if tt.tabEdge() == TabEdgeRight {
			lo = tt.Bounds().Width - w
		}
		return got, lo + 8, lo + w - 8
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		for _, pos := range []TabPosition{TabsSide, TabsSideOpposite} {
			eng, lo, _ := at(dir, pos, english)
			if eng != lo {
				t.Errorf("%v %v: the English label sits at %d, want the slot's left at %d",
					dir, pos, eng, lo)
			}
			heb, lo, hi := at(dir, pos, hebrewName)
			if heb <= lo {
				t.Errorf("%v %v: the Hebrew label sits at %d, hugging the slot's left at %d",
					dir, pos, heb, lo)
			}
			if heb >= hi {
				t.Errorf("%v %v: the Hebrew label at %d has run past the slot's right at %d",
					dir, pos, heb, hi)
			}
		}
	}
}

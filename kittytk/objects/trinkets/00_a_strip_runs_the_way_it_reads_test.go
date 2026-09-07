package trinkets

import (
	"fmt"
	"sort"
	"strings"
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

	// A strip too narrow for the first label, so the tab has to trim it and
	// carry its own overflow mark. Given room for the whole word the dots
	// would be the STRIP's, and those belong to the strip's run.
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
		tt.SetBounds(core.UnitRect{Width: 17 * 8, Height: 10 * 16})
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

// The mouse finds a tab where the painter put it. A turned-over strip draws
// its tabs reflected, so a press lands on the tab whose reflection covers it:
// sweeping the bar across gives the same sequence of tabs, in reverse.
func TestAPressFindsTheTabItWasDrawnOn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const wCells = 40
	// A fresh strip for every probe: pressing the overflow mark scrolls, which
	// would otherwise leave the next probe looking at a different strip. Each
	// one is PAINTED before it is pressed, because where the painter put things
	// is what the press reads.
	press := func(dir core.Direction, pos TabPosition, n, scroll int, x core.Unit) int {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		form.AddChild(tt)
		for i := 0; i < n; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.tabScrollOffset = scroll
		tt.SetBounds(core.UnitRect{Width: wCells * 8, Height: 10 * 16})
		tt.currentIndex = -1 // so anything the press selects is visible as one
		tt.Paint(core.NewPainter(px))
		tt.handleTabBarPress(x)
		return tt.currentIndex
	}
	sweep := func(dir core.Direction, pos TabPosition, n, scroll int) []int {
		out := make([]int, 0, wCells*8)
		for x := core.Unit(0); x < wCells*8; x++ {
			out = append(out, press(dir, pos, n, scroll, x))
		}
		return out
	}

	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		for _, c := range []struct{ n, scroll int }{{3, 0}, {5, 0}, {9, 0}, {9, 2}} {
			straight := sweep(core.DirLTR, pos, c.n, c.scroll)
			turned := sweep(core.DirRTL, pos, c.n, c.scroll)
			if len(straight) != len(turned) {
				t.Fatalf("%v n=%d: swept %d against %d", pos, c.n, len(straight), len(turned))
			}
			hits := 0
			for i := range straight {
				want := straight[len(straight)-1-i]
				if turned[i] != want {
					t.Errorf("%v n=%d scroll=%d: at x=%d the turned strip hits tab %d; "+
						"reflected, the straight one hits %d", pos, c.n, c.scroll, i, turned[i], want)
					break
				}
				if want >= 0 {
					hits++
				}
			}
			if hits == 0 {
				t.Errorf("%v n=%d scroll=%d: the sweep hit no tab at all", pos, c.n, c.scroll)
			}
		}
	}
}

// A strip with room to spare can put its slack at either end, or split it.
// Which end is "natural" is the run's own, so the whole arrangement turns over
// with the strip -- and a strip that has to scroll has no slack to place.
func TestASlackStripPutsItsTabsWhereItIsTold(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// Where the run begins, measured from the end the run starts at.
	runStart := func(dir core.Direction, a TabAlign, tabs int) (into, bar core.Unit) {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := newInk(t)
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		form.AddChild(tt)
		for i := 0; i < tabs; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetTabAlign(a)
		tt.SetBounds(core.UnitRect{Width: 60 * 8, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))
		x, ok := ink.textAt("0000")
		if !ok {
			t.Fatalf("%v %d: the strip drew no first tab", dir, a)
		}
		w := tt.Bounds().Width
		if core.ChromeMirrored(tt) {
			return w - x - tt.MeasureText("0000"), w
		}
		return x, w
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		natural, bar := runStart(dir, TabsAlignNatural, 3)
		center, _ := runStart(dir, TabsAlignCenter, 3)
		opposite, _ := runStart(dir, TabsAlignOpposite, 3)

		if center <= natural {
			t.Errorf("%v: centred, the run begins %d into the strip; packed, %d",
				dir, center, natural)
		}
		if opposite <= center {
			t.Errorf("%v: at the far end the run begins %d into the strip; centred, %d",
				dir, opposite, center)
		}
		if opposite >= bar {
			t.Errorf("%v: the run begins at %d, past the strip's %d", dir, opposite, bar)
		}
		// Centred means the slack is split, so the run's start is halfway
		// between where the two packed arrangements put it.
		if want := (natural + opposite) / 2; center != want {
			t.Errorf("%v: centred, the run begins at %d; halfway between %d and %d is %d",
				dir, center, natural, opposite, want)
		}
	}

	// And the mouse follows: a press at the run's start finds the first tab,
	// while the slack in front of it belongs to no tab at all.
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		form.AddChild(tt)
		for i := 0; i < 3; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetTabAlign(TabsAlignOpposite)
		tt.SetBounds(core.UnitRect{Width: 60 * 8, Height: 10 * 16})
		tt.Paint(core.NewPainter(px))

		hit := func(x core.Unit) int {
			tt.currentIndex = -1
			tt.handleTabBarPress(x)
			return tt.currentIndex
		}
		into := tt.tabRunOffset()
		at := into + 4*cell // inside the first tab, past its prefix
		if core.ChromeMirrored(tt) {
			at = tt.Bounds().Width - at - 1
		}
		if got := hit(at); got != 0 {
			t.Errorf("%v: a press %d into the packed run found tab %d, want the first",
				dir, into, got)
		}
		slack := into / 2 // well inside the room in front of the run
		if core.ChromeMirrored(tt) {
			slack = tt.Bounds().Width - slack - 1
		}
		if got := hit(slack); got >= 0 {
			t.Errorf("%v: a press in the slack in front of the run found tab %d", dir, got)
		}
	}

	// A strip that has to scroll spends its room on the overflow marks.
	for _, a := range []TabAlign{TabsAlignCenter, TabsAlignOpposite} {
		packed, _ := runStart(core.DirLTR, TabsAlignNatural, 40)
		got, _ := runStart(core.DirLTR, a, 40)
		if got != packed {
			t.Errorf("a scrolling strip moved its run to %d for align %d; it has no slack to place",
				got, a)
		}
	}
}

// A strip is made of more than its tabs, and a press has to tell the parts
// apart: the mark standing for the tabs that fell off the front, the two
// scroll buttons, a tab the strip's end cut short, and the close button on a
// tab's last cell. Each is found where the painter put it, so each turns over
// with the strip.
func TestAPressFindsThePartOfTheStripItLandedOn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const wCells = core.Unit(40)
	const tabs = 9

	type strip struct {
		tt     *TabTrinket
		ink    *inkRecorder
		closed int
	}
	build := func(dir core.Direction, pos TabPosition, scroll int, closable bool) *strip {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		s := &strip{ink: &inkRecorder{RenderBackend: px}, closed: -1}
		form := NewPanel()
		form.SetDirection(dir)
		s.tt = NewTabTrinket()
		s.tt.SetTabPosition(pos)
		s.tt.SetClosable(closable)
		s.tt.SetOnTabCloseRequested(func(i int) { s.closed = i })
		form.AddChild(s.tt)
		for i := 0; i < tabs; i++ {
			s.tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		s.tt.SetBounds(core.UnitRect{Width: wCells * cell, Height: 10 * 16})
		s.tt.tabScrollOffset = scroll
		s.tt.currentIndex = -1 // so anything the press selects is visible as one
		s.tt.Paint(core.NewPainter(s.ink))
		return s
	}
	// A place in the RUN, pressed where the strip put it on the screen.
	press := func(s *strip, runX core.Unit) {
		x := runX
		if core.ChromeMirrored(s.tt) {
			x = s.tt.Bounds().Width - runX - 1
		}
		s.tt.handleTabBarPress(x)
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		for _, pos := range []TabPosition{TabsTop, TabsBottom} {
			// The overflow mark stands for the tabs scrolled off the front of
			// the run, so pressing it brings the nearest of them back.
			s := build(dir, pos, 2, false)
			press(s, cell/2)
			if s.tt.tabScrollOffset != 1 || s.tt.currentIndex != 1 {
				t.Errorf("%v %v: a press on the overflow mark left the run at %d showing tab %d, "+
					"want the tab in front of it back", dir, pos, s.tt.tabScrollOffset, s.tt.currentIndex)
			}

			// The two scroll buttons stand at the far end of the run, three
			// cells each. They arm the strip's scrolling; they select nothing.
			s = build(dir, pos, 2, false)
			press(s, (wCells-6)*cell+cell/2)
			if s.tt.scrollButtonPressed != -1 || s.tt.currentIndex != -1 {
				t.Errorf("%v %v: a press on [<] armed %d and selected tab %d, want the strip to scroll back",
					dir, pos, s.tt.scrollButtonPressed, s.tt.currentIndex)
			}
			s = build(dir, pos, 2, false)
			press(s, (wCells-3)*cell+cell/2)
			if s.tt.scrollButtonPressed != 1 || s.tt.currentIndex != -1 {
				t.Errorf("%v %v: a press on [>] armed %d and selected tab %d, want the strip to scroll on",
					dir, pos, s.tt.scrollButtonPressed, s.tt.currentIndex)
			}

			// The buttons are painted over the strip, and a label measured in
			// proportional glyphs leaves the pen off the cell grid, so the
			// tab beside them can have its last cell hanging into their first.
			// What the eye finds there is the button.
			s = build(dir, pos, 0, false)
			press(s, (wCells-6)*cell)
			if s.tt.currentIndex != -1 || s.tt.tabScrollOffset != 0 {
				t.Errorf("%v %v: a press on the first unit of [<] selected tab %d and left the run at %d, "+
					"want the button the strip drew over the tab there",
					dir, pos, s.tt.currentIndex, s.tt.tabScrollOffset)
			}

			// The last thing on the run is a tab the strip had no room to draw
			// whole. Pressing what there is of it, or the dots that stand for
			// the rest, brings the whole of it into view.
			s = build(dir, pos, 0, false)
			press(s, (wCells-6)*cell-cell/2)
			cut := s.tt.currentIndex
			if cut <= 0 {
				t.Fatalf("%v %v: a press at the end of the run found tab %d", dir, pos, cut)
			}
			if s.tt.tabScrollOffset == 0 {
				t.Errorf("%v %v: a press on the tab the run was cut short at selected tab %d "+
					"and left the run where it was", dir, pos, cut)
			}
			s.ink.texts = nil
			s.tt.Paint(core.NewPainter(s.ink))
			if _, ok := s.ink.textAt(fmt.Sprintf("%04d", cut)); !ok {
				t.Errorf("%v %v: the strip selected tab %d without bringing it into view",
					dir, pos, cut)
			}

			// The close button is the label's last cell -- the cell the label
			// ENDS at, which is the one the eye reaches last, whichever way the
			// strip runs. The other end of the label selects, as any tab does.
			s = build(dir, pos, 0, true)
			lx, ok := s.ink.textAt("0000")
			if !ok {
				t.Fatalf("%v %v: the strip drew no first tab", dir, pos)
			}
			lw := s.tt.MeasureText("0000")
			last, first := lx+lw-cell/2, lx+cell/2
			if core.ChromeMirrored(s.tt) {
				last, first = first, last
			}
			s.tt.handleTabBarPress(last)
			if s.closed != 0 {
				t.Errorf("%v %v: a press on the first tab's close button closed tab %d",
					dir, pos, s.closed)
			}
			s = build(dir, pos, 0, true)
			s.tt.handleTabBarPress(first)
			if s.closed != -1 || s.tt.currentIndex != 0 {
				t.Errorf("%v %v: a press on the far side of the first tab's label closed %d and selected %d, "+
					"want it selected and nothing closed", dir, pos, s.closed, s.tt.currentIndex)
			}
		}
	}
}

// The run between two tabs belongs to both of them, and where the press falls
// in it says which. An even separator divides in half. The three-cell one a
// bottom strip draws beside its selected tab has a middle cell all of its own,
// carrying the slash: that cell goes to whichever of the two is selected
// already, so a press on the slash never tips the selection over.
func TestTheSlashBetweenTwoBottomTabsKeepsTheSelection(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// A press so many units past the end of tab 1's label, wherever the strip
	// put that label.
	after := func(dir core.Direction, into core.Unit) int {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := &inkRecorder{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(TabsBottom)
		form.AddChild(tt)
		for i := 0; i < 4; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetBounds(core.UnitRect{Width: 40 * cell, Height: 10 * 16})
		tt.SetCurrentIndex(1)
		tt.Paint(core.NewPainter(ink))
		lx, ok := ink.textAt("0001")
		if !ok {
			t.Fatalf("%v: the strip drew no second tab", dir)
		}
		// The separator follows the label along the RUN, so it stands on the
		// far side of it from where the run travels.
		x := lx + tt.MeasureText("0001") + into
		if core.ChromeMirrored(tt) {
			x = lx - into - 1
		}
		tt.handleTabBarPress(x)
		return tt.currentIndex
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		if got := after(dir, cell/2); got != 1 {
			t.Errorf("%v: a press on the first cell of the separator selected tab %d, want the tab in front of it", dir, got)
		}
		if got := after(dir, cell+cell/2); got != 1 {
			t.Errorf("%v: a press on the slash between tabs 1 and 2 selected tab %d, "+
				"want the selection to stay on the tab the slash leans into", dir, got)
		}
		if got := after(dir, 2*cell+cell/2); got != 2 {
			t.Errorf("%v: a press on the last cell of the separator selected tab %d, want the tab behind it", dir, got)
		}
	}
}

// Slack is what the strip has left after its run, so the run has to be
// measured as it is drawn. The two shapes join their tabs with a different
// number of cells, and a strip that reckons its run wider than it draws it
// keeps room back: pushed to the far end, the run stops short of it, and
// centred, it sits off to one side by half of what was kept back.
func TestACentredRunHasTheSameRoomAtBothEnds(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// Where the painted run begins and ends within the strip, from the marks
	// the strip made rather than from the widths it reckoned with.
	run := func(dir core.Direction, pos TabPosition, a TabAlign, cur int) (before, after core.Unit) {
		px, err := raster.New(600, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := &inkRecorder{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		tt.SetTabAlign(a)
		form.AddChild(tt)
		for i := 0; i < 3; i++ {
			tt.AddTab(fmt.Sprintf("%04d", i), NewPanel())
		}
		tt.SetBounds(core.UnitRect{Width: 40 * cell, Height: 10 * 16})
		tt.currentIndex = cur
		tt.Paint(core.NewPainter(ink))

		lo, hi := core.Unit(-1), core.Unit(-1)
		for _, sp := range tt.stripSpans {
			if sp.owner < 0 {
				continue // the strip's own furniture, not the run
			}
			if lo < 0 || sp.x < lo {
				lo = sp.x
			}
			if sp.x+sp.w > hi {
				hi = sp.x + sp.w
			}
		}
		if lo < 0 {
			t.Fatalf("%v %v: the strip drew no tabs", dir, pos)
		}
		return lo, tt.Bounds().Width - hi
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		for _, pos := range []TabPosition{TabsTop, TabsBottom} {
			// Every selection, because the join beside the selected tab is the
			// one the two shapes draw differently.
			for _, cur := range []int{-1, 0, 1, 2} {
				before, after := run(dir, pos, TabsAlignCenter, cur)
				// Odd slack cannot be split evenly, so the halves can differ
				// by the one unit that will not divide.
				if d := before - after; d > 1 || d < -1 {
					t.Errorf("%v %v cur=%d: centred, the run has %d in front of it and %d behind",
						dir, pos, cur, before, after)
				}

				if _, after := run(dir, pos, TabsAlignOpposite, cur); after != 0 {
					t.Errorf("%v %v cur=%d: pushed to the far end, the run stops %d short of it",
						dir, pos, cur, after)
				}
				if before, _ := run(dir, pos, TabsAlignNatural, cur); before != 0 {
					t.Errorf("%v %v cur=%d: packed, the run begins %d into the strip",
						dir, pos, cur, before)
				}
			}
		}
	}
}

// Given exactly the room it says it needs, a strip draws every tab whole. The
// width a strip asks for and the width it lays down are worked out separately,
// tab by tab, and a strip that asks for more than it draws spends the
// difference trimming a label and marking the loss with dots it had the room
// not to need.
func TestAStripGivenTheRoomItAsksForDrawsEveryTabWhole(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		for _, cur := range []int{0, 1, 2} {
			px, err := raster.New(900, 300)
			if err != nil {
				t.Fatal(err)
			}
			core.SetTextMeasurer(px)
			ink := newInk(t)
			tt := NewTabTrinket()
			tt.SetTabPosition(pos)
			NewPanel().AddChild(tt)
			for _, name := range []string{"One", "Second", "Third"} {
				tt.AddTab(name, NewPanel())
			}
			tt.SetCurrentIndex(cur)
			tt.SetBounds(core.UnitRect{Width: tt.calculateTotalTabsWidth(), Height: 10 * 16})
			tt.Paint(core.NewPainter(ink))

			for i, name := range []string{"One", "Second", "Third"} {
				if _, ok := ink.textAt(name); !ok {
					t.Errorf("%v cur=%d: tab %d's label %q was not drawn whole in the %d it asked for",
						pos, cur, i, name, tt.Bounds().Width)
				}
			}
			if _, ok := ink.cellAt('.'); ok {
				t.Errorf("%v cur=%d: the strip marked an overflow in the room it asked for", pos, cur)
			}
			if tt.tabsNeedScrolling() {
				t.Errorf("%v cur=%d: the strip wants to scroll in the room it asked for", pos, cur)
			}
		}
	}

	// The same reckoning decides, tab by tab, how much of a label a scrolling
	// strip has room for. A lead-in and a join are three cells each, so a
	// strip with exactly that much past its scroll buttons draws the first
	// label whole -- and one cell less is one cell short.
	const label = "Default"
	// Whether the whole label was drawn, and whether the join out of it was:
	// a strip that reckons the join wider than it draws has room for the
	// label and then stops, leaving the tab without the shape that joins it
	// to the run.
	fits := func(pos TabPosition, spare core.Unit) (whole, joined bool) {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := newInk(t)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		NewPanel().AddChild(tt)
		for _, name := range []string{label, "Second", "Third", "Fourth", "Fifth"} {
			tt.AddTab(name, NewPanel())
		}
		tt.SetCurrentIndex(0)
		// The buttons, the lead-in, the label, and the join out of it.
		room := 6*cell + 3*cell + tt.MeasureText(label) + 3*cell
		tt.SetBounds(core.UnitRect{Width: room + spare, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))
		if !tt.tabsNeedScrolling() {
			t.Fatalf("%v spare=%d: the strip has room for every tab; the case needs it to scroll", pos, spare)
		}
		_, whole = ink.textAt(label)
		// Each shape's join carries the diagonal its lead-in does not, so
		// finding it says the strip drew the join and not just the way in.
		join := '\\'
		if tt.tabEdge() == TabEdgeBottom {
			join = '/'
		}
		_, joined = ink.cellAt(join)
		return whole, joined
	}
	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		whole, joined := fits(pos, 0)
		if !whole {
			t.Errorf("%v: given exactly the room for its lead-in, its label and its join, "+
				"the strip trimmed %q anyway", pos, label)
		}
		if !joined {
			t.Errorf("%v: given exactly the room for its lead-in, its label and its join, "+
				"the strip drew %q and then stopped, without the join out of it", pos, label)
		}
		if whole, _ := fits(pos, -cell); whole {
			t.Errorf("%v: a cell short of the room for its lead-in, its label and its join, "+
				"the strip drew %q whole", pos, label)
		}
	}
}

// A strip marks an overflow at its end when there is a tab past it. Where the
// tabs it is showing fill it exactly, the run ends where the strip does and
// there is nothing to mark: the dots at the front, standing for the tabs
// scrolled off it, are the only ones.
func TestAStripShowingItsLastTabWholeMarksNoOverflowAtItsEnd(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const scroll = 2
	names := []string{"One", "Second", "Third", "Fourth", "Fifth"}

	// How much room the tabs it is showing take, learned from a strip with
	// room to spare rather than reckoned a second time here.
	strip := func(pos TabPosition, width core.Unit) (*TabTrinket, *inkRecorder) {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := newInk(t)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		NewPanel().AddChild(tt)
		for _, name := range names {
			tt.AddTab(name, NewPanel())
		}
		tt.SetCurrentIndex(scroll)
		tt.SetBounds(core.UnitRect{Width: width, Height: 10 * 16})
		tt.tabScrollOffset = scroll
		tt.Paint(core.NewPainter(ink))
		return tt, ink
	}

	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		roomy, _ := strip(pos, 90*cell)
		runEnd := core.Unit(0)
		for _, sp := range roomy.stripSpans {
			if sp.owner >= 0 && sp.x+sp.w > runEnd {
				runEnd = sp.x + sp.w
			}
		}
		// Exactly that much, plus the scroll buttons the strip will want.
		tt, ink := strip(pos, runEnd+6*cell)
		if !tt.tabsNeedScrolling() {
			t.Fatalf("%v: the strip has room for every tab; the case needs it to scroll", pos)
		}
		lo, hi, n := ink.span('.')
		if n != 3 {
			t.Errorf("%v: the strip drew %d dots, want the three standing for the tabs in front of the run",
				pos, n)
		}
		if n > 0 && (lo != 0 || hi >= 3*cell) {
			t.Errorf("%v: the strip drew dots from %d to %d; showing its last tab whole, "+
				"the only dots are the mark at the front", pos, lo, hi)
		}
		if tt.canScrollRight() {
			t.Errorf("%v: the strip says it can scroll on, with its last tab whole on it", pos)
		}
	}
}

// A terminal that cannot underline draws the strip's line with the underscore
// glyph, which sits at the foot of its cell. So each join beside the selected
// tab spends one of its three cells on an underscore -- on the side of the
// diagonal where the line it stands for runs. On a top strip the tabs rise out
// of the bar's line, which runs along the bottom of the row OUTSIDE the tab, so
// the underscore sits against the neighbouring label; on a bottom strip the
// line along the bottom is the selected tab's own outer edge, so it sits inside
// the tab instead. The other side of the diagonal is the focus marker's.
func TestAJoinCarriesItsLineOnTheSideThatLineRunsAlong(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// The two cells either side of a diagonal, with the middle tab selected.
	joins := func(pos TabPosition) (beforeIn, afterIn, beforeOut, afterOut rune) {
		px, err := raster.New(900, 300)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		ink := newInk(t)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		NewPanel().AddChild(tt)
		for _, name := range []string{"One", "Two", "Six"} {
			tt.AddTab(name, NewPanel())
		}
		tt.SetCurrentIndex(1)
		tt.SetBounds(core.UnitRect{Width: 30 * cell, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))

		// Each shape leads into its tab with one diagonal and out with the
		// other: a top strip rises on a slash, a bottom one hangs on a
		// backslash.
		in, out := '/', '\\'
		if tt.tabEdge() == TabEdgeBottom {
			in, out = '\\', '/'
		}
		inX, ok := ink.cellAt(in)
		if !ok {
			t.Fatalf("%v: the strip drew no way into its selected tab", pos)
		}
		outX, ok := ink.cellAt(out)
		if !ok {
			t.Fatalf("%v: the strip drew no way out of its selected tab", pos)
		}
		beforeIn, _ = ink.glyphAt(inX - cell)
		afterIn, _ = ink.glyphAt(inX + cell)
		beforeOut, _ = ink.glyphAt(outX - cell)
		afterOut, _ = ink.glyphAt(outX + cell)
		return beforeIn, afterIn, beforeOut, afterOut
	}

	// A top strip: the bar's line runs outside the tab, so the underscores are
	// the cells the neighbouring labels stand against.
	beforeIn, afterIn, beforeOut, afterOut := joins(TabsTop)
	if beforeIn != '_' {
		t.Errorf("a top strip leads into its tab over %q, want the line running in on an underscore", beforeIn)
	}
	if afterOut != '_' {
		t.Errorf("a top strip leaves its tab onto %q, want the line running on over an underscore", afterOut)
	}
	if afterIn == '_' || beforeOut == '_' {
		t.Errorf("a top strip put an underscore inside its tab, at %q and %q; the line there is the tab's top edge",
			afterIn, beforeOut)
	}

	// A bottom strip: the line along the bottom of the row is the tab's own
	// outer edge, so the underscores are inside it.
	beforeIn, afterIn, beforeOut, afterOut = joins(TabsBottom)
	if afterIn != '_' || beforeOut != '_' {
		t.Errorf("a bottom strip drew %q and %q inside its tab, want its outer edge run along on underscores",
			afterIn, beforeOut)
	}
	if beforeIn == '_' || afterOut == '_' {
		t.Errorf("a bottom strip put an underscore on the bar beside its tab, at %q and %q; "+
			"the bar's line there runs along the top of the row", beforeIn, afterOut)
	}
}

// A tab's caption reaches a cell target the way every other caption does: cut
// to fit first, prepared after, and the run that is drawn is the one the strip
// measures and places. A strip trims by CHARACTER, so preparing before the
// trim would cut a turned-over run at the wrong end.
func TestATabHandsItsCaptionOverInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	drawn := func(dir core.Direction, pos TabPosition, caption string) []string {
		px, err := raster.New(900, 400)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(dir)
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		form.AddChild(tt)
		tt.AddTab(caption, NewPanel())
		tt.AddTab("Beta", NewPanel())
		tt.SetBounds(core.UnitRect{Width: 40 * cell, Height: 10 * 16})
		tt.Paint(core.NewPainter(ink))
		return ink.texts
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		for _, pos := range []TabPosition{TabsTop, TabsBottom, TabsSide, TabsSideOpposite} {
			if !handedOver(drawn(dir, pos, shalom), turned) {
				t.Errorf("%v %v: the strip handed over %q, want the turned-over caption %q",
					dir, pos, drawn(dir, pos, shalom), turned)
			}
		}
	}

	// Trimmed, the fragment drawn is the turned-over form of what the strip
	// kept -- the FRONT of the caption. Prepared before the trim, the strip
	// would have cut the turned-over run instead and kept its far end: the
	// letters the reader reaches last, standing in for the ones it dropped.
	const long = "אבגדהוזחטי"
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		var fragments []string
		for _, got := range narrowDraw(t, dir, long) {
			if r := []rune(got); len(r) > 0 && strings.ContainsRune(long, r[0]) {
				fragments = append(fragments, got)
			}
		}
		if len(fragments) == 0 {
			t.Fatalf("%v: the narrow strip drew none of the caption", dir)
		}
		for _, f := range fragments {
			kept := reverseRunes(f)
			if !strings.HasPrefix(long, kept) {
				t.Errorf("%v: the strip drew %q, which turned back reads %q -- not the front "+
					"of the caption it kept", dir, f, kept)
			}
		}
	}
}

// narrowDraw paints a strip too narrow for its first caption, so the strip has
// to trim it.
func narrowDraw(t *testing.T, dir core.Direction, caption string) []string {
	t.Helper()
	px, err := raster.New(900, 400)
	if err != nil {
		t.Fatal(err)
	}
	ink := &cellInk{RenderBackend: px}
	form := NewPanel()
	form.SetDirection(dir)
	tt := NewTabTrinket()
	form.AddChild(tt)
	tt.AddTab(caption, NewPanel())
	tt.AddTab("Beta", NewPanel())
	tt.SetBounds(core.UnitRect{Width: 14 * cell, Height: 10 * 16})
	tt.Paint(core.NewPainter(ink))
	return ink.texts
}

func reverseRunes(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

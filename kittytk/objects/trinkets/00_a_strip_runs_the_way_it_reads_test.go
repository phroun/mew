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
			tt.AddTab(fmt.Sprintf("Tab%d", i), NewPanel())
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

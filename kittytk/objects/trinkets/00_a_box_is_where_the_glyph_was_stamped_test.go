package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/kittytk/text"
)

// stampRecorder keeps every clipped run the field stamped: the device-pixel
// offset it was drawn at, and the columns revealed of it.
type stampRecorder struct {
	*raster.Backend
	stamps []stamp
}

type stamp struct {
	off, lo, hi int
	s           string
}

func (r *stampRecorder) DrawTextPxClipped(xPx, yPx int, s string, st style.CellStyle,
	f *core.Font, clipX0, clipX1 int) int {
	r.stamps = append(r.stamps, stamp{off: xPx, lo: clipX0, hi: clipX1, s: s})
	return r.Backend.DrawTextPxClipped(xPx, yPx, s, st, f, clipX0, clipX1)
}

// The boxes and the ink are ONE layout.
//
// A box says where a rune was drawn; the run is stamped at an offset and
// revealed through a clip. So a rune's box has to be that offset plus where the
// shaper put the rune -- and the clip has to be the box. Move one without the
// other and the caret stands beside the glyph it names, the clip cuts a glyph
// in half, and the same run gets stamped twice into one place.
func TestABoxIsWhereTheGlyphWasStamped(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, c := range []struct {
		name   string
		text   string
		marked bool
	}{
		// A line whose reading ENDS at the left reserves room there, which
		// moves the whole run: the boxes and the stamp have to move together.
		{"reading ends left", "Hello. א", false},
		{"reading ends left, marked", "Hello. א", true},
		// And one broken into pieces by the marks, each moved by its own shift.
		{"pieces", "אHello World.", true},
		{"pieces unmarked", "אHello World.", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			px, err := raster.New(800, 200)
			if err != nil {
				t.Fatal(err)
			}
			core.SetTextMeasurer(px)
			rec := &stampRecorder{Backend: px}

			ti := NewTextInput()
			ti.SetShowBidiControls(c.marked)
			ti.SetText(c.text)
			// Wide enough that nothing scrolls and no arrow takes room, so the
			// field's coordinates are the run's.
			ti.SetBounds(core.UnitRect{Width: 120 * 8, Height: 16})
			ti.SetFocus()
			p := core.NewPainter(rec)
			ti.Paint(p)

			runes := []rune(c.text)
			ppu := p.PxPerUnitF()
			g := ti.runGeometry(runes, ti.EffectiveFont(), true, ti.markersShown(), ppu)

			// Where the shaper itself put each rune, before anything moved it.
			e := text.Shared()
			sp := e.ShapeRun(ti.EffectiveFont(), c.text)
			if sp == nil || len(sp.Lines) == 0 {
				t.Fatal("the line did not shape")
			}
			raw := &sp.Lines[0]

			for i := range runes {
				rawLo, _, ok := raw.BoxOfPx(i, ppu)
				if !ok {
					continue
				}
				lo, hi := g.loPx[i], g.hiPx[i]
				if hi <= lo {
					continue
				}
				// The stamp that revealed this box, and where it put the run.
				found := false
				for _, st := range rec.stamps {
					if st.s != c.text || lo < st.lo || hi > st.hi {
						continue
					}
					found = true
					if st.off+rawLo != lo {
						t.Errorf("rune %d (%q): its box starts at %d, but the run was "+
							"stamped at %d and the shaper put it at %d -- %d",
							i, string(runes[i]), lo, st.off, rawLo, st.off+rawLo)
					}
					break
				}
				if !found {
					t.Errorf("rune %d (%q): no stamp revealed its box [%d,%d) (stamps %v)",
						i, string(runes[i]), lo, hi, rec.stamps)
				}
			}
		})
	}
}

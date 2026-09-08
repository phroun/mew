package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Two rules in mew were written by hand -- the selection and its padding -- and
// the gutter's colour leaked into the text because nothing had been written for
// it. Rather than a third rule per thing that paints, the emitter catches
// whatever is left: past the point a row starts drifting, anything painted at
// the CELL goes. That reaches every trinket at once -- tree rows, list items,
// menu highlights -- without any of them knowing the question exists.
func TestTheEmitterDropsAGroundItCannotPlace(t *testing.T) {
	t.Cleanup(func() {
		core.ForgetHostBidi()
		core.SetRtlMarkMode("")
	})

	// A pointed Hebrew letter, then a blank and a glyph wearing a highlight
	// nothing has given up.
	row := func() string {
		b, out := newTestTUI(20, 2)
		b.colorDepth = 256
		b.BeginFrame()
		b.DrawText(0, 0, "לִ", style.DefaultStyle(), nil)
		b.DrawText(b.metrics.CellToUnitsX(1), 0, " ",
			style.DefaultStyle().WithBg(style.ColorBlue), nil)
		b.DrawText(b.metrics.CellToUnitsX(2), 0, "x",
			style.DefaultStyle().WithFg(style.ColorWhite).WithBg(style.ColorBlue), nil)
		b.EndFrame()
		return out.String()
	}

	// A host that places what it is sent keeps every fill it was given.
	core.SetHostAppliesBidi(true, false, false)
	got := row()
	if !strings.Contains(got, "44") {
		t.Errorf("a host that can place a fill lost one anyway: %q", got)
	}
	if strings.ContainsRune(got, fallbackBlank) {
		t.Errorf("a host that can place a fill got a shade instead: %q", got)
	}

	// One that miscounts loses the ground and the blank draws its own.
	core.SetHostAppliesBidi(true, false, true)
	got = row()
	if strings.Contains(got, "44") {
		t.Errorf("a ground survived past the drift: %q", got)
	}
	if !strings.ContainsRune(got, fallbackBlank) {
		t.Errorf("the blank after the run did not draw its own ground: %q", got)
	}
	if !strings.Contains(got, "x") {
		t.Errorf("the glyph did not survive the drop: %q", got)
	}
}

// Only what is painted at the cell is dropped. The glyph's own colour and its
// weight ride the reordering through and are kept.
func TestOnlyWhatIsPaintedAtTheCellIsDropped(t *testing.T) {
	s := style.DefaultStyle().WithFg(style.ColorRed).WithBg(style.ColorBlue)
	s.Attrs = style.StyleBold | style.StyleItalic | style.StyleUnderline |
		style.StyleReverse | style.StyleStrikethrough

	got := dropGround(s)
	if got.Bg != style.ColorDefault {
		t.Errorf("a background survived: %v", got.Bg)
	}
	if got.Attrs&cellPainted != 0 {
		t.Errorf("something drawn at the cell survived: %v", got.Attrs)
	}
	if got.Fg != style.ColorRed {
		t.Errorf("the glyph's own colour was dropped: %v", got.Fg)
	}
	if got.Attrs&style.StyleBold == 0 || got.Attrs&style.StyleItalic == 0 {
		t.Errorf("weight or slant was dropped, and both ride the glyph: %v", got.Attrs)
	}
}

// A blank draws its background rather than being given one -- except a black
// one, which is the ground a terminal already shows. That still goes, since a
// black fill landing on a coloured cell would black that cell out.
func TestABlankDrawsItsOwnGroundUnlessItIsBlack(t *testing.T) {
	for _, c := range []struct {
		name string
		bg   style.Color
		draw bool
	}{
		{"a colour is drawn", style.ColorBlue, true},
		{"bright black is a colour", style.ColorBrightBlack, true},
		{"black is the ground itself", style.ColorBlack, false},
		{"and so is no background at all", style.ColorDefault, false},
	} {
		ink, ok := groundAsInk(style.DefaultStyle().WithBg(c.bg))
		if ok != c.draw {
			t.Errorf("%s: groundAsInk drew=%v, want %v", c.name, ok, c.draw)
		}
		if ink.Bg != style.ColorDefault {
			t.Errorf("%s: the ground was kept: %v", c.name, ink.Bg)
		}
		if c.draw && ink.Fg != c.bg {
			t.Errorf("%s: drew %v, want the ground's own colour %v", c.name, ink.Fg, c.bg)
		}
	}
}

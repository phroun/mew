package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// shadowedTrinket reserves room for decoration on its trailing edges, the way
// a button does for its drop shadow: the size it asks for is its content plus
// that reservation.
type shadowedTrinket struct {
	core.TrinketBase
	content  core.UnitSize
	insets   core.UnitMargins
	align    core.Alignment
	alignSet bool
}

func newShadowedTrinket(w, h core.Unit, ins core.UnitMargins) *shadowedTrinket {
	s := &shadowedTrinket{content: core.UnitSize{Width: w, Height: h}, insets: ins}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	return s
}

func (s *shadowedTrinket) LayoutAlignment() (core.Alignment, bool) {
	return s.align, s.alignSet
}

func (s *shadowedTrinket) SizeHint() core.UnitSize {
	return core.UnitSize{
		Width:  s.content.Width + s.insets.Horizontal(),
		Height: s.content.Height + s.insets.Vertical(),
	}
}

func (s *shadowedTrinket) StyleInsets() core.UnitMargins { return s.insets }

// A trinket beside one with a drop shadow lines up with the CAP, not with the
// shadow row beneath it.
//
// A button asks for a row for its cap and another for its shadow, so a row
// holding one is two rows tall. Aligned against the whole of that, a one-row
// field sat half a row down -- or, filled, grew to two rows. The shadow row is
// set aside before anything is aligned, so the field is one row, level with the
// cap, and the shadow keeps the row below to itself.
func TestAShadowRowIsNotAlignedAgainst(t *testing.T) {
	const row = core.Unit(16)
	shadow := core.UnitMargins{Right: 8, Bottom: row}

	button := newShadowedTrinket(60, row, shadow)
	field := newSizedTrinket(24, row)
	field.align = core.AlignFill

	// A row sized to what it holds, which is what a vertical box gives an
	// inner row: two rows tall, because the button asked for its shadow.
	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	l.AddTrinket(button)
	l.AddTrinket(field)
	l.Layout(nil, core.UnitRect{Width: 400, Height: 2 * row})
	got := []core.UnitRect{button.Bounds(), field.Bounds()}

	if got[0].Height != 2*row {
		t.Errorf("the shadowed trinket is %d tall, want its cap and its shadow (%d)",
			got[0].Height, 2*row)
	}
	if got[1].Height != row {
		t.Errorf("its neighbour is %d tall, want one row (%d)", got[1].Height, row)
	}
	if got[0].Y != got[1].Y {
		t.Errorf("the cap starts at y=%d and the neighbour at y=%d; they should be level",
			got[0].Y, got[1].Y)
	}
}

// The allowance is the growth that decoration ALONE caused. Where something
// else is taller, the shadow already fits inside the line and nothing is set
// aside -- so a line is never made taller by having a shadow in it.
func TestNothingIsSetAsideWhenTheShadowAlreadyFits(t *testing.T) {
	const row = core.Unit(16)
	shadow := core.UnitMargins{Right: 8, Bottom: row}

	tall := newSizedTrinket(24, 4*row) // taller than the button, no decoration
	button := newShadowedTrinket(60, row, shadow)

	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	l.AddTrinket(tall)
	l.AddTrinket(button)
	if allow := l.styleAllowance(); allow.Bottom != 0 {
		t.Errorf("a line whose tallest item is content set aside %d for decoration, want 0",
			allow.Bottom)
	}
}

// Down a vertical box the same rule runs across: the column a shadow falls in
// is not part of what a right-aligned item aligns to.
func TestTheShadowColumnIsNotAlignedAgainstEither(t *testing.T) {
	const col = core.Unit(8)
	shadow := core.UnitMargins{Right: col, Bottom: 16}

	button := newShadowedTrinket(60, 16, shadow)
	field := newSizedTrinket(24, 16)
	field.align = core.AlignRight

	got := boxWith(core.Vertical, button, field)

	// The band ends one column short of the box, so a right-aligned neighbour
	// stops where the cap does rather than reaching into the shadow's column.
	if want := got[0].X + got[0].Width - col; got[1].X+got[1].Width != want {
		t.Errorf("the right-aligned neighbour ends at %d, want the cap's right edge %d",
			got[1].X+got[1].Width, want)
	}
}

// Centred, a trinket puts its CONTENT in the middle of the whole allocation and
// lets its decoration fall where it falls.
//
// A button in a row three deep belongs on the middle row with its shadow on the
// last, not pushed up a row so the pair straddles the middle. The shadow is not
// in the middle and has no business moving what is.
func TestCentringIgnoresTheDecoration(t *testing.T) {
	const row = core.Unit(16)
	button := newShadowedTrinket(60, row, core.UnitMargins{Right: 8, Bottom: row})
	button.align, button.alignSet = core.AlignMiddle, true

	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	l.AddTrinket(button)
	l.Layout(nil, core.UnitRect{Width: 400, Height: 3 * row})

	got := button.Bounds()
	if got.Y != row {
		t.Errorf("the cap starts at y=%d, want the middle row (%d)", got.Y, row)
	}
	if got.Height != 2*row {
		t.Errorf("the button is %d tall, want its cap and its shadow (%d)", got.Height, 2*row)
	}
	if end := got.Y + got.Height; end != 3*row {
		t.Errorf("the shadow ends at %d, want the last row's end (%d)", end, 3*row)
	}
}

// A trailing edge is the BAND's edge: a bottom- or right-aligned trinket leaves
// the row or column a neighbour's shadow reserves free, rather than sitting in
// it. One with a shadow of its own puts its cap on that edge and its shadow
// beyond it.
func TestTrailingAlignmentLeavesTheDecorationRoomFree(t *testing.T) {
	const row = core.Unit(16)
	button := newShadowedTrinket(60, row, core.UnitMargins{Right: 8, Bottom: row})
	field := newSizedTrinket(24, row)
	field.align = core.AlignBottom

	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	l.AddTrinket(button)
	l.AddTrinket(field)
	l.Layout(nil, core.UnitRect{Width: 400, Height: 3 * row})

	// The line is three rows; the shadow reserves the last, so the band ends at
	// row two and a bottom-aligned field sits on it.
	if got := field.Bounds().Y; got != row {
		t.Errorf("the bottom-aligned field starts at y=%d, want the band's last row (%d)",
			got, row)
	}
}

// Decoration is counted once. Along the MAIN axis the sizing pass already
// measured the whole trinket, shadow included; adding the insets again there
// stretched a button in a vertical box by its own shadow row.
func TestDecorationIsNotCountedTwice(t *testing.T) {
	const row = core.Unit(16)
	button := newShadowedTrinket(60, row, core.UnitMargins{Right: 8, Bottom: row})

	l := NewBoxLayout(core.Vertical)
	l.SetSpacing(0)
	l.AddTrinket(button)
	l.Layout(nil, core.UnitRect{Width: 400, Height: 400})

	if got, want := button.Bounds().Height, button.SizeHint().Height; got != want {
		t.Errorf("down a vertical box the trinket is %d tall, want the %d it asked for",
			got, want)
	}
}

// A cross axis a trinket says is FIXED does not grow, whatever it is asked to
// do. Fill has nothing to fill there, so it centres like the rest -- which is
// what puts a button's cap on the middle row of a row three deep rather than
// turning it into a three-row slab.
func TestAFixedCrossAxisDoesNotFill(t *testing.T) {
	const row = core.Unit(16)
	button := newShadowedTrinket(60, row, core.UnitMargins{Right: 8, Bottom: row})
	button.SetSizePolicy(core.NewSizePolicy(core.SizeFixed, core.SizeFixed))

	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	l.AddTrinket(button)
	l.Layout(nil, core.UnitRect{Width: 400, Height: 3 * row})

	got := button.Bounds()
	if got.Height != 2*row {
		t.Errorf("filled into a three-row row the trinket is %d tall, want its cap and shadow (%d)",
			got.Height, 2*row)
	}
	if got.Y != row {
		t.Errorf("its cap starts at y=%d, want the middle row (%d)", got.Y, row)
	}
}

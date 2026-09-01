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
	content core.UnitSize
	insets  core.UnitMargins
}

func newShadowedTrinket(w, h core.Unit, ins core.UnitMargins) *shadowedTrinket {
	s := &shadowedTrinket{content: core.UnitSize{Width: w, Height: h}, insets: ins}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	return s
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

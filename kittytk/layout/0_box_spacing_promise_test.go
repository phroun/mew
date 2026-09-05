package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// inlineChild is a trinket that reads as inline -- the small controls a
// horizontal box opens a column around.
type inlineChild struct {
	core.TrinketBase
	own core.UnitSize
}

func newInlineChild(w, h core.Unit) *inlineChild {
	c := &inlineChild{own: core.UnitSize{Width: w, Height: h}}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *inlineChild) SizeHint() core.UnitSize { return c.own }
func (c *inlineChild) IsInlineTrinket() bool   { return true }

// A box asks for the spacing it is going to consume.
//
// A horizontal box opens a column around inline trinkets on top of the
// configured spacing, and SizeHint counted only the configured spacing -- so a
// box handed exactly what it asked for laid its children out past its own
// right edge, and the last one's drop shadow fell outside whatever contained
// it.
func TestABoxAsksForTheSpacingItConsumes(t *testing.T) {
	l := NewHBoxLayout()
	l.SetSpacing(8)
	a, b := newInlineChild(56, 32), newInlineChild(72, 32)
	l.AddTrinket(a)
	l.AddTrinket(b)

	c := newDirContainer(core.DirLTR)
	c.AddChild(a)
	c.AddChild(b)

	// Hand the box exactly what it asks for, which is what a parent placing it
	// at its hint does.
	hint := l.SizeHint(c)
	l.Layout(c, core.UnitRect{Width: hint.Width, Height: hint.Height})

	right := b.Bounds().X + b.Bounds().Width
	if right > hint.Width {
		t.Errorf("the box asked for %d and laid its children out to %d", hint.Width, right)
	}

	// And the amount is the three columns an inline pair opens: one before the
	// first, one between them, one after the last. Agreement alone would hold
	// with any of them missing, so each is read off separately.
	m := core.FindEffectiveCellMetrics(c.Self())
	cell := m.UnitsPerCellWidth
	if got := a.Bounds().X; got != cell {
		t.Errorf("the first inline child starts at %d, want a column in at %d", got, cell)
	}
	if gap := b.Bounds().X - (a.Bounds().X + a.Bounds().Width); gap != cell {
		t.Errorf("the gap between two inline children is %d, want a column of %d", gap, cell)
	}
	if trail := hint.Width - right; trail != cell {
		t.Errorf("the box keeps %d after its last inline child, want a column of %d", trail, cell)
	}
	for _, k := range []*inlineChild{a, b} {
		if got := k.Bounds().Width; got != k.own.Width {
			t.Errorf("a child at the box's own size is %d wide, want its %d", got, k.own.Width)
		}
	}
}

// The same for a vertical box, whose spacing is the plain configured one.
func TestAVerticalBoxAsksForItsSpacing(t *testing.T) {
	l := NewVBoxLayout()
	l.SetSpacing(16)
	a, b := newInlineChild(56, 32), newInlineChild(72, 32)
	l.AddTrinket(a)
	l.AddTrinket(b)

	c := newDirContainer(core.DirLTR)
	c.AddChild(a)
	c.AddChild(b)

	hint := l.SizeHint(c)
	if want := core.Unit(32 + 32 + 16); hint.Height != want {
		t.Errorf("a vertical box of two 32-tall children at spacing 16 asks for %d, want %d",
			hint.Height, want)
	}
	l.Layout(c, core.UnitRect{Width: hint.Width, Height: hint.Height})
	if bottom := b.Bounds().Y + b.Bounds().Height; bottom > hint.Height {
		t.Errorf("the box asked for %d and laid its children out to %d", hint.Height, bottom)
	}
}

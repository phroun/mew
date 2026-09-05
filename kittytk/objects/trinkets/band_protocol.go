package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for grid bands. A BAND is one track of a grid, and which
// axis it belongs to is decided by the collection it is poured into rather
// than by anything it says about itself:
//
//	new panel layout=grid spacing=8 columns={
//	    new band id=labels
//	    new band id=fields stretch=1 min_size=80
//	} children={
//	    new label caption="Name:" row=0 column=labels halign=textend fill=none
//	    new textinput row=0 column=fields
//	}
//
// Bands take their index from their position, so a form never writes a track
// number down, and a child places itself by naming one -- which survives an
// insertion ahead of it that a number would not.

// wireBand is the virtual band record: described on its own statement, then
// handed to a grid by the collection it was written in.
type wireBand struct{ band layout.Band }

func init() {
	protocol.RegisterType("band", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireBand{} },
		Props: map[string]protocol.Property{
			"id": protocol.NewProperty("word", wprop("id", func(_ *protocol.BindContext, b *wireBand, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("id", v, f)
				if err != nil {
					return err
				}
				b.band.ID = w
				return nil
			})).Tip("Name children place themselves by (row=, column=). Blank leaves the band reachable only by its position."),
			"stretch": protocol.NewProperty("int", wprop("stretch", func(_ *protocol.BindContext, b *wireBand, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("stretch", v, f)
				if err != nil {
					return err
				}
				if n < 0 {
					return fmt.Errorf("stretch: %d is below 0", n)
				}
				b.band.Stretch = n
				return nil
			})).Def("0").Tip("Weight with which this band takes what is left over; 0 takes none of it."),
			"min_size": protocol.NewProperty("units", wprop("min_size", func(_ *protocol.BindContext, b *wireBand, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("min_size", v, f)
				if err != nil {
					return err
				}
				if n < 0 {
					return fmt.Errorf("min_size: %d is below 0", n)
				}
				b.band.Minimum = core.Unit(n)
				return nil
			})).Def("0").Tip("Floor across the band -- width for a column, height for a row -- in units."),
		},
	})
}

// bandCollection is a panel's columns= or rows=: each band is appended to the
// panel's grid, taking the index its position gives it.
func bandCollection(name string, add func(*layout.GridLayout, layout.Band)) protocol.Property {
	return protocol.NewCollection(func(parent, child any) error {
		p, ok := parent.(*Panel)
		if !ok {
			return fmt.Errorf("%s: wrong parent type %T", name, parent)
		}
		g, ok := p.LayoutManager().(*layout.GridLayout)
		if !ok {
			return fmt.Errorf("%s: set layout=grid before %s", name, name)
		}
		b, ok := child.(*wireBand)
		if !ok {
			return fmt.Errorf("%s: members must be bands, got %T", name, child)
		}
		add(g, b.band)
		return nil
	}).Members("band")
}

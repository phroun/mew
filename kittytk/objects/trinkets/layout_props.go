package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
)

// The wire vocabulary for the two layout managers that need more than the
// common hints: a grid, which has to be told which cell a child is in, and a
// flex layout, whose knobs live on the container.
//
// Grid and flex hints are common properties for the same reason stretch and
// halign are: what may be put in a grid is any trinket, so the hint cannot
// belong to a type.

// flexEnumProp is a panel property that sets one knob on the panel's flex
// layout. The layout has to exist first -- `layout=flex` before anything that
// configures it -- which is the same order `spacing` already requires.
func flexEnumProp[T any](name string, words map[string]T, set func(*layout.FlexLayout, T)) protocol.Property {
	return protocol.NewProperty("enum", wprop(name, func(_ *protocol.BindContext, p *Panel, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord(name, v, f)
		if err != nil {
			return err
		}
		value, ok := words[word]
		if !ok {
			return fmt.Errorf("%s: unknown value %q", name, word)
		}
		fl, ok := p.LayoutManager().(*layout.FlexLayout)
		if !ok {
			return fmt.Errorf("%s: set layout=flex before %s", name, name)
		}
		set(fl, value)
		return nil
	}))
}

// gridPlacementOf reads a trinket's grid placement, starting from a fresh one
// so setting one part does not reset the others.
func gridPlacementOf(w core.Trinket) core.GridPlacement {
	if h, ok := w.(interface {
		LayoutGridPlacement() (core.GridPlacement, bool)
	}); ok {
		if p, set := h.LayoutGridPlacement(); set {
			return p
		}
	}
	return core.GridPlacement{RowSpan: 1, ColumnSpan: 1}
}

// gridProp is one integer of a child's grid placement.
func gridProp(name string, into func(*core.GridPlacement, int), min int) protocol.Property {
	return protocol.NewProperty("int", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		if n < min {
			return fmt.Errorf("%s: %d is below %d", name, n, min)
		}
		h, ok := w.(interface{ SetLayoutGridPlacement(core.GridPlacement) })
		if !ok {
			return fmt.Errorf("%s: not supported by this type", name)
		}
		p := gridPlacementOf(w)
		into(&p, n)
		h.SetLayoutGridPlacement(p)
		return nil
	}))
}

// gridTrackProp is a child's row or column, which is either an index or the
// id of a band. A name is kept as written and settled against the grid's
// bands at layout time, so a child may be given before the band it names.
func gridTrackProp(name string, into func(*core.GridPlacement, int, string)) protocol.Property {
	return protocol.NewProperty("int", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		h, ok := w.(interface{ SetLayoutGridPlacement(core.GridPlacement) })
		if !ok {
			return fmt.Errorf("%s: not supported by this type", name)
		}
		p := gridPlacementOf(w)
		switch {
		case f == protocol.FlagNone && v != nil && v.Kind == protocol.WordValue:
			into(&p, 0, v.Word)
		case f == protocol.FlagNone && v != nil && v.Kind == protocol.NumberValue && v.IsInt:
			n := int(v.Number)
			if n < 0 {
				return fmt.Errorf("%s: %d is below 0", name, n)
			}
			into(&p, n, "")
		default:
			return fmt.Errorf("%s: expected an index or a band id", name)
		}
		h.SetLayoutGridPlacement(p)
		return nil
	}))
}

// flexHintsOf reads a trinket's flex hints, starting from the defaults a flex
// layout gives a child that says nothing: no growing, ordinary shrinking, and
// a size taken from the child itself.
func flexHintsOf(w core.Trinket) core.FlexHints {
	if h, ok := w.(interface {
		LayoutFlex() (core.FlexHints, bool)
	}); ok {
		if f, set := h.LayoutFlex(); set {
			return f
		}
	}
	return core.FlexHints{Shrink: 1}
}

func setFlexHints(name string, w core.Trinket, f core.FlexHints) error {
	h, ok := w.(interface{ SetLayoutFlex(core.FlexHints) })
	if !ok {
		return fmt.Errorf("%s: not supported by this type", name)
	}
	h.SetLayoutFlex(f)
	return nil
}

// flexFloatProp is a child's grow or shrink factor.
func flexFloatProp(name string, into func(*core.FlexHints, float64)) protocol.Property {
	return protocol.NewProperty("float", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		x, err := protocol.AsFloat(name, v, f)
		if err != nil {
			return err
		}
		if x < 0 {
			return fmt.Errorf("%s: %v is below 0", name, x)
		}
		hints := flexHintsOf(w)
		into(&hints, x)
		return setFlexHints(name, w, hints)
	}))
}

// registerLayoutProperties wires the grid and flex hints that travel with a
// child.
func registerLayoutProperties() {
	protocol.RegisterCommonProperty("row", gridTrackProp("row",
		func(p *core.GridPlacement, n int, id string) { p.Row, p.RowID = n, id }).
		Def("0").Tip("Grid row this child occupies: an index, or the id of a band."))
	protocol.RegisterCommonProperty("column", gridTrackProp("column",
		func(p *core.GridPlacement, n int, id string) { p.Column, p.ColumnID = n, id }).
		Def("0").Tip("Grid column this child occupies: an index, or the id of a band."))
	protocol.RegisterCommonProperty("row_span", gridProp("row_span",
		func(p *core.GridPlacement, n int) { p.RowSpan = n }, 1).
		Def("1").Tip("How many grid rows this child covers."))
	protocol.RegisterCommonProperty("column_span", gridProp("column_span",
		func(p *core.GridPlacement, n int) { p.ColumnSpan = n }, 1).
		Def("1").Tip("How many grid columns this child covers."))
	protocol.RegisterCommonProperty("row_stretch", gridProp("row_stretch",
		func(p *core.GridPlacement, n int) { p.RowStretch = n }, 0).
		Def("0").Tip("Weight with which this child's grid ROW takes leftover height."))
	protocol.RegisterCommonProperty("column_stretch", gridProp("column_stretch",
		func(p *core.GridPlacement, n int) { p.ColumnStretch = n }, 0).
		Def("0").Tip("Weight with which this child's grid COLUMN takes leftover width."))

	protocol.RegisterCommonProperty("grow", flexFloatProp("grow",
		func(h *core.FlexHints, x float64) { h.Grow = x }).
		Def("0").Tip("Share of a flex line's leftover space this child takes."))
	protocol.RegisterCommonProperty("shrink", flexFloatProp("shrink",
		func(h *core.FlexHints, x float64) { h.Shrink, h.ShrinkSet = x, true }).
		Def("1").Tip("Share of a flex line's shortfall this child gives up; never below its minimum."))
	protocol.RegisterCommonProperty("basis", protocol.NewProperty("units", wprop("basis", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt("basis", v, f)
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("basis: %d is below 0", n)
		}
		hints := flexHintsOf(w)
		hints.Basis = core.Unit(n)
		return setFlexHints("basis", w, hints)
	})).Def("0").Tip("Size a flex child starts from, in units; 0 takes it from the child."))
}

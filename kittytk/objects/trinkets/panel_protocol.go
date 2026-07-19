package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// Wire registration for Panel (see docs/property-vocabulary.md).
// Order matters where properties interact: set layout before spacing.
func init() {
	regTrinket("panel",
		func() core.Trinket { return NewPanel() },
		map[string]protocol.Property{
			"border": boolProp("border", (*Panel).SetBorder).Tip("Draw a border around the panel").Def("false"),
			"border_style": protocol.NewProperty("enum", wprop("border_style", func(_ *protocol.BindContext, p *Panel, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("border_style", v, f)
				if err != nil {
					return err
				}
				styles := map[string]style.BorderStyle{
					"single":  style.BorderSingle,
					"double":  style.BorderDouble,
					"rounded": style.BorderRounded,
					"heavy":   style.BorderHeavy,
					"ascii":   style.BorderASCII,
				}
				bs, ok := styles[w]
				if !ok {
					return fmt.Errorf("border_style: unknown value %q", w)
				}
				p.SetBorderStyle(bs)
				return nil
			})).OneOf("single", "double", "rounded", "heavy", "ascii").Tip("Border line style"),
			"layout": protocol.NewProperty("enum", wprop("layout", func(_ *protocol.BindContext, p *Panel, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("layout", v, f)
				if err != nil {
					return err
				}
				switch w {
				case "vbox":
					p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
				case "hbox":
					p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))
				case "none":
					// no layout manager
				default:
					return fmt.Errorf("layout: unknown value %q (grid arrives later)", w)
				}
				return nil
			})).OneOf("vbox", "hbox", "none").Tip("Child layout manager"),
			"fixed_width": protocol.NewProperty("int", wprop("fixed_width", func(_ *protocol.BindContext, p *Panel, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("fixed_width", v, f)
				if err != nil {
					return err
				}
				p.SetFixedWidth(core.Unit(n))
				return nil
			})).Tip("Fixed panel width in units"),
			"spacing": protocol.NewProperty("int", wprop("spacing", func(_ *protocol.BindContext, p *Panel, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("spacing", v, f)
				if err != nil {
					return err
				}
				lm, ok := p.LayoutManager().(interface{ SetSpacing(core.Unit) })
				if !ok {
					return fmt.Errorf("spacing: set layout before spacing")
				}
				lm.SetSpacing(core.Unit(n))
				return nil
			})).Tip("Spacing between laid-out children"),
		},
		func(parent, child core.Trinket) error {
			parent.(*Panel).AddChild(child)
			return nil
		},
		nil,
	)
}

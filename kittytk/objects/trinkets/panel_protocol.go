package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// Wire registration for Panel; the wiki's Panel page is
// generated from it.
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
				case "grid":
					p.SetLayoutManager(layout.NewGridLayout())
				case "flex":
					p.SetLayoutManager(layout.NewFlexLayout())
				case "none":
					// no layout manager
				default:
					return fmt.Errorf("layout: unknown value %q", w)
				}
				return nil
			})).OneOf("vbox", "hbox", "grid", "flex", "none").Tip("Child layout manager"),
			"columns": bandCollection("columns", (*layout.GridLayout).AddColumn).
				Tip("The grid's columns, in order, left to right."),
			"rows": bandCollection("rows", (*layout.GridLayout).AddRow).
				Tip("The grid's rows, in order, top to bottom."),
			"flex_direction": flexEnumProp("flex_direction",
				map[string]layout.FlexDirection{
					"row":            layout.FlexRow,
					"row_reverse":    layout.FlexRowReverse,
					"column":         layout.FlexColumn,
					"column_reverse": layout.FlexColumnReverse,
				},
				func(l *layout.FlexLayout, v layout.FlexDirection) { l.SetDirection(v) },
			).OneOf("row", "row_reverse", "column", "column_reverse").Def("row").
				Tip("Flex main axis, and whether it runs backwards."),
			"flex_wrap": flexEnumProp("flex_wrap",
				map[string]layout.FlexWrap{
					"none":         layout.FlexNoWrap,
					"wrap":         layout.FlexWrapNormal,
					"wrap_reverse": layout.FlexWrapReverse,
				},
				func(l *layout.FlexLayout, v layout.FlexWrap) { l.SetWrap(v) },
			).OneOf("none", "wrap", "wrap_reverse").Def("none").
				Tip("Whether a flex run breaks into further lines when it overflows."),
			"justify": flexEnumProp("justify",
				map[string]layout.FlexJustify{
					"begin":         layout.FlexJustifyStart,
					"end":           layout.FlexJustifyEnd,
					"center":        layout.FlexJustifyCenter,
					"space_between": layout.FlexJustifySpaceBetween,
					"space_around":  layout.FlexJustifySpaceAround,
					"space_evenly":  layout.FlexJustifySpaceEvenly,
				},
				func(l *layout.FlexLayout, v layout.FlexJustify) { l.SetJustify(v) },
			).OneOf("begin", "end", "center", "space_between", "space_around", "space_evenly").
				Def("begin").Tip("Where a flex line's leftover space along the main axis goes."),
			"align_items": flexEnumProp("align_items",
				map[string]layout.FlexAlign{
					"stretch": layout.FlexAlignStretch,
					"begin":   layout.FlexAlignStart,
					"end":     layout.FlexAlignEnd,
					"center":  layout.FlexAlignCenter,
				},
				func(l *layout.FlexLayout, v layout.FlexAlign) { l.SetAlignItems(v) },
			).OneOf("stretch", "begin", "end", "center").Def("stretch").
				Tip("Where flex children sit across the line; a child's own alignment wins."),
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
		nil,
		func(parent, child core.Trinket) error {
			parent.(*Panel).AddChild(child)
			return nil
		},
		nil,
	)
}

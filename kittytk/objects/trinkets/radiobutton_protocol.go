package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for RadioButton (see docs/property-vocabulary.md).
// Group membership is a plain named property: buttons sharing a
// group= word on the same connection exclude each other. The groups
// themselves live in the connection's stash - no container trinket,
// no positional coupling.
func init() {
	regTrinket("radiobutton",
		func() core.Trinket { return NewRadioButton("") },
		map[string]protocol.Property{
			"caption": stringProp("caption", (*RadioButton).SetText).Tip("Radio button label text."),
			"checked": boolProp("checked", (*RadioButton).SetChecked).Tip("Whether this button is selected.").Def("false"),
			"wrap":    boolProp("wrap", (*RadioButton).SetWordWrap).Tip("Word-wrap the label.").Def("false"),
			"group": protocol.NewProperty("word", wprop("group", func(ctx *protocol.BindContext, r *RadioButton, v *protocol.Value, f protocol.FlagState) error {
				word, err := protocol.AsWord("group", v, f)
				if err != nil {
					return err
				}
				g := ctx.Stash("radiogroup:"+word, func() any { return NewRadioGroup() }).(*RadioGroup)
				g.AddButton(r)
				return nil
			})).Tip("Radio group membership."),
		},
		nil,
		func(ctx *protocol.BindContext, w core.Trinket) {
			r := w.(*RadioButton)
			id := trinketID(r)
			r.SetOnToggled(func(checked bool) {
				flag := protocol.FlagFalse
				if checked {
					flag = protocol.FlagTrue
				}
				ctx.EmitEvent(protocol.NewEvent("toggle").
					WithUint("trinket", id).WithFlag("checked", flag))
			})
		},
	)
}

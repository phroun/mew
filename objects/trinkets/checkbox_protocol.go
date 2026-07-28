package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for Checkbox (see docs/property-vocabulary.md).
// `checked` is tri-capable per D16: checked / !checked / ?checked.
func init() {
	regTrinket("checkbox",
		func() core.Trinket { return NewCheckbox("") },
		map[string]protocol.Property{
			"caption": stringProp("caption", (*Checkbox).SetText).Tip("Checkbox label text."),
			"checked": protocol.NewProperty("flag", wprop("checked", func(_ *protocol.BindContext, c *Checkbox, v *protocol.Value, f protocol.FlagState) error {
				switch f {
				case protocol.FlagTrue:
					c.SetCheckState(Checked)
				case protocol.FlagFalse:
					c.SetCheckState(Unchecked)
				case protocol.FlagIndeterminate:
					c.SetCheckState(PartiallyChecked)
				default:
					// Long forms for generic tooling (D12/D16).
					w, err := protocol.AsWord("checked", v, f)
					if err != nil {
						return err
					}
					switch w {
					case "true":
						c.SetCheckState(Checked)
					case "false":
						c.SetCheckState(Unchecked)
					case "mixed":
						c.SetCheckState(PartiallyChecked)
					default:
						return fmt.Errorf("checked: unknown value %q", w)
					}
				}
				return nil
			})).Tip("Checked state (tri-capable: on/off/mixed)."),
			"tristate": boolProp("tristate", (*Checkbox).SetTriState).Tip("Allow clicking through the mixed state.").Def("false"),
			"wrap":     boolProp("wrap", (*Checkbox).SetWordWrap).Tip("Word-wrap the label.").Def("false"),
			"action":   actionProp("action").Tip("Optional command dispatched on toggle."),
		},
		nil,
		func(ctx *protocol.BindContext, w core.Trinket) {
			c := w.(*Checkbox)
			id := trinketID(c)
			c.SetOnStateChanged(func(state CheckState) {
				ctx.FireAction(id)
				flag := protocol.FlagFalse
				switch state {
				case Checked:
					flag = protocol.FlagTrue
				case PartiallyChecked:
					flag = protocol.FlagIndeterminate
				}
				ctx.EmitEvent(protocol.NewEvent("toggle").
					WithUint("trinket", id).WithFlag("checked", flag))
			})
		},
	)
}

package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for Button (see docs/property-vocabulary.md).
func init() {
	regTrinket("button",
		func() core.Trinket { return NewButton("") },
		map[string]protocol.Property{
			"caption": stringProp("caption", (*Button).SetText).Tip("Display text (& = accelerator)."),
			"default": boolProp("default", (*Button).SetDefault).Tip("Default-button styling and Enter behavior.").Def("false"),
			// action is OPTIONAL: when set, clicking dispatches the
			// command ID (via BindContext.FireAction in the click
			// wiring below).
			"action": actionProp("action").Tip("Optional command dispatched on click."),
		},
		nil, // buttons take no children
		func(ctx *protocol.BindContext, w core.Trinket) {
			b := w.(*Button)
			id := trinketID(b)
			b.SetOnClick(func() {
				ctx.FireAction(id)
				ctx.EmitEvent(protocol.NewEvent("click").WithUint("trinket", id))
			})
		},
	)
}

package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for Button; the wiki's Button page is
// generated from it.
func init() {
	regTrinket("button",
		func() core.Trinket { return NewButton("") },
		map[string]protocol.Property{
			// "&" marks a mnemonic letter, as in a menu title: it is not
			// shown, and "&&" is an ampersand. Whether the letter is drawn
			// out and answered to is up to what the button sits in -- a
			// message box's area offers its buttons by letter while it holds
			// the focus -- and a button anywhere else paints as before.
			"caption": stringProp("caption", (*Button).SetText).Tip(`Display text. "&" marks a mnemonic letter (not shown; "&&" is an ampersand), answered by a bare keypress where the container offers its buttons by letter.`),
			"default": boolProp("default", (*Button).SetDefault).Tip("Default-button styling and Enter behavior.").Def("false"),
			// action is OPTIONAL: when set, clicking dispatches the
			// command ID (via BindContext.FireAction in the click
			// wiring below).
			"action": actionProp("action").Tip("Optional command dispatched on click."),
		},
		map[string]protocol.EventDesc{
			"command": protocol.NewEventDesc("The command named by action= was activated. Raised only when action= is set, and before the click event, so a client may act on either. Carries the command rather than the trinket, because a command is what an application binds to and the same one may be reachable from several places.").
				Field("action", "word", "The command ID from action=."),
			"click": protocol.NewEventDesc("The button was activated, by pointer or by keyboard.").
				Field("trinket", "uint", "The button's object ID."),
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

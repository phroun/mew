package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for TextInput (see docs/property-vocabulary.md).
//
// cursor, selection, readonly and mask are NOT here. The vocabulary doc lists
// them and this comment used to claim they arrived with the set verb; neither
// was true. See OPEN-FINDINGS.md -- readonly/mask/max_length are simply
// missing, and cursor/selection were deliberately deferred (d2-read-audit.md,
// C2 exception 3).
func init() {
	regTrinket("textinput",
		func() core.Trinket { return NewTextInput() },
		map[string]protocol.Property{
			"text":        stringProp("text", (*TextInput).SetText).Tip("Editable content (server-authoritative)."),
			"placeholder": stringProp("placeholder", (*TextInput).SetPlaceholder).Tip("Placeholder text shown when empty."),
		},
		map[string]protocol.EventDesc{
			"change": protocol.NewEventDesc("The content changed through user editing — a typed character, a deletion, a paste, or a committed composition. A set from the client does not raise it.").
				Field("trinket", "uint", "The field's object ID.").
				Field("text", "string", "The full new content."),
			"complete": protocol.NewEventDesc("The person typing said they are done with the field — Return, or whatever else the keymap makes mean trinket_activate here. Not a submit: nothing is sent anywhere and the field is still editable, still holding its text, which it carries so a handler need not read it back.").
				Field("trinket", "uint", "The field's object ID.").
				Field("text", "string", "The content at the moment it was completed."),
		},
		nil,
		func(ctx *protocol.BindContext, w core.Trinket) {
			t := w.(*TextInput)
			id := trinketID(t)
			t.SetOnTextChanged(func(text string) {
				ctx.EmitEvent(protocol.NewEvent("change").
					WithUint("trinket", id).WithString("text", text))
			})
			t.SetOnComplete(func() {
				ctx.EmitEvent(protocol.NewEvent("complete").
					WithUint("trinket", id).WithString("text", t.Text()))
			})
		},
	)
}

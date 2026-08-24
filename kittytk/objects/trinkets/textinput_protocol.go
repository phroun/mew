package trinkets

import (
	"fmt"

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
			"readonly":    boolProp("readonly", (*TextInput).SetReadOnly).Tip("Content can be read and selected but not edited.").Def("false"),
			"max_length":  intProp("max_length", (*TextInput).SetMaxLength).Tip("Longest content the field accepts, in runes. -1 is no limit.").Def("-1"),
			// echo is what the field PAINTS, which is not what it holds: the
			// text property is the content either way, and a masked field is
			// masked on screen only.
			"echo": protocol.NewProperty("enum", wprop("echo", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
				word, err := protocol.AsWord("echo", v, f)
				if err != nil {
					return err
				}
				mode, ok := map[string]EchoMode{
					"normal":   EchoNormal,
					"password": EchoPassword,
					"none":     EchoNoEcho,
				}[word]
				if !ok {
					return fmt.Errorf("echo: unknown value %q (normal, password, none)", word)
				}
				w.(*TextInput).SetEchoMode(mode)
				return nil
			})).OneOf("normal", "password", "none").Def("normal").
				Tip("How the content is painted: normally, masked (see mask), or not at all. A masked field also reports itself as a password field to a screen reader."),
			"mask": stringProp("mask", func(t *TextInput, s string) {
				r := []rune(s)
				if len(r) == 0 {
					t.SetMaskChar(0) // back to the default bullet
					return
				}
				t.SetMaskChar(r[0])
			}).Tip("The single character echo=password paints for each rune. Blank restores the default bullet; only the first character is used.").Def("•"),
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

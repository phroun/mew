package trinkets

import (
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for InputDialog. It finishes the way a message box does,
// with a result word, and carries the text when the answer was OK:
//
//	dlg=new inputdialog title="Rename" prompt="New name:" text="draft.txt"
//	sub dlg finish
//	-> event finish trinket=<id> result=ok text="notes.txt"
//
// The result is ok, cancel, or none for a window closed without choosing. An
// application's own `destroy` takes the dialog away without a finish: the one
// who asked is the one who stopped asking.
func init() {
	stringProp := func(name, tip string, set func(*InputDialog, string)) protocol.Property {
		return protocol.NewProperty("string", wprop(name, func(_ *protocol.BindContext, d *InputDialog, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString(name, v, f)
			if err != nil {
				return err
			}
			set(d, s)
			return nil
		})).Tip(tip)
	}

	protocol.RegisterType("inputdialog", &protocol.TypeSpec{
		Events: map[string]protocol.EventDesc{
			"finish": protocol.NewEventDesc("The input dialog was answered.").
				Field("trinket", "uint", "The input dialog's object ID.").
				Field("result", "word", "ok, cancel, or none for a window closed without choosing.").
				Field("text", "string", "What was typed, when the result is ok."),
		},
		New: func() any { return NewInputDialog("", "", "") },
		ID: func(t any) uint64 {
			return uint64(t.(*InputDialog).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			d := target.(*InputDialog)
			id := uint64(d.ObjectID())
			d.SetOnFinished(func(text string, accepted bool) {
				ev := protocol.NewEvent("finish").WithUint("trinket", id)
				switch d.Answer() {
				case ResultOK:
					ev = ev.WithWord("result", "ok").WithString("text", text)
				case ResultCancel:
					ev = ev.WithWord("result", "cancel")
				default:
					ev = ev.WithWord("result", "none")
				}
				ctx.EmitEvent(ev)
			})
		},
		Props: map[string]protocol.Property{
			"title":       stringProp("title", "Dialog title bar text", func(d *InputDialog, s string) { d.SetTitle(s) }),
			"prompt":      stringProp("prompt", "What the dialog asks for, shown above the field", (*InputDialog).SetPrompt),
			"text":        stringProp("text", "What is in the field when the dialog opens, selected so typing replaces it", (*InputDialog).SetText),
			"placeholder": stringProp("placeholder", "Hint shown while the field is empty", (*InputDialog).SetPlaceholder),
		},
		Destroy: func(t any) error {
			t.(*InputDialog).dismiss()
			return nil
		},
	})
}

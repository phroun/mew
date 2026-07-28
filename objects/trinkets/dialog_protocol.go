package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Wire registration for MessageBox. Buttons are individual flags per
// D12 (no bitsets on the wire); the finish event reports which one
// closed the dialog:
//
//	dlg=new messagebox title="Confirm" text="Save changes?" yes no cancel icon=question
//	sub dlg finish
//	-> event finish trinket=<id> result=yes
func init() {
	buttonFlags := map[string]DialogButton{
		"ok":      ButtonOK,
		"cancel":  ButtonCancel,
		"yes":     ButtonYes,
		"no":      ButtonNo,
		"retry":   ButtonRetry,
		"ignore":  ButtonIgnore,
		"abort":   ButtonAbort,
		"save":    ButtonSave,
		"discard": ButtonDiscard,
		"apply":   ButtonApply,
		"help":    ButtonHelp,
	}
	resultWords := map[DialogResult]string{
		ResultOK:      "ok",
		ResultCancel:  "cancel",
		ResultYes:     "yes",
		ResultNo:      "no",
		ResultRetry:   "retry",
		ResultIgnore:  "ignore",
		ResultAbort:   "abort",
		ResultSave:    "save",
		ResultDiscard: "discard",
		ResultApply:   "apply",
		ResultHelp:    "help",
	}

	props := map[string]protocol.Property{
		"title": protocol.NewProperty("string", wprop("title", func(_ *protocol.BindContext, m *MessageBox, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString("title", v, f)
			if err != nil {
				return err
			}
			m.SetTitle(s)
			return nil
		})).Tip("Dialog title bar text"),
		"text": protocol.NewProperty("string", wprop("text", func(_ *protocol.BindContext, m *MessageBox, v *protocol.Value, f protocol.FlagState) error {
			s, err := protocol.AsString("text", v, f)
			if err != nil {
				return err
			}
			m.SetText(s)
			return nil
		})).Tip("Message body text"),
		"icon": protocol.NewProperty("enum", wprop("icon", func(_ *protocol.BindContext, m *MessageBox, v *protocol.Value, f protocol.FlagState) error {
			w, err := protocol.AsWord("icon", v, f)
			if err != nil {
				return err
			}
			icon, ok := map[string]MessageBoxIcon{
				"none":        IconNone,
				"information": IconInformation,
				"warning":     IconWarning,
				"error":       IconError,
				"question":    IconQuestion,
			}[w]
			if !ok {
				return fmt.Errorf("icon: unknown value %q", w)
			}
			m.SetIcon(icon)
			return nil
		})).OneOf("none", "information", "warning", "error", "question").Tip("Icon shown beside the message"),
	}
	for name, flag := range buttonFlags {
		name, flag := name, flag
		props[name] = protocol.NewProperty("flag", wprop(name, func(_ *protocol.BindContext, m *MessageBox, v *protocol.Value, f protocol.FlagState) error {
			b, err := protocol.AsBool(name, v, f)
			if err != nil {
				return err
			}
			if b {
				m.SetButtons(m.Buttons() | flag)
			} else {
				m.SetButtons(m.Buttons() &^ flag)
			}
			return nil
		})).Tip("Include the " + name + " button").Def("false")
	}

	protocol.RegisterType("messagebox", &protocol.TypeSpec{
		New: func() any { return NewMessageBox("", "", 0) },
		ID: func(t any) uint64 {
			return uint64(t.(*MessageBox).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			m := target.(*MessageBox)
			id := uint64(m.ObjectID())
			m.SetOnFinished(func(result DialogResult) {
				word, ok := resultWords[result]
				if !ok {
					word = "none"
				}
				ctx.EmitEvent(protocol.NewEvent("finish").
					WithUint("trinket", id).WithWord("result", word))
			})
		},
		Props: props,
		Destroy: func(t any) error {
			t.(*MessageBox).Close()
			return nil
		},
	})
}

//go:build mew

package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the mew-backed "editor" (the -tags mew build). It
// registers the SAME property/event names as the vanilla placeholder
// (editor_protocol.go, //go:build !mew) — mutually exclusive by build tag — so
// an app targets one contract (docs/editor-trinket.md) and runs identically on
// either. The difference: here the rich properties are HONORED (mapped into the
// mew session's config), not ignored.
//
// The mew session runs server-side, driving the internal PurfecTerm directly,
// so there is no client-side relay (nil bind arg from the host's perspective) —
// but we DO bind, to turn session end into a `commit` event.
func init() {
	regTrinket("editor",
		func() core.Trinket { return NewEditor() },
		map[string]protocol.Property{
			// Core.
			"value":       stringProp("value", (*Editor).SetValue).Tip("Ephemeral text; the commit event carries the final value."),
			"filename":    stringProp("filename", (*Editor).SetFilename).Tip("File to open (wins over value); opened through the host FS."),
			"placeholder": stringProp("placeholder", (*Editor).SetPlaceholder).Tip("Hint shown when empty."),
			"caption":     stringProp("caption", (*Editor).SetCaption).Tip("A title for the editor."),
			"readonly":    boolProp("readonly", (*Editor).SetReadOnly).Tip("View only.").Def("false"),

			// Rich — honored by mew, defaulting to inherit (mew's own resolution:
			// the user's editor.conf, grammar overlays, syntax detection).
			"wrap":         mewBoolProp("wrap", (*Editor).SetWrap).Tip("Soft-wrap long lines."),
			"tab_size":     mewIntProp("tab_size", (*Editor).SetTabSize).Tip("Tab width."),
			"syntax":       mewWordProp("syntax", (*Editor).SetSyntax).Tip("Grammar/language, or auto to detect from filename."),
			"line_numbers": mewBoolProp("line_numbers", (*Editor).SetLineNumbers).Tip("Show line numbers."),
			"caret":        stringProp("caret", (*Editor).SetCaret).Tip("Open at line:col (mew's +N)."),
		},
		nil, // leaf: no children
		func(ctx *protocol.BindContext, w core.Trinket) {
			ed := w.(*Editor)
			id := trinketID(ed)
			ed.SetOnCommit(func(value, filename string) {
				ev := protocol.NewEvent("commit").WithUint("trinket", id)
				if filename != "" {
					ev = ev.WithString("filename", filename)
				} else {
					ev = ev.WithString("value", value)
				}
				ctx.EmitEvent(ev)
			})
			ed.SetOnCancel(func() {
				ctx.EmitEvent(protocol.NewEvent("cancel").WithUint("trinket", id))
			})
		},
	)
}

// isInheritValue reports whether a rich property's wire value is the
// "default"/inherit sentinel, meaning "leave mew's own resolution alone".
func isInheritValue(name string, v *protocol.Value, f protocol.FlagState) bool {
	s, err := protocol.AsWord(name, v, f)
	if err != nil {
		return false
	}
	return s == "" || s == "default" || s == "inherit"
}

// mewBoolProp / mewIntProp / mewWordProp are inherit-aware appliers: the
// "default" sentinel is a no-op (mew resolves the option itself); any real value
// overrides.
func mewBoolProp(name string, set func(*Editor, bool)) protocol.Property {
	return protocol.NewProperty("bool", wprop(name, func(_ *protocol.BindContext, ed *Editor, v *protocol.Value, f protocol.FlagState) error {
		if isInheritValue(name, v, f) {
			return nil
		}
		b, err := protocol.AsBool(name, v, f)
		if err != nil {
			return err
		}
		set(ed, b)
		return nil
	})).Def("default")
}

func mewIntProp(name string, set func(*Editor, int)) protocol.Property {
	return protocol.NewProperty("int", wprop(name, func(_ *protocol.BindContext, ed *Editor, v *protocol.Value, f protocol.FlagState) error {
		if isInheritValue(name, v, f) {
			return nil
		}
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		set(ed, n)
		return nil
	})).Def("default")
}

func mewWordProp(name string, set func(*Editor, string)) protocol.Property {
	return protocol.NewProperty("word", wprop(name, func(_ *protocol.BindContext, ed *Editor, v *protocol.Value, f protocol.FlagState) error {
		s, err := protocol.AsWord(name, v, f)
		if err != nil {
			return err
		}
		if s == "default" || s == "inherit" {
			return nil
		}
		set(ed, s)
		return nil
	})).Def("default")
}

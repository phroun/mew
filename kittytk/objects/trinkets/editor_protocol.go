//go:build !mew

package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the placeholder "editor" (the vanilla, non-mew build).
// Under -tags mew a real mew-backed editor registers "editor" instead; the two
// are mutually exclusive by build tag, so exactly one "editor" exists per build.
// See docs/editor-trinket.md for the contract these properties/events implement.
func init() {
	regTrinket("editor",
		func() core.Trinket { return NewEditor() },
		map[string]protocol.Property{
			// Core — the placeholder honors these.
			"value":       stringProp("value", (*Editor).SetValue).Tip("The text content (read back via the commit event)."),
			"placeholder": stringProp("placeholder", (*Editor).SetPlaceholder).Tip("Hint shown when empty."),
			"caption":     stringProp("caption", (*Editor).SetCaption).Tip("Title on the editor frame."),
			"readonly":    boolProp("readonly", (*Editor).SetReadOnly).Tip("View only; disables the edit affordance.").Def("false"),
			"filename":    stringProp("filename", (*Editor).SetFilename).Tip("Host-granted file handle (placeholder: names the temp file)."),

			// Rich — accepted and IGNORED by the placeholder; mew honors them.
			// They default to the "default" inherit sentinel, so the appliers
			// must accept any value without error (see ignoredProp).
			"wrap":         ignoredProp("bool").Def("default").Tip("Soft-wrap long lines (mew honors; placeholder ignores)."),
			"tab_size":     ignoredProp("int").Def("default").Tip("Tab width (mew honors; placeholder ignores)."),
			"syntax":       ignoredProp("string").Def("default").Tip("Grammar/language, or auto (mew honors; placeholder ignores)."),
			"line_numbers": ignoredProp("bool").Def("default").Tip("Show line numbers (mew honors; placeholder ignores)."),
			"caret":        ignoredProp("string").Tip("Cursor position line:col (mew honors; placeholder ignores)."),
		},
		nil, // leaf: no children
		func(ctx *protocol.BindContext, w core.Trinket) {
			ed := w.(*Editor)
			id := trinketID(ed)
			ed.SetOnCommit(func(v string) {
				ctx.EmitEvent(protocol.NewEvent("commit").WithUint("trinket", id).WithString("value", v))
			})
			ed.SetOnCancel(func() {
				ctx.EmitEvent(protocol.NewEvent("cancel").WithUint("trinket", id))
			})
			ed.SetOnDirty(func(d bool) {
				n := 0
				if d {
					n = 1
				}
				ctx.EmitEvent(protocol.NewEvent("dirty").WithUint("trinket", id).WithInt("dirty", n))
			})
		},
	)
}

// ignoredProp declares a contract property the placeholder accepts but does
// nothing with, so apps can set the full editor vocabulary uniformly (mew
// honors these). The applier never errors, so ANY value — including the
// "default"/inherit sentinel or a real bool/int — is silently accepted. kind is
// the value kind reported for introspection only.
func ignoredProp(kind string) protocol.Property {
	return protocol.NewProperty(kind, wprop("", func(_ *protocol.BindContext, _ *Editor, _ *protocol.Value, _ protocol.FlagState) error {
		return nil
	}))
}

package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/text"
)

// textDirectionOf answers core.TextDirectioner for a trinket carrying one
// string: the direction set on it when there is one, else what the string
// itself says, else no opinion at all.
func textDirectionOf(explicit core.Direction, s string) (core.Direction, bool) {
	if explicit != core.DirInherit {
		return explicit, true
	}
	if d := text.FirstStrongDirection(s); d != core.DirInherit {
		return d, true
	}
	return core.DirInherit, false
}

// directionWord maps the wire vocabulary onto core.Direction. "auto" and
// "inherit" are the same value under two names, because a trinket handing the
// question to its own text and one handing it to its ancestors both answer
// with DirInherit -- they differ only in who is asked next.
func directionWord(prop, word string) (core.Direction, error) {
	switch word {
	case "inherit", "auto":
		return core.DirInherit, nil
	case "ltr":
		return core.DirLTR, nil
	case "rtl":
		return core.DirRTL, nil
	}
	return core.DirInherit, fmt.Errorf("%s: unknown value %q", prop, word)
}

// textDirectionProp is the `text_direction` property for a trinket carrying
// text: which way its own text is taken to run, overriding what the text says.
func textDirectionProp[T any](set func(w T, d core.Direction)) protocol.Property {
	return protocol.NewProperty("enum", wprop("text_direction", func(_ *protocol.BindContext, w T, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord("text_direction", v, f)
		if err != nil {
			return err
		}
		d, err := directionWord("text_direction", word)
		if err != nil {
			return err
		}
		set(w, d)
		return nil
	})).OneOf("auto", "ltr", "rtl").Def("auto").
		Tip("Which way this trinket's own text runs (auto reads the text).")
}

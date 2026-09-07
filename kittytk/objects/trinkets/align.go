package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// The wire vocabulary for alignment. Horizontal placement is stated against a
// direction unless it is spelled optically; vertical placement is not, our
// rows running top to bottom whatever the direction.
var (
	hAlignWords = map[string]core.HAlign{
		"textnatural":    core.AlignTextNatural,
		"textopposite":   core.AlignTextOpposite,
		"layoutnatural":  core.AlignLayoutNatural,
		"layoutopposite": core.AlignLayoutOpposite,
		"center":         core.AlignCenter,
		"opticalleft":    core.AlignOpticalLeft,
		"opticalright":   core.AlignOpticalRight,
	}
	vAlignWords = map[string]core.VAlign{
		"top":    core.AlignTop,
		"middle": core.AlignMiddle,
		"bottom": core.AlignBottom,
	}
	fillWords = map[string][2]bool{
		"none": {false, false},
		"h":    {true, false},
		"v":    {false, true},
		"both": {true, true},
	}
)

func hAlignWordList() []string {
	return []string{"textnatural", "textopposite", "layoutnatural", "layoutopposite", "center", "opticalleft", "opticalright"}
}

// hAlignWord maps the wire vocabulary onto a horizontal alignment.
func hAlignWord(prop, word string) (core.HAlign, error) {
	if a, ok := hAlignWords[word]; ok {
		return a, nil
	}
	return core.AlignCenter, fmt.Errorf("%s: unknown value %q", prop, word)
}

// alignmentOf reads a trinket's alignment hint, starting from the toolkit
// default when it has none, so setting one axis does not silently reset the
// other.
func alignmentOf(w core.Trinket) core.Alignment {
	if h, ok := w.(interface {
		LayoutAlignment() (core.Alignment, bool)
	}); ok {
		if a, set := h.LayoutAlignment(); set {
			return a
		}
	}
	return core.DefaultAlignment()
}

func setAlignment(prop string, w core.Trinket, a core.Alignment) error {
	h, ok := w.(interface{ SetLayoutAlignment(core.Alignment) })
	if !ok {
		return fmt.Errorf("%s: not supported by this type", prop)
	}
	h.SetLayoutAlignment(a)
	return nil
}

// registerAlignmentProperties wires halign, valign and fill: where an item
// sits on each axis of the space its layout gives it, and whether it grows to
// that space on each axis.
func registerAlignmentProperties() {
	protocol.RegisterCommonProperty("halign", protocol.NewProperty("enum", wprop("halign", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord("halign", v, f)
		if err != nil {
			return err
		}
		x, err := hAlignWord("halign", word)
		if err != nil {
			return err
		}
		return setAlignment("halign", w, alignmentOf(w).WithH(x))
	})).OneOf(hAlignWordList()...).Def("center").
		Tip("Where this item sits horizontally when it does not fill."))

	protocol.RegisterCommonProperty("valign", protocol.NewProperty("enum", wprop("valign", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord("valign", v, f)
		if err != nil {
			return err
		}
		y, ok := vAlignWords[word]
		if !ok {
			return fmt.Errorf("valign: unknown value %q", word)
		}
		return setAlignment("valign", w, alignmentOf(w).WithV(y))
	})).OneOf("top", "middle", "bottom").Def("middle").
		Tip("Where this item sits vertically when it does not fill."))

	protocol.RegisterCommonProperty("fill", protocol.NewProperty("enum", wprop("fill", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord("fill", v, f)
		if err != nil {
			return err
		}
		axes, ok := fillWords[word]
		if !ok {
			return fmt.Errorf("fill: unknown value %q", word)
		}
		return setAlignment("fill", w, alignmentOf(w).WithFill(axes[0], axes[1]))
	})).OneOf("none", "h", "v", "both").Def("both").
		Tip("Which axes this item grows to fill the space it is given."))
}

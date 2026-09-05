package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for Label (see docs/property-vocabulary.md).
func init() {
	regTrinket("label",
		func() core.Trinket { return NewLabel("") },
		map[string]protocol.Property{
			"caption": stringProp("caption", (*Label).SetText).Tip("Displayed text (may contain newlines)."),
			"wrap":    boolProp("wrap", (*Label).SetWordWrap).Tip("Word-wrap text (enables height-for-width).").Def("false"),
			// text_align is the TEXT alignment within the label; the
			// common halign property is the layout-item hint. Distinct
			// concepts, distinct names, one vocabulary.
			"text_align": protocol.NewProperty("enum", wprop("text_align", func(_ *protocol.BindContext, l *Label, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("text_align", v, f)
				if err != nil {
					return err
				}
				a, err := hAlignWord("text_align", w)
				if err != nil {
					return err
				}
				l.SetAlignment(a)
				return nil
			})).OneOf(hAlignWordList()...).Def("textbegin").
				Tip("Text alignment within the label."),
			"text_direction": textDirectionProp((*Label).SetTextDirection),
		},
		nil,
		nil,
		nil,
	)
}

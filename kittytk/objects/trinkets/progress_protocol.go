package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the progress bar trinket; the wiki's ProgressBar page is
// generated from it.
func init() {
	regTrinket("progress",
		func() core.Trinket { return NewProgressBar() },
		map[string]protocol.Property{
			"value":         intProp("value", (*ProgressBar).SetValue).Tip("Current progress value."),
			"minimum":       intProp("minimum", (*ProgressBar).SetMinimum).Tip("Minimum value."),
			"maximum":       intProp("maximum", (*ProgressBar).SetMaximum).Tip("Maximum value."),
			"caption":       stringProp("caption", (*ProgressBar).SetCaption).Tip("Text shown on the bar in place of the percentage, as written. Empty shows the percentage."),
			"text_visible":  boolProp("text_visible", (*ProgressBar).SetTextVisible).Tip("Show text on the bar: the caption, or the percentage when there is none.").Def("true"),
			"indeterminate": boolProp("indeterminate", (*ProgressBar).SetIndeterminate).Tip("Show indeterminate busy animation.").Def("false"),
		},
		nil,
		nil,
		nil,
	)
}

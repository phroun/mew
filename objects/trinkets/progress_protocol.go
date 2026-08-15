package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the progress bar trinket (see docs/property-vocabulary.md).
func init() {
	regTrinket("progress",
		func() core.Trinket { return NewProgressBar() },
		map[string]protocol.Property{
			"value":         intProp("value", (*ProgressBar).SetValue).Tip("Current progress value."),
			"minimum":       intProp("minimum", (*ProgressBar).SetMinimum).Tip("Minimum value."),
			"maximum":       intProp("maximum", (*ProgressBar).SetMaximum).Tip("Maximum value."),
			"caption":       stringProp("caption", (*ProgressBar).SetFormat).Tip("Optional overlay text."),
			"indeterminate": boolProp("indeterminate", (*ProgressBar).SetIndeterminate).Tip("Show indeterminate busy animation.").Def("false"),
		},
		nil,
		nil,
	)
}

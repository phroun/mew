package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the spacer trinket (see docs/property-vocabulary.md).
func init() {
	regTrinket("spacer",
		func() core.Trinket { return NewSpacer() },
		map[string]protocol.Property{
			"width": protocol.NewProperty("int", wprop("width", func(_ *protocol.BindContext, s *Spacer, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("width", v, f)
				if err != nil {
					return err
				}
				size := s.Size()
				size.Width = core.Unit(n)
				s.SetSize(size)
				return nil
			})).Tip("Explicit spacer width in units"),
			"height": protocol.NewProperty("int", wprop("height", func(_ *protocol.BindContext, s *Spacer, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("height", v, f)
				if err != nil {
					return err
				}
				size := s.Size()
				size.Height = core.Unit(n)
				s.SetSize(size)
				return nil
			})).Tip("Explicit spacer height in units"),
		},
		nil,
		nil,
	)
}

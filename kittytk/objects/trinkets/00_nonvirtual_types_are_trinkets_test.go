package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// A non-virtual type gets the common properties (enabled, visible, fg, ...)
// on top of its own, and every one of them applies to a core.Trinket. So a
// type registered non-virtual whose target is not a trinket advertises
// fifteen properties that cannot work, and each one fails by naming a Go
// type rather than by saying the property does not apply.
//
// Virtual is the marker for a wire type that is a record rather than a
// trinket -- a menu, a menu item, a status bar section -- and it is the half
// of the registration that is easy to leave off.
func TestNonVirtualTypesAreTrinkets(t *testing.T) {
	f := protocol.NewRegistryFactory(&protocol.BindContext{})
	for _, ti := range protocol.DescribeVocabulary().Types {
		if ti.Virtual {
			continue
		}
		obj, err := f.New(ti.Name)
		if err != nil {
			t.Errorf("%s: cannot construct: %v", ti.Name, err)
			continue
		}
		holder, ok := obj.(interface{ Target() any })
		if !ok {
			continue
		}
		if _, isTrinket := holder.Target().(core.Trinket); !isTrinket {
			t.Errorf("%s is registered non-virtual but its target is %T, "+
				"which is not a core.Trinket, so every common property "+
				"fails against it", ti.Name, holder.Target())
		}
	}
}

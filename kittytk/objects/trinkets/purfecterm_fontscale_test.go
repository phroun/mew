package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The rendered terminal font size is the requested size interpreted
// relative to the interface font, with 12pt as the neutral anchor: 12pt
// matches the UI font, larger sizes render proportionally larger, smaller
// proportionally smaller. At a 12pt interface it is identity.
func TestPurfecTermFontScalesWithInterface(t *testing.T) {
	term := NewPurfecTerm()
	cases := []struct{ ui, req, want int }{
		{12, 12, 12}, // historical default: identity
		{12, 14, 14},
		{12, 10, 10},
		{18, 12, 18}, // 12pt request matches the 18pt interface
		{18, 14, 21}, // a touch larger than the interface
		{18, 10, 15}, // a touch smaller than the interface
		{6, 12, 6},   // tiny interface: terminal matches it
	}
	for _, c := range cases {
		term.SetFont(&core.Font{Name: "ui-text", Size: c.ui}) // interface font
		term.SetTerminalFontSize(c.req)
		if got := term.renderTermFont().Size; got != c.want {
			t.Errorf("interface=%dpt request=%dpt: rendered %dpt, want %dpt", c.ui, c.req, got, c.want)
		}
		// The requested size is preserved verbatim for config round-trips.
		if got := term.effTermFont().Size; got != c.req {
			t.Errorf("interface=%dpt request=%dpt: effTermFont %dpt, want the raw %dpt", c.ui, c.req, got, c.req)
		}
	}
}

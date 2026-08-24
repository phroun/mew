package trinkets

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A disabled field sits on its CONTAINER's background.
//
// DisabledTextFG is the disabled counterpart to normal window text, so the
// ground it is chosen against is the container's, not the edit box's.
//
// Asserted against a container whose background DIFFERS from the edit box's:
// in the default scheme the two are the same colour, and a test on the
// default would pass whichever background the field used.
func TestDisabledFieldPaintsOnItsContainersBackground(t *testing.T) {
	b, err := raster.New(200, 16)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)

	host := NewPanel()
	host.SetParent(d)
	ground := style.RGB(90, 20, 90) // nothing else in the scheme is this
	host.SetBackgroundColor(&ground)

	ti := NewTextInput()
	ti.SetParent(host)
	ti.SetText("MMMM")
	ti.SetEnabled(false)
	ti.SetBounds(core.UnitRect{Width: 200, Height: 16})

	if got := ti.EffectiveBackgroundColor(); got != ground {
		t.Fatalf("harness: field inherits %v, want the container's %v", got, ground)
	}
	editBg := ti.GetScheme().GetEditBox(style.GetPaneType(ground)).Bg
	if editBg == ground {
		t.Fatal("harness: the container and the edit box share a colour; the test proves nothing")
	}

	b.Clear(style.DefaultStyle())
	ti.Paint(core.NewPainter(b))

	// Past the text, so what is sampled is the field's own ground and its
	// speckle rather than any glyph.
	counts := map[color.RGBA]int{}
	for x := 120; x < 200; x++ {
		for y := 0; y < 16; y++ {
			counts[b.Image().RGBAAt(x, y)]++
		}
	}
	er, eg, eb := editBg.RGBComponents()
	for c, n := range counts {
		if c.R == er && c.G == eg && c.B == eb {
			t.Errorf("the disabled field painted the edit-box background %d,%d,%d (%d px); "+
				"it should be on its container's", er, eg, eb, n)
		}
	}
}

// A disabled field is speckled. A flat rectangle says "empty"; the texture
// says "a field, unavailable".
//
// Checked against an enabled unfocused field, which is flat: the disabled one
// must not come out as the same single colour.
func TestDisabledFieldKeepsTheSpeckle(t *testing.T) {
	render := func(disabled bool) map[color.RGBA]int {
		b, _ := raster.New(200, 16)
		d := NewDesktop()
		d.SetBackend(b)
		ti := NewTextInput()
		ti.SetParent(d)
		ti.SetEnabled(!disabled)
		ti.SetBounds(core.UnitRect{Width: 200, Height: 16})
		b.Clear(style.DefaultStyle())
		ti.Paint(core.NewPainter(b))
		counts := map[color.RGBA]int{}
		for x := 120; x < 200; x++ {
			for y := 0; y < 16; y++ {
				counts[b.Image().RGBAAt(x, y)]++
			}
		}
		return counts
	}

	plain := render(false)
	off := render(true)
	if len(plain) != 1 {
		t.Fatalf("harness: an enabled unfocused field is not flat (%d colours)", len(plain))
	}
	// The speckle changes what the empty field is filled with, so the two
	// must not come out as the same single colour.
	for c := range plain {
		if len(off) == 1 {
			for d := range off {
				if d == c {
					t.Error("the disabled field is filled exactly like a plain one; the speckle is missing")
				}
			}
		}
	}
}
